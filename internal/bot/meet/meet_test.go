package meet_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/omnaiduu/meeting-agent/internal/audio"
	"github.com/omnaiduu/meeting-agent/internal/bot"
	"github.com/omnaiduu/meeting-agent/internal/bot/meet"
	"github.com/omnaiduu/meeting-agent/internal/bot/meet/fakemeet"
)

// These tests drive real (headless) Chrome against the local fake Meet.
// Skipped with -short or when no Chrome is installed.

func launch(t *testing.T) (*bot.Browser, *meet.Meet) {
	t.Helper()
	if testing.Short() {
		t.Skip("browser test")
	}
	if os.Getenv("CHROME_PATH") == "" {
		if _, err := exec.LookPath("google-chrome"); err != nil {
			if _, err := exec.LookPath("chromium"); err != nil {
				t.Skip("no Chrome")
			}
		}
	}
	b, err := bot.Launch(context.Background(), bot.BrowserOptions{
		ChromePath: os.Getenv("CHROME_PATH"), Headless: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	m := meet.New(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	m.Poll = 200 * time.Millisecond
	return b, m
}

type pcmSink struct {
	mu  sync.Mutex
	pcm []byte
}

func (s *pcmSink) push(seq int, b []byte) { s.mu.Lock(); s.pcm = append(s.pcm, b...); s.mu.Unlock() }
func (s *pcmSink) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.pcm...)
}

func TestJoinRecordAndLeaveWhenAlone(t *testing.T) {
	b, m := launch(t)
	wav, _ := fakemeet.Speech("Testing one two three. The launch is on Friday.")
	f := fakemeet.Start(fakemeet.Script{AdmitAfter: 2 * time.Second, Others: 2, Speech: wav, AloneAfter: 6 * time.Second})
	defer f.Close()

	sink := &pcmSink{}
	if err := m.Prepare(b.Ctx, sink.push); err != nil {
		t.Fatal(err)
	}
	var phases []bot.Phase
	ctx, cancel := context.WithTimeout(b.Ctx, 60*time.Second)
	defer cancel()
	err := m.Join(ctx, bot.JoinOptions{URL: f.MeetingURL(), BotName: "Test Bot", LobbyTimeout: 30 * time.Second,
		OnPhase: func(p bot.Phase) { phases = append(phases, p) }})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if got := f.Knocked(); got != "Test Bot" {
		t.Errorf("typed name %q", got)
	}
	want := []bot.Phase{bot.PhaseJoining, bot.PhaseLobby, bot.PhaseInCall}
	if len(phases) != 3 || phases[0] != want[0] || phases[1] != want[1] || phases[2] != want[2] {
		t.Errorf("phases %v, want %v", phases, want)
	}

	reason, err := m.WaitUntilEnded(ctx, bot.WatchOptions{AloneTimeout: 3 * time.Second, PollInterval: 500 * time.Millisecond})
	if err != nil || reason != bot.EndAlone {
		t.Fatalf("end: %v %v", reason, err)
	}
	if err := m.Leave(ctx); err != nil {
		t.Fatal(err)
	}
	if s := f.State(); s != "left" {
		t.Errorf("fake meet state %q, want left (Leave call clicked)", s)
	}
	pcm := sink.bytes()
	if audio.Duration(len(pcm)) < 3*time.Second || audio.IsSilent(pcm, audio.DefaultSilence) {
		t.Errorf("captured %v of audio, silent=%v", audio.Duration(len(pcm)), audio.IsSilent(pcm, audio.DefaultSilence))
	}
}

func TestDenied(t *testing.T) {
	b, m := launch(t)
	f := fakemeet.Start(fakemeet.Script{AdmitAfter: time.Second, Deny: true})
	defer f.Close()
	ctx, cancel := context.WithTimeout(b.Ctx, 30*time.Second)
	defer cancel()
	err := m.Join(ctx, bot.JoinOptions{URL: f.MeetingURL(), BotName: "Bot", LobbyTimeout: 20 * time.Second})
	if !errors.Is(err, bot.ErrDenied) {
		t.Fatalf("got %v, want ErrDenied", err)
	}
}

// The lobby shows "Leave call" and "Return to home screen" like real Meet.
// Neither may be read as "admitted" or "ended": the bot must wait, then time out.
func TestLobbyTrapsAndTimeout(t *testing.T) {
	b, m := launch(t)
	f := fakemeet.Start(fakemeet.Script{}) // never admits
	defer f.Close()
	ctx, cancel := context.WithTimeout(b.Ctx, 30*time.Second)
	defer cancel()
	err := m.Join(ctx, bot.JoinOptions{URL: f.MeetingURL(), BotName: "Bot", LobbyTimeout: 3 * time.Second})
	if !errors.Is(err, bot.ErrLobbyTimeout) {
		t.Fatalf("got %v, want ErrLobbyTimeout", err)
	}
}

func TestMeetingEnds(t *testing.T) {
	b, m := launch(t)
	f := fakemeet.Start(fakemeet.Script{AdmitAfter: time.Second, Others: 3, EndAfter: 3 * time.Second})
	defer f.Close()
	if err := m.Prepare(b.Ctx, func(int, []byte) {}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(b.Ctx, 40*time.Second)
	defer cancel()
	if err := m.Join(ctx, bot.JoinOptions{URL: f.MeetingURL(), BotName: "Bot", LobbyTimeout: 20 * time.Second}); err != nil {
		t.Fatal(err)
	}
	reason, err := m.WaitUntilEnded(ctx, bot.WatchOptions{AloneTimeout: time.Minute, PollInterval: 500 * time.Millisecond})
	if err != nil || reason != bot.EndMeetingOver {
		t.Fatalf("got %v %v, want meeting_over", reason, err)
	}
}

func TestStopCancels(t *testing.T) {
	b, m := launch(t)
	f := fakemeet.Start(fakemeet.Script{AdmitAfter: time.Second, Others: 2})
	defer f.Close()
	ctx, cancel := context.WithTimeout(b.Ctx, 30*time.Second)
	defer cancel()
	if err := m.Join(ctx, bot.JoinOptions{URL: f.MeetingURL(), BotName: "Bot", LobbyTimeout: 20 * time.Second}); err != nil {
		t.Fatal(err)
	}
	stopCtx, stop := context.WithCancel(ctx)
	time.AfterFunc(time.Second, stop)
	reason, _ := m.WaitUntilEnded(stopCtx, bot.WatchOptions{AloneTimeout: time.Minute, PollInterval: 300 * time.Millisecond})
	if reason != bot.EndStopped {
		t.Fatalf("got %v", reason)
	}
}

func TestMeetingCode(t *testing.T) {
	if c := meet.MeetingCode("https://meet.google.com/abc-defg-hij?authuser=0"); c != "abc-defg-hij" {
		t.Fatal(c)
	}
}
