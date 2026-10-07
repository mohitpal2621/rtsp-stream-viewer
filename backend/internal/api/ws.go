package api

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 30 * time.Second
	pingPeriod = pongWait * 2 / 3
)

// handleStreamSocket upgrades to a WebSocket and forwards the session's
// messages to the browser: JSON text for status and init metadata, binary for
// MP4 segments.
func (s *Server) handleStreamSocket(w http.ResponseWriter, r *http.Request) {
	// Checked here as well as in Upgrade so a refused page never starts FFmpeg.
	if !s.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin not allowed")
		return
	}
	sess, ok := s.streams.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown stream; register the URL again")
		return
	}
	sub, err := sess.Subscribe()
	if err != nil {
		writeError(w, http.StatusNotFound, "stream has stopped; register the URL again")
		return
	}
	defer sess.Unsubscribe(sub)

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has already replied with an HTTP error
	}
	defer conn.Close()

	// The browser never sends data; reading only processes pongs and notices
	// when the client goes away.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		conn.SetReadLimit(512)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(pongWait))
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(pingPeriod)
	defer ping.Stop()
	for {
		select {
		case msg, ok := <-sub.Messages():
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The session dropped us (too slow, or shutting down). 1013
				// "try again later" tells the client to reconnect.
				_ = conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "stream restarting"))
				return
			}
			kind := websocket.TextMessage
			if msg.Binary {
				kind = websocket.BinaryMessage
			}
			if err := conn.WriteMessage(kind, msg.Data); err != nil {
				return
			}
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-closed:
			return
		}
	}
}
