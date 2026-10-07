package stream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeSource is a Runner the test drives by hand.
type fakeSource struct {
	starts chan RunOptions
	segs   chan Segment
	fail   chan error
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		starts: make(chan RunOptions, 10),
		segs:   make(chan Segment),
		fail:   make(chan error),
	}
}

func (f *fakeSource) Run(ctx context.Context, opts RunOptions, emit func(Segment)) error {
	f.starts <- opts
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case seg := <-f.segs:
			emit(seg)
		case err := <-f.fail:
			return err
		}
	}
}

func (f *fakeSource) waitStart(t *testing.T) RunOptions {
	t.Helper()
	select {
	case opts := <-f.starts:
		return opts
	case <-time.After(3 * time.Second):
		t.Fatal("runner was not started")
		return RunOptions{}
	}
}

var (
	initSeg = Segment{Init: true, Data: []byte("init"), Codec: "avc1.64001f"}
	frag1   = Segment{Data: []byte("frag1")}
	frag2   = Segment{Data: []byte("frag2")}
)

func next(t *testing.T, sub *Subscriber) Message {
	t.Helper()
	select {
	case m, ok := <-sub.Messages():
		if !ok {
			t.Fatal("subscriber channel closed")
		}
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a message")
		return Message{}
	}
}

func expectJSON(t *testing.T, sub *Subscriber, field, want string) map[string]any {
	t.Helper()
	m := next(t, sub)
	if m.Binary {
		t.Fatalf("got binary message %q, want JSON with %s=%s", m.Data, field, want)
	}
	var v map[string]any
	if err := json.Unmarshal(m.Data, &v); err != nil {
		t.Fatal(err)
	}
	if v[field] != want {
		t.Fatalf("got %s, want %s=%s", m.Data, field, want)
	}
	return v
}

func expectBinary(t *testing.T, sub *Subscriber, want string) {
	t.Helper()
	m := next(t, sub)
	if !m.Binary || string(m.Data) != want {
		t.Fatalf("got %q (binary=%v), want binary %q", m.Data, m.Binary, want)
	}
}

func TestViewersShareOneRun(t *testing.T) {
	src := newFakeSource()
	m := NewManager(src.Run, time.Minute, 4, discard)
	defer m.Close()

	s, err := m.Register("rtsp://camera/one")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Subscribe()
	src.waitStart(t)
	b, _ := s.Subscribe()

	src.segs <- initSeg
	src.segs <- frag1
	for _, sub := range []*Subscriber{a, b} {
		expectJSON(t, sub, "state", "connecting")
		v := expectJSON(t, sub, "type", "init")
		if v["mime"] != `video/mp4; codecs="avc1.64001f"` {
			t.Errorf("mime = %v", v["mime"])
		}
		expectBinary(t, sub, "init")
		expectJSON(t, sub, "state", "live")
		expectBinary(t, sub, "frag1")
	}

	// A late joiner gets the current status and the init segment straight
	// away, then fragments from the next one on.
	c, _ := s.Subscribe()
	expectJSON(t, c, "state", "live")
	expectJSON(t, c, "type", "init")
	expectBinary(t, c, "init")
	src.segs <- frag2
	expectBinary(t, c, "frag2")

	select {
	case <-src.starts:
		t.Fatal("a second runner was started for the same URL")
	default:
	}
	if got := s.Info().Viewers; got != 3 {
		t.Errorf("viewers = %d, want 3", got)
	}
}

func TestSessionStopsWhenIdle(t *testing.T) {
	src := newFakeSource()
	m := NewManager(src.Run, 50*time.Millisecond, 4, discard)
	defer m.Close()

	s, _ := m.Register("rtsp://camera/idle")
	sub, _ := s.Subscribe()
	src.waitStart(t)
	s.Unsubscribe(sub)

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := m.Get(s.ID); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle session was not removed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := s.Subscribe(); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Subscribe on closed session: err = %v", err)
	}
	s.wait() // the runner goroutine must have exited

	// Registering the URL again creates a fresh session with the same ID.
	again, err := m.Register("rtsp://camera/idle")
	if err != nil || again == s || again.ID != s.ID {
		t.Fatalf("re-register: err=%v same=%v id=%s/%s", err, again == s, again.ID, s.ID)
	}
}

func TestRegisteredButUnwatchedSessionExpires(t *testing.T) {
	m := NewManager(newFakeSource().Run, 50*time.Millisecond, 4, discard)
	defer m.Close()
	s, _ := m.Register("rtsp://camera/never-watched")
	time.Sleep(200 * time.Millisecond)
	if _, ok := m.Get(s.ID); ok {
		t.Fatal("session nobody subscribed to was kept")
	}
}

func TestFailureIsReportedAndRetried(t *testing.T) {
	src := newFakeSource()
	m := NewManager(src.Run, time.Minute, 4, discard)
	defer m.Close()

	s, _ := m.Register("rtsp://camera/flaky")
	sub, _ := s.Subscribe()
	src.waitStart(t)
	expectJSON(t, sub, "state", "connecting")

	src.segs <- initSeg
	expectJSON(t, sub, "type", "init")
	expectBinary(t, sub, "init")

	src.fail <- &SourceError{Message: "Connection refused: is the RTSP server running?"}
	v := expectJSON(t, sub, "state", "retrying")
	if v["error"] != "Connection refused: is the RTSP server running?" || v["retryIn"] != float64(1) {
		t.Errorf("retrying status = %v", v)
	}

	// The stale init segment must not be replayed to viewers joining now.
	late, _ := s.Subscribe()
	expectJSON(t, late, "state", "retrying")
	select {
	case m := <-late.Messages():
		t.Fatalf("late viewer got %q while retrying", m.Data)
	default:
	}

	src.waitStart(t) // retried after the backoff
	expectJSON(t, sub, "state", "connecting")
}

func TestUnsupportedCodecSwitchesToTranscoding(t *testing.T) {
	src := newFakeSource()
	m := NewManager(src.Run, time.Minute, 4, discard)
	defer m.Close()

	s, _ := m.Register("rtsp://camera/hevc")
	if _, err := s.Subscribe(); err != nil {
		t.Fatal(err)
	}
	if opts := src.waitStart(t); opts.Transcode {
		t.Fatal("first run should copy the video")
	}
	src.fail <- &UnsupportedCodecError{Codec: "hvc1"}
	if opts := src.waitStart(t); !opts.Transcode {
		t.Fatal("second run should transcode")
	}
	if !s.Info().Transcoding {
		t.Error("Info should report transcoding")
	}
}

func TestSlowViewerDoesNotBlockOthers(t *testing.T) {
	src := newFakeSource()
	m := NewManager(src.Run, time.Minute, 4, discard)
	defer m.Close()

	s, _ := m.Register("rtsp://camera/busy")
	slow, _ := s.Subscribe()
	src.waitStart(t)
	fast, _ := s.Subscribe()
	src.segs <- initSeg

	// Drain only the fast viewer while sending more fragments than the
	// slow viewer's queue can hold.
	for range 3 {
		next(t, fast) // status, init text, init binary
	}
	for i := range subscriberQueue * 2 {
		src.segs <- frag1
		if i == 0 {
			expectJSON(t, fast, "state", "live")
		}
		expectBinary(t, fast, "frag1")
	}

	s.mu.Lock()
	dropped := slow.dropped
	s.mu.Unlock()
	if dropped == 0 {
		t.Fatal("slow viewer should have skipped fragments")
	}
	// The slow viewer is still subscribed and its queue starts with what it
	// needs to decode.
	expectJSON(t, slow, "state", "connecting")
	expectJSON(t, slow, "type", "init")
	expectBinary(t, slow, "init")
}

func TestRegisterValidatesAndLimits(t *testing.T) {
	m := NewManager(newFakeSource().Run, time.Minute, 2, discard)
	defer m.Close()

	if _, err := m.Register("http://camera/one"); !errors.Is(err, ErrInvalidURL) {
		t.Errorf("http URL: err = %v, want ErrInvalidURL", err)
	}
	a, _ := m.Register("rtsp://camera/one")
	b, _ := m.Register("  rtsp://camera/one ")
	if a != b {
		t.Error("the same URL should map to the same session")
	}
	if _, err := m.Register("rtsp://camera/two"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Register("rtsp://camera/three"); !errors.Is(err, ErrTooManyStreams) {
		t.Errorf("third stream: err = %v, want ErrTooManyStreams", err)
	}
}

func TestValidateURL(t *testing.T) {
	valid := []string{
		"rtsp://localhost:8554/cam",
		"RTSP://192.168.1.10/stream1",
		"rtsps://user:p%40ss@camera.example.com:322/live?channel=1",
	}
	for _, u := range valid {
		if _, err := ValidateURL(u); err != nil {
			t.Errorf("ValidateURL(%q) = %v", u, err)
		}
	}
	invalid := map[string]string{
		"":                       "empty",
		"http://example.com/cam": "rtsp://",
		"rtsp:///no-host":        "missing host",
		"rtsp://host/with space": "whitespace",
		"-i rtsp://host":         "whitespace",
		"rtsp://" + strings.Repeat("a", maxURLLength): "longer than",
	}
	for u, want := range invalid {
		_, err := ValidateURL(u)
		if !errors.Is(err, ErrInvalidURL) || !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateURL(%.40q) = %v, want error mentioning %q", u, err, want)
		}
	}
}

func TestRedactURL(t *testing.T) {
	got := RedactURL("rtsp://admin:secret@cam.local:554/h264")
	if strings.Contains(got, "secret") || !strings.Contains(got, "admin") {
		t.Errorf("RedactURL = %q", got)
	}
}
