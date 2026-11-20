// Command server runs the Meeting Agent HTTP API, bot manager and dashboard.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/omnaiduu/meeting-agent/internal/api"
	"github.com/omnaiduu/meeting-agent/internal/bot"
	"github.com/omnaiduu/meeting-agent/internal/bot/meet"
	"github.com/omnaiduu/meeting-agent/internal/config"
	"github.com/omnaiduu/meeting-agent/internal/events"
	"github.com/omnaiduu/meeting-agent/internal/meeting"
	"github.com/omnaiduu/meeting-agent/internal/store"
	"github.com/omnaiduu/meeting-agent/internal/summarize"
	"github.com/omnaiduu/meeting-agent/internal/transcribe"
	"github.com/omnaiduu/meeting-agent/web"
)

func main() {
	// Container healthcheck without curl in the image: meeting-agent -healthcheck
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, "meeting-agent.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	stt, sum := providers(cfg)
	log.Info("providers", "stt", stt.Name(), "summary", sum.Name())

	hub := events.NewHub()
	svc := meeting.New(meeting.Config{DataDir: cfg.DataDir, DefaultBotName: cfg.BotName, AllowedHosts: cfg.AllowedHosts}, st, hub, log)
	if err := svc.RecoverAfterRestart(ctx); err != nil {
		return err
	}
	mgr := bot.NewManager(bot.Config{
		MaxConcurrent: cfg.MaxConcurrentBots,
		LobbyTimeout:  cfg.LobbyTimeout,
		AloneTimeout:  cfg.AloneTimeout,
		MaxDuration:   cfg.MaxMeetingDuration,
		ChunkSeconds:  cfg.ChunkSeconds,
		Language:      cfg.Language,
		DebugDir:      filepath.Join(cfg.DataDir, "debug"),
		Browser:       bot.BrowserOptions{ChromePath: cfg.ChromePath, Headless: cfg.Headless, NoSandbox: cfg.NoSandbox},
	}, log, svc, stt, sum, map[string]func() bot.Platform{
		"google_meet": func() bot.Platform { return meet.New(log) },
	})
	svc.SetRunner(mgr)

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.NewHandler(api.Deps{
			Log: log, DevCORS: cfg.DevCORS, Meetings: svc, SPA: web.Dist(),
			Info: api.Info{BotName: cfg.BotName, STTProvider: stt.Name(), SummaryProvider: sum.Name()},
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down: asking bots to leave")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	mgr.Shutdown(shutdownCtx)
	return srv.Shutdown(shutdownCtx)
}

func providers(cfg config.Config) (transcribe.Transcriber, summarize.Summarizer) {
	var stt transcribe.Transcriber = transcribe.Fake{}
	if cfg.STTProvider == "groq" {
		stt = &transcribe.Groq{APIKey: cfg.GroqAPIKey, BaseURL: cfg.GroqBaseURL, Model: cfg.GroqSTTModel}
	}
	var sum summarize.Summarizer = summarize.Fake{}
	if cfg.SummaryProvider == "groq" {
		sum = &summarize.Groq{APIKey: cfg.GroqAPIKey, BaseURL: cfg.GroqBaseURL, Model: cfg.GroqSummaryModel}
	}
	return stt, sum
}

func healthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://" + addr + "/api/health")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
