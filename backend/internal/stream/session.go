package stream

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// State is where a session's pipeline is in its lifecycle.
type State string

const (
	StateConnecting State = "connecting" // FFmpeg is starting, or waiting for the first fragment
	StateLive       State = "live"       // fragments are flowing
	StateRetrying   State = "retrying"   // the last attempt failed; waiting before the next one
)

// Status is what viewers are told about a session.
type Status struct {
	State   State  `json:"state"`
	Error   string `json:"error,omitempty"`
	RetryIn int    `json:"retryIn,omitempty"` // seconds until the next attempt
}

// Message is one WebSocket message for a viewer: JSON text for control
// messages, binary for MP4 data.
type Message struct {
	Binary bool
	Data   []byte
}

// Subscriber is one viewer's queue of outgoing messages.
type Subscriber struct {
	ch      chan Message
	closed  bool // guarded by Session.mu
	dropped int  // fragments skipped because the viewer fell behind; guarded by Session.mu
}

// Messages returns the viewer's queue. It is closed when the session drops
// the viewer or shuts down.
func (sub *Subscriber) Messages() <-chan Message { return sub.ch }

func (sub *Subscriber) offer(m Message) bool {
	select {
	case sub.ch <- m:
		return true
	default:
		return false
	}
}

// ErrSessionClosed is returned when subscribing to a session that has shut down.
var ErrSessionClosed = errors.New("stream session closed")

const (
	subscriberQueue = 16 // messages; with one fragment per GOP this is several seconds of video
	minRetryDelay   = time.Second
	maxRetryDelay   = 30 * time.Second
	stableRunTime   = 30 * time.Second // a run this long resets the retry backoff
)

// Session pulls one RTSP stream with a single FFmpeg process and fans the
// output out to every viewer of that URL. FFmpeg starts with the first viewer
// and stops once the last one has been gone for the idle timeout.
type Session struct {
	ID  string
	URL string

	run         Runner
	idleTimeout time.Duration
	onClose     func(*Session)
	log         *slog.Logger
	created     time.Time

	mu          sync.Mutex
	subs        map[*Subscriber]struct{}
	status      Status
	initMsgs    []Message // the latest init segment, replayed to viewers who join later
	codec       string
	transcoding bool
	stop        context.CancelFunc // cancels the running pipeline; nil if none
	done        chan struct{}      // closed when the pipeline goroutine exits
	idleTimer   *time.Timer
	idleGen     int // invalidates idle timers that fired after being replaced
	closed      bool
}

func newSession(id, url string, run Runner, idleTimeout time.Duration, log *slog.Logger, onClose func(*Session)) *Session {
	s := &Session{
		ID:          id,
		URL:         url,
		run:         run,
		idleTimeout: idleTimeout,
		onClose:     onClose,
		log:         log.With("stream", id),
		created:     time.Now(),
		subs:        make(map[*Subscriber]struct{}),
		status:      Status{State: StateConnecting},
	}
	s.mu.Lock()
	s.armIdleTimerLocked() // a session nobody subscribes to still goes away
	s.mu.Unlock()
	return s
}

// Subscribe adds a viewer, starting FFmpeg if it isn't running. The viewer's
// queue starts with the current status and, if one is known, the init segment.
func (s *Session) Subscribe() (*Subscriber, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrSessionClosed
	}

	sub := &Subscriber{ch: make(chan Message, subscriberQueue)}
	s.subs[sub] = struct{}{}
	s.cancelIdleTimerLocked()

	sub.offer(statusMessage(s.status))
	for _, m := range s.initMsgs {
		sub.offer(m)
	}

	if s.stop == nil {
		ctx, cancel := context.WithCancel(context.Background())
		s.stop = cancel
		s.done = make(chan struct{})
		go s.loop(ctx, s.done)
	}
	s.log.Info("viewer joined", "viewers", len(s.subs))
	return sub, nil
}

// Unsubscribe removes a viewer. It is safe to call more than once.
func (s *Session) Unsubscribe(sub *Subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[sub]; !ok {
		return
	}
	s.dropLocked(sub)
	s.log.Info("viewer left", "viewers", len(s.subs), "dropped_fragments", sub.dropped)
}

// touch postpones idle shutdown, so a session that was just looked up by a
// client about to connect isn't closed under it.
func (s *Session) touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed && len(s.subs) == 0 {
		s.armIdleTimerLocked()
	}
}

// Close stops the pipeline and disconnects every viewer.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked()
}

// wait blocks until the pipeline goroutine, if one was started, has exited.
func (s *Session) wait() {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Info is a snapshot of a session for the API.
type Info struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	State       State     `json:"state"`
	Error       string    `json:"error,omitempty"`
	Viewers     int       `json:"viewers"`
	Codec       string    `json:"codec,omitempty"`
	Transcoding bool      `json:"transcoding"`
	Created     time.Time `json:"created"`
}

func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{
		ID:          s.ID,
		URL:         RedactURL(s.URL),
		State:       s.status.State,
		Error:       s.status.Error,
		Viewers:     len(s.subs),
		Codec:       s.codec,
		Transcoding: s.transcoding,
		Created:     s.created,
	}
}

// loop runs FFmpeg until ctx is cancelled, retrying with exponential backoff
// when the source fails.
func (s *Session) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	delay := minRetryDelay
	transcode := false

	for {
		started := time.Now()
		err := s.run(ctx, RunOptions{URL: s.URL, Transcode: transcode}, s.publish)
		if ctx.Err() != nil {
			return
		}

		var codecErr *UnsupportedCodecError
		if errors.As(err, &codecErr) && !transcode {
			s.log.Info("source is not H.264, switching to transcoding", "codec", codecErr.Codec)
			transcode = true
			s.mu.Lock()
			s.transcoding = true
			s.mu.Unlock()
			continue
		}

		if time.Since(started) > stableRunTime {
			delay = minRetryDelay
		}
		message := err.Error()
		var srcErr *SourceError
		if errors.As(err, &srcErr) {
			s.log.Warn("stream failed", "error", message, "ffmpeg", lastLine(srcErr.Detail), "retry_in", delay)
		} else {
			s.log.Warn("stream failed", "error", message, "retry_in", delay)
		}
		s.setStatus(Status{State: StateRetrying, Error: message, RetryIn: int(delay.Seconds())}, true)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, maxRetryDelay)
		s.setStatus(Status{State: StateConnecting}, false)
	}
}

// publish delivers a segment from the runner to every viewer.
func (s *Session) publish(seg Segment) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if seg.Init {
		meta, _ := json.Marshal(struct {
			Type string `json:"type"`
			Mime string `json:"mime"`
		}{"init", `video/mp4; codecs="` + seg.Codec + `"`})
		s.initMsgs = []Message{{Data: meta}, {Binary: true, Data: seg.Data}}
		s.codec = seg.Codec
		for sub := range s.subs {
			s.sendControlLocked(sub, s.initMsgs...)
		}
		return
	}

	if s.status.State != StateLive {
		s.setStatusLocked(Status{State: StateLive}, false)
	}
	frag := Message{Binary: true, Data: seg.Data}
	for sub := range s.subs {
		// Every fragment starts on a keyframe, so a viewer that is behind can
		// skip one and carry on decoding from the next.
		if !sub.offer(frag) {
			sub.dropped++
		}
	}
}

func (s *Session) setStatus(st Status, clearInit bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setStatusLocked(st, clearInit)
}

// setStatusLocked records and broadcasts a status. clearInit forgets the init
// segment, which is stale once FFmpeg has exited.
func (s *Session) setStatusLocked(st Status, clearInit bool) {
	s.status = st
	if clearInit {
		s.initMsgs = nil
	}
	msg := statusMessage(st)
	for sub := range s.subs {
		s.sendControlLocked(sub, msg)
	}
}

// sendControlLocked queues messages a viewer can't do without. A viewer whose
// queue is too full to take them is disconnected; its client reconnects and
// starts over from a fresh init segment.
func (s *Session) sendControlLocked(sub *Subscriber, msgs ...Message) {
	for _, m := range msgs {
		if !sub.offer(m) {
			s.log.Warn("viewer too slow, disconnecting")
			s.dropLocked(sub)
			return
		}
	}
}

func (s *Session) dropLocked(sub *Subscriber) {
	delete(s.subs, sub)
	if !sub.closed {
		sub.closed = true
		close(sub.ch)
	}
	if len(s.subs) == 0 && !s.closed {
		s.armIdleTimerLocked()
	}
}

func (s *Session) armIdleTimerLocked() {
	s.cancelIdleTimerLocked()
	gen := s.idleGen
	s.idleTimer = time.AfterFunc(s.idleTimeout, func() { s.expire(gen) })
}

func (s *Session) cancelIdleTimerLocked() {
	s.idleGen++
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
}

func (s *Session) expire(gen int) {
	s.mu.Lock()
	if s.closed || gen != s.idleGen || len(s.subs) > 0 {
		s.mu.Unlock()
		return
	}
	s.log.Info("no viewers, stopping stream")
	s.closeLocked()
	s.mu.Unlock()
	s.onClose(s)
}

func (s *Session) closeLocked() {
	if s.closed {
		return
	}
	s.closed = true
	s.cancelIdleTimerLocked()
	if s.stop != nil {
		s.stop()
	}
	for sub := range s.subs {
		delete(s.subs, sub)
		sub.closed = true
		close(sub.ch)
	}
}

func statusMessage(st Status) Message {
	data, _ := json.Marshal(struct {
		Type string `json:"type"`
		Status
	}{"status", st})
	return Message{Data: data}
}
