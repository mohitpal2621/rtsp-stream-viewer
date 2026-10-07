// Package config reads the server's settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr           string        // PORT: listen port (default 8080)
	AllowedOrigins []string      // ALLOWED_ORIGINS: comma-separated browser origins, or "*"
	FFmpegPath     string        // FFMPEG_PATH: ffmpeg binary (default "ffmpeg")
	RTSPTransport  string        // RTSP_TRANSPORT: "tcp" (default) or "udp"
	IdleTimeout    time.Duration // IDLE_TIMEOUT: how long FFmpeg keeps running with no viewers
	StallTimeout   time.Duration // STALL_TIMEOUT: restart FFmpeg if it sends nothing for this long
	MaxStreams     int           // MAX_STREAMS: cap on concurrent FFmpeg processes
	StaticDir      string        // STATIC_DIR: serve the built frontend from here, if set
	DemoStreams    []DemoStream  // DEMO_STREAMS: "Name=rtsp://...,Other=rtsp://..."
	LogLevel       string        // LOG_LEVEL: debug, info, warn or error
}

// DemoStream is a ready-made stream the UI offers with one click.
type DemoStream struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func Load() (Config, error) {
	c := Config{
		Addr:           ":" + env("PORT", "8080"),
		AllowedOrigins: splitList(env("ALLOWED_ORIGINS", "*")),
		FFmpegPath:     env("FFMPEG_PATH", "ffmpeg"),
		RTSPTransport:  env("RTSP_TRANSPORT", "tcp"),
		StaticDir:      env("STATIC_DIR", ""),
		LogLevel:       env("LOG_LEVEL", "info"),
	}
	var err error
	if c.IdleTimeout, err = duration("IDLE_TIMEOUT", 20*time.Second); err != nil {
		return c, err
	}
	if c.StallTimeout, err = duration("STALL_TIMEOUT", 20*time.Second); err != nil {
		return c, err
	}
	if c.MaxStreams, err = integer("MAX_STREAMS", 16); err != nil {
		return c, err
	}
	if c.RTSPTransport != "tcp" && c.RTSPTransport != "udp" {
		return c, fmt.Errorf("RTSP_TRANSPORT must be tcp or udp, got %q", c.RTSPTransport)
	}
	if c.DemoStreams, err = demoStreams(env("DEMO_STREAMS", "")); err != nil {
		return c, err
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	v := env(key, "")
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as 30s, got %q", key, v)
	}
	return d, nil
}

func integer(key string, fallback int) (int, error) {
	v := env(key, "")
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", key, v)
	}
	return n, nil
}

func splitList(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func demoStreams(v string) ([]DemoStream, error) {
	var out []DemoStream
	for _, item := range splitList(v) {
		name, url, ok := strings.Cut(item, "=")
		if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("DEMO_STREAMS entries must look like Name=rtsp://host/path, got %q", item)
		}
		out = append(out, DemoStream{Name: strings.TrimSpace(name), URL: strings.TrimSpace(url)})
	}
	return out, nil
}
