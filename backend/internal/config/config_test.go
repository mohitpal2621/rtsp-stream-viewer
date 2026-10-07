package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.RTSPTransport != "tcp" || c.MaxStreams != 16 || c.IdleTimeout != 20*time.Second {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if len(c.AllowedOrigins) != 1 || c.AllowedOrigins[0] != "*" {
		t.Errorf("AllowedOrigins = %v", c.AllowedOrigins)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("PORT", "9000")
	t.Setenv("ALLOWED_ORIGINS", "https://a.example, https://b.example")
	t.Setenv("IDLE_TIMEOUT", "5s")
	t.Setenv("DEMO_STREAMS", "Test pattern=rtsp://localhost:8554/testsrc, Life=rtsp://localhost:8554/life?x=1")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":9000" || c.IdleTimeout != 5*time.Second || len(c.AllowedOrigins) != 2 {
		t.Errorf("got %+v", c)
	}
	want := []DemoStream{
		{"Test pattern", "rtsp://localhost:8554/testsrc"},
		{"Life", "rtsp://localhost:8554/life?x=1"},
	}
	if len(c.DemoStreams) != 2 || c.DemoStreams[0] != want[0] || c.DemoStreams[1] != want[1] {
		t.Errorf("DemoStreams = %+v", c.DemoStreams)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	for key, value := range map[string]string{
		"IDLE_TIMEOUT":   "soon",
		"MAX_STREAMS":    "-1",
		"RTSP_TRANSPORT": "http",
		"DEMO_STREAMS":   "no-equals-sign",
	} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := Load(); err == nil {
				t.Errorf("%s=%q should be rejected", key, value)
			}
		})
	}
}
