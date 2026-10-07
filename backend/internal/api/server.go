// Package api exposes stream sessions over HTTP and WebSocket.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/mohitpal2621/rtsp-stream-viewer/backend/internal/config"
	"github.com/mohitpal2621/rtsp-stream-viewer/backend/internal/stream"
)

type Server struct {
	streams  *stream.Manager
	cfg      config.Config
	log      *slog.Logger
	upgrader websocket.Upgrader
}

func NewServer(streams *stream.Manager, cfg config.Config, log *slog.Logger) *Server {
	s := &Server{streams: streams, cfg: cfg, log: log}
	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 64 << 10,
		CheckOrigin:     s.originAllowed,
	}
	return s
}

// Handler returns the HTTP routes:
//
//	GET  /healthz              liveness check
//	GET  /api/config           demo streams for the UI
//	GET  /api/streams          active sessions
//	POST /api/streams          register an RTSP URL, returns its stream ID
//	GET  /api/streams/{id}/ws  WebSocket carrying the stream as fragmented MP4
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/streams", s.handleListStreams)
	mux.HandleFunc("POST /api/streams", s.handleCreateStream)
	mux.HandleFunc("GET /api/streams/{id}/ws", s.handleStreamSocket)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	if s.cfg.StaticDir != "" {
		mux.Handle("/", spaHandler(s.cfg.StaticDir))
	}
	return s.cors(mux)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	demos := s.cfg.DemoStreams
	if demos == nil {
		demos = []config.DemoStream{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"demoStreams": demos})
}

func (s *Server) handleListStreams(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"streams": s.streams.List()})
}

func (s *Server) handleCreateStream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be JSON like {\"url\": \"rtsp://...\"}")
		return
	}

	sess, err := s.streams.Register(req.URL)
	switch {
	case errors.Is(err, stream.ErrInvalidURL):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, stream.ErrTooManyStreams):
		writeError(w, http.StatusServiceUnavailable, "The server is already pulling as many streams as it allows. Try again later.")
		return
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": sess.ID})
}

// cors lets a frontend hosted on another origin call the API.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && s.originAllowed(r) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.Header().Set("Access-Control-Max-Age", "3600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed accepts requests with no Origin header, from the server's own
// origin, and from the configured origins.
func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if u, err := url.Parse(origin); err == nil && strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, allowed := range s.cfg.AllowedOrigins {
		if allowed == "*" || strings.EqualFold(strings.TrimSuffix(allowed, "/"), origin) {
			return true
		}
	}
	return false
}

// spaHandler serves the built frontend, falling back to index.html for
// client-side routes.
func spaHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") { // Vite puts content hashes in these names
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
