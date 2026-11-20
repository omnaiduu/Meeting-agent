// Command joinmeet is a manual test for the bot: join one meeting, record its
// audio to a WAV file, and leave when the meeting ends.
//
//	go run ./cmd/joinmeet -url https://meet.google.com/abc-defg-hij
//	go run ./cmd/joinmeet -fake            # against a local fake Meet (no Google needed)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/omnaiduu/meeting-agent/internal/audio"
	"github.com/omnaiduu/meeting-agent/internal/bot"
	"github.com/omnaiduu/meeting-agent/internal/bot/meet"
	"github.com/omnaiduu/meeting-agent/internal/bot/meet/fakemeet"
)

func main() {
	var (
		url      = flag.String("url", "", "Google Meet URL")
		name     = flag.String("name", "Meeting Agent (Notetaker)", "bot display name")
		out      = flag.String("out", "data/debug/joinmeet.wav", "where to write the recording")
		fake     = flag.Bool("fake", false, "join a local fake Meet instead of Google")
		headless = flag.Bool("headless", false, "headless Chrome (only works with -fake; Meet blocks headless)")
		lobby    = flag.Duration("lobby-timeout", 10*time.Minute, "give up if not admitted")
		alone    = flag.Duration("alone-timeout", 2*time.Minute, "leave after being alone this long")
		maxDur   = flag.Duration("max", 2*time.Hour, "max meeting length")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if *fake {
		wav, spoken := fakemeet.Speech("Hello everyone. Let's ship the new release on Friday. Priya will write the launch notes.")
		srv := fakemeet.Start(fakemeet.Script{AdmitAfter: 3 * time.Second, Others: 2, Speech: wav, AloneAfter: 15 * time.Second})
		defer srv.Close()
		*url = srv.MeetingURL()
		*alone = 6 * time.Second
		log.Info("fake meet", "url", *url, "espeak", spoken)
	}
	if *url == "" {
		fmt.Fprintln(os.Stderr, "need -url or -fake")
		os.Exit(2)
	}
	if err := run(log, *url, *name, *out, *headless, *lobby, *alone, *maxDur); err != nil {
		log.Error("failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, url, name, out string, headless bool, lobby, alone, maxDur time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	b, err := bot.Launch(context.Background(), bot.BrowserOptions{Headless: headless})
	if err != nil {
		return err
	}
	defer b.Close()

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	rec, err := audio.NewRecorder(out)
	if err != nil {
		return err
	}
	defer func() {
		rec.Close()
		log.Info("recording saved", "path", out, "seconds", audio.Duration(rec.Bytes()).Seconds())
	}()
	order := audio.NewReorderer(func(pcm []byte) { rec.Write(pcm) })

	m := meet.New(log)
	if err := m.Prepare(b.Ctx, order.Push); err != nil {
		return err
	}
	joinCtx, cancel := context.WithCancel(b.Ctx)
	go func() { <-ctx.Done(); cancel() }()
	err = m.Join(joinCtx, bot.JoinOptions{URL: url, BotName: name, LobbyTimeout: lobby,
		OnPhase: func(p bot.Phase) { log.Info("phase", "phase", p) }})
	if err != nil {
		shot(b.Ctx, log)
		return err
	}
	reason, err := m.WaitUntilEnded(joinCtx, bot.WatchOptions{AloneTimeout: alone, MaxDuration: maxDur})
	log.Info("call over", "reason", reason, "err", err)
	if lerr := m.Leave(b.Ctx); lerr != nil && !errors.Is(lerr, context.Canceled) {
		log.Warn("leave", "err", lerr)
	}
	return nil
}

func shot(ctx context.Context, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	png, err := chromedp.Run(ctx, chromedp.FullScreenshot(80))
	if err != nil {
		return
	}
	p := filepath.Join("data", "debug", fmt.Sprintf("joinmeet-%d.png", time.Now().Unix()))
	if os.WriteFile(p, png, 0o644) == nil {
		log.Info("screenshot saved", "path", p)
	}
}
