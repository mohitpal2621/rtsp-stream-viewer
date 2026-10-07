package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mohitpal2621/rtsp-stream-viewer/backend/internal/config"
	"github.com/mohitpal2621/rtsp-stream-viewer/backend/internal/stream"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeRunner emits an init segment and then a fragment every 20ms.
func fakeRunner(ctx context.Context, _ stream.RunOptions, emit func(stream.Segment)) error {
	emit(stream.Segment{Init: true, Data: []byte("init"), Codec: "avc1.42e01e"})
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			emit(stream.Segment{Data: []byte("fragment")})
		}
	}
}

func newTestServer(t *testing.T, cfg config.Config) *httptest.Server {
	t.Helper()
	streams := stream.NewManager(fakeRunner, time.Minute, 4, discard)
	srv := httptest.NewServer(NewServer(streams, cfg, discard).Handler())
	t.Cleanup(func() {
		streams.Close()
		srv.Close()
	})
	return srv
}

func postStream(t *testing.T, base, body string) (int, map[string]string) {
	t.Helper()
	resp, err := http.Post(base+"/api/streams", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestCreateStream(t *testing.T) {
	srv := newTestServer(t, config.Config{})

	code, out := postStream(t, srv.URL, `{"url":"rtsp://camera.local/live"}`)
	if code != http.StatusOK || len(out["id"]) != 16 {
		t.Fatalf("valid URL: %d %v", code, out)
	}
	_, again := postStream(t, srv.URL, `{"url":"rtsp://camera.local/live"}`)
	if again["id"] != out["id"] {
		t.Errorf("same URL got a different ID: %s vs %s", again["id"], out["id"])
	}

	for _, body := range []string{`{"url":"http://camera.local"}`, `not json`, `{"url":""}`} {
		code, out := postStream(t, srv.URL, body)
		if code != http.StatusBadRequest || out["error"] == "" {
			t.Errorf("body %s: %d %v, want 400 with an error", body, code, out)
		}
	}
}

func TestStreamSocket(t *testing.T) {
	srv := newTestServer(t, config.Config{})
	_, out := postStream(t, srv.URL, `{"url":"rtsp://camera.local/live"}`)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/streams/" + out["id"] + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	// connecting status, init metadata, init segment, live status, fragment
	want := []struct {
		kind int
		has  string
	}{
		{websocket.TextMessage, `"state":"connecting"`},
		{websocket.TextMessage, `"type":"init"`},
		{websocket.BinaryMessage, "init"},
		{websocket.TextMessage, `"state":"live"`},
		{websocket.BinaryMessage, "fragment"},
	}
	for i, w := range want {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if kind != w.kind || !strings.Contains(string(data), w.has) {
			t.Fatalf("message %d = %q (type %d), want type %d containing %q", i, data, kind, w.kind, w.has)
		}
	}
}

func TestStreamSocketUnknownID(t *testing.T) {
	srv := newTestServer(t, config.Config{})
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/streams/0000000000000000/ws"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("err=%v resp=%v, want 404", err, resp)
	}
}

func TestOrigins(t *testing.T) {
	srv := newTestServer(t, config.Config{AllowedOrigins: []string{"https://viewer.example.com"}})

	preflight := func(origin string) *http.Response {
		req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/api/streams", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if got := preflight("https://viewer.example.com").Header.Get("Access-Control-Allow-Origin"); got != "https://viewer.example.com" {
		t.Errorf("allowed origin: Access-Control-Allow-Origin = %q", got)
	}
	if got := preflight("https://evil.example.com").Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("other origin: Access-Control-Allow-Origin = %q, want none", got)
	}

	_, out := postStream(t, srv.URL, `{"url":"rtsp://camera.local/live"}`)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/streams/" + out["id"] + "/ws"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": {"https://evil.example.com"}})
	if err == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("WebSocket from a foreign origin: err=%v status=%v, want 403", err, resp.StatusCode)
	}

	// The refused request must not have subscribed, which would start FFmpeg.
	list, err := http.Get(srv.URL + "/api/streams")
	if err != nil {
		t.Fatal(err)
	}
	defer list.Body.Close()
	var body struct {
		Streams []stream.Info `json:"streams"`
	}
	if err := json.NewDecoder(list.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Streams) != 1 || body.Streams[0].Viewers != 0 {
		t.Errorf("streams after a refused WebSocket: %+v, want one with no viewers", body.Streams)
	}
}

func TestDemoConfig(t *testing.T) {
	srv := newTestServer(t, config.Config{DemoStreams: []config.DemoStream{{Name: "Test", URL: "rtsp://localhost:8554/testsrc"}}})
	resp, err := http.Get(srv.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		DemoStreams []config.DemoStream `json:"demoStreams"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.DemoStreams) != 1 || body.DemoStreams[0].Name != "Test" {
		t.Errorf("demoStreams = %+v", body.DemoStreams)
	}
}
