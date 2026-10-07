package stream

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidURL     = errors.New("invalid stream URL")
	ErrTooManyStreams = errors.New("too many active streams")
	ErrShuttingDown   = errors.New("server is shutting down")
)

const maxURLLength = 2048

// Manager owns the stream sessions, one per distinct RTSP URL.
type Manager struct {
	run         Runner
	idleTimeout time.Duration
	maxStreams  int
	log         *slog.Logger

	mu       sync.Mutex
	sessions map[string]*Session
	closed   bool
}

func NewManager(run Runner, idleTimeout time.Duration, maxStreams int, log *slog.Logger) *Manager {
	return &Manager{
		run:         run,
		idleTimeout: idleTimeout,
		maxStreams:  maxStreams,
		log:         log,
		sessions:    make(map[string]*Session),
	}
}

// Register returns the session for rawURL, creating it if needed. Clients
// watching the same URL share one session and so one FFmpeg process.
func (m *Manager) Register(rawURL string) (*Session, error) {
	u, err := ValidateURL(rawURL)
	if err != nil {
		return nil, err
	}
	id := streamID(u)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrShuttingDown
	}
	if s, ok := m.sessions[id]; ok && !s.isClosed() {
		s.touch()
		return s, nil
	}
	if len(m.sessions) >= m.maxStreams {
		return nil, ErrTooManyStreams
	}
	s := newSession(id, u, m.run, m.idleTimeout, m.log, m.remove)
	m.sessions[id] = s
	m.log.Info("stream registered", "stream", id, "url", RedactURL(u))
	return s, nil
}

// Get returns the session with the given ID.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// List returns a snapshot of every session, oldest first.
func (m *Manager) List() []Info {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()

	infos := make([]Info, 0, len(sessions))
	for _, s := range sessions {
		infos = append(infos, s.Info())
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Created.Before(infos[j].Created) })
	return infos
}

// Close stops every session and waits for their FFmpeg processes to exit.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
	for _, s := range sessions {
		s.wait()
	}
}

func (m *Manager) remove(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[s.ID] == s {
		delete(m.sessions, s.ID)
	}
}

// ValidateURL checks that raw is an rtsp:// or rtsps:// URL with a host and
// returns it trimmed of surrounding whitespace.
func ValidateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: URL is empty", ErrInvalidURL)
	}
	if len(raw) > maxURLLength {
		return "", fmt.Errorf("%w: URL is longer than %d characters", ErrInvalidURL, maxURLLength)
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return "", fmt.Errorf("%w: URL contains whitespace", ErrInvalidURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "rtsp" && scheme != "rtsps" {
		return "", fmt.Errorf("%w: must start with rtsp:// or rtsps://", ErrInvalidURL)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("%w: missing host", ErrInvalidURL)
	}
	return raw, nil
}

// RedactURL hides the password in a URL so it can be logged or shown.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(unparseable URL)"
	}
	return u.Redacted()
}

// streamID derives a stable ID from the URL, so re-registering the same URL,
// for example after a server restart, gives clients the same ID.
func streamID(u string) string {
	sum := sha256.Sum256([]byte(u))
	return hex.EncodeToString(sum[:8])
}
