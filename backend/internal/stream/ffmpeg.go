package stream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RunOptions describes one attempt at pulling a stream.
type RunOptions struct {
	URL       string
	Transcode bool // re-encode to H.264 instead of copying the source video
}

// Runner pulls the stream described by opts and calls emit for each segment
// until the stream ends, fails, or ctx is cancelled. A Runner must not call
// emit after it returns.
type Runner func(ctx context.Context, opts RunOptions, emit func(Segment)) error

// UnsupportedCodecError is returned when the source video is not H.264 and
// the run was not transcoding, so browsers could not decode it.
type UnsupportedCodecError struct {
	Codec string
}

func (e *UnsupportedCodecError) Error() string {
	return fmt.Sprintf("source codec %q is not playable in browsers", e.Codec)
}

// SourceError is a failed or ended run, with a message fit to show users.
type SourceError struct {
	Message string
	Detail  string // last lines of FFmpeg's stderr, for logs
}

func (e *SourceError) Error() string { return e.Message }

// FFmpeg runs one ffmpeg process per attempt and splits its fragmented MP4
// output into segments.
type FFmpeg struct {
	Path          string        // ffmpeg binary
	RTSPTransport string        // "tcp" or "udp"
	StallTimeout  time.Duration // kill ffmpeg if it produces no output for this long
	Log           *slog.Logger
}

// Args builds the ffmpeg command line for opts.
func (f *FFmpeg) Args(opts RunOptions) []string {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-rtsp_transport", f.RTSPTransport,
		"-timeout", "10000000", // socket I/O timeout in microseconds
		"-i", opts.URL,
		"-map", "0:v:0", "-an", "-sn", "-dn",
	}
	if opts.Transcode {
		args = append(args,
			"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
			"-pix_fmt", "yuv420p", "-vf", "scale='min(1280,iw)':-2",
			"-force_key_frames", "expr:gte(t,n_forced*1)", // a fragment every second
		)
	} else {
		args = append(args, "-c:v", "copy")
	}
	return append(args,
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"pipe:1",
	)
}

// Run implements Runner.
func (f *FFmpeg) Run(ctx context.Context, opts RunOptions, emit func(Segment)) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(runCtx, f.Path, f.Args(opts)...)
	cmd.WaitDelay = 2 * time.Second
	stderr := &tailBuffer{max: 4 << 10}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	var lastOutput atomic.Int64
	lastOutput.Store(time.Now().UnixNano())
	var stalled atomic.Bool
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-tick.C:
				if time.Since(time.Unix(0, lastOutput.Load())) > f.StallTimeout {
					stalled.Store(true)
					cancel()
					return
				}
			}
		}
	}()

	readErr := readSegments(stdout, func(seg Segment) error {
		lastOutput.Store(time.Now().UnixNano())
		if seg.Init && !opts.Transcode && !isH264(seg.Codec) {
			return &UnsupportedCodecError{Codec: seg.Codec}
		}
		emit(seg)
		return nil
	})
	cancel() // stop ffmpeg if we stopped reading first
	waitErr := cmd.Wait()

	var codecErr *UnsupportedCodecError
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.As(readErr, &codecErr):
		return codecErr
	case stalled.Load():
		return &SourceError{Message: fmt.Sprintf("No video received for %s", f.StallTimeout), Detail: stderr.String()}
	}
	detail := stderr.String()
	f.Log.Debug("ffmpeg exited", "read_err", readErr, "wait_err", waitErr, "stderr", detail)
	return &SourceError{Message: describeFailure(detail, waitErr), Detail: detail}
}

// describeFailure turns ffmpeg's stderr into a short message for users.
func describeFailure(stderr string, waitErr error) string {
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "401 unauthorized"):
		return "Authentication failed: check the username and password in the URL"
	case strings.Contains(s, "404 not found"):
		return "Stream not found: check the path in the URL"
	case strings.Contains(s, "connection refused"):
		return "Connection refused: is the RTSP server running?"
	// -138 is how FFmpeg reports ETIMEDOUT on Windows. Don't match on
	// "timeout" alone: FFmpeg echoes the URL with "?timeout=" in most errors.
	case strings.Contains(s, "timed out") || strings.Contains(s, "error number -138"):
		return "Timed out waiting for the RTSP server"
	case strings.Contains(s, "failed to resolve hostname") || strings.Contains(s, "name or service not known"):
		return "Unknown host: check the address in the URL"
	case strings.Contains(s, "network is unreachable") || strings.Contains(s, "no route to host"):
		return "Network unreachable: the server can't reach that address"
	case strings.Contains(s, "invalid data found"):
		return "The server did not send a stream FFmpeg can read"
	}
	if line := lastLine(stderr); line != "" {
		return line
	}
	if waitErr != nil {
		return "Stream stopped: " + waitErr.Error()
	}
	return "Stream ended"
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// tailBuffer is an io.Writer that keeps only the last max bytes written.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
