// Command server is the RTSP stream viewer backend. It pulls RTSP streams
// with FFmpeg and relays them to browsers over WebSockets.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mohitpal2621/rtsp-stream-viewer/backend/internal/api"
	"github.com/mohitpal2621/rtsp-stream-viewer/backend/internal/config"
	"github.com/mohitpal2621/rtsp-stream-viewer/backend/internal/stream"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}))

	timeoutOption, err := stream.SocketTimeoutOption(cfg.FFmpegPath)
	if err != nil {
		log.Error("can't run ffmpeg; install it or set FFMPEG_PATH", "path", cfg.FFmpegPath, "error", err)
		os.Exit(1)
	}

	ffmpeg := &stream.FFmpeg{
		Path:          cfg.FFmpegPath,
		RTSPTransport: cfg.RTSPTransport,
		TimeoutOption: timeoutOption,
		StallTimeout:  cfg.StallTimeout,
		Log:           log,
	}
	streams := stream.NewManager(ffmpeg.Run, cfg.IdleTimeout, cfg.MaxStreams, log)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.NewServer(streams, cfg, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("listening", "addr", cfg.Addr, "static_dir", cfg.StaticDir, "demo_streams", len(cfg.DemoStreams))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Stopping the sessions first closes the WebSocket viewers, which
	// Shutdown does not track because they are hijacked connections.
	streams.Close()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "error", err)
	}
}

func parseLevel(s string) slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return level
}
