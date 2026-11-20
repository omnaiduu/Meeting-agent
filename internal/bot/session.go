package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/omnaiduu/meeting-agent/internal/audio"
	"github.com/omnaiduu/meeting-agent/internal/summarize"
	"github.com/omnaiduu/meeting-agent/internal/transcribe"
)

// session is one bot attending one meeting.
type session struct {
	m        *Manager
	job      Job
	platform Platform
	log      *slog.Logger

	stopOnce sync.Once
	stop     chan struct{}

	mu    sync.Mutex
	lines []summarize.Line
}

func (s *session) requestStop() { s.stopOnce.Do(func() { close(s.stop) }) }

func (s *session) status(st Status) {
	s.log.Info("status", "status", st)
	s.m.rep.SetStatus(s.job.ID, st)
}

// run is the whole life of a bot: launch → join → record → leave → transcribe → summarize.
func (s *session) run(root context.Context) {
	cfg := s.m.cfg
	id := s.job.ID

	// ctx dies on hard shutdown; callCtx also dies when the user presses Stop.
	ctx, cancel := context.WithCancel(root)
	defer cancel()
	callCtx, cancelCall := context.WithCancel(ctx)
	defer cancelCall()
	go func() {
		select {
		case <-s.stop:
			cancelCall()
		case <-callCtx.Done():
		}
	}()

	s.status(StatusLaunching)
	bopts := cfg.Browser
	bopts.Log = func(msg string, args ...any) { s.log.Debug(msg, args...) }
	b, err := Launch(ctx, bopts)
	if err != nil {
		s.m.rep.Fail(id, "chrome_launch", err.Error())
		return
	}
	browserOpen := true
	closeBrowser := func() {
		if browserOpen {
			b.Close()
			browserOpen = false
		}
	}
	defer closeBrowser()

	// Audio path: page → reorder → (WAV file + 20 s chunks) → STT worker.
	if err := os.MkdirAll(s.job.AudioDir, 0o755); err != nil {
		s.m.rep.Fail(id, "storage", err.Error())
		return
	}
	wavPath := filepath.Join(s.job.AudioDir, "audio.wav")
	rec, err := audio.NewRecorder(wavPath)
	if err != nil {
		s.m.rep.Fail(id, "storage", err.Error())
		return
	}
	chunks := make(chan audio.Chunk, 64)
	chunker := audio.NewChunker(time.Duration(cfg.ChunkSeconds)*time.Second, func(c audio.Chunk) {
		select {
		case chunks <- c:
		default:
			s.log.Warn("transcription backlog full; dropping chunk", "index", c.Index)
			s.m.rep.Warn(id, "stt_backlog", fmt.Sprintf("Skipped audio at %s: transcription is falling behind.", summarize.Clock(c.Offset)))
		}
	})
	var (
		audioMu  sync.Mutex // serializes late page calls with the final flush
		finished bool
	)
	order := audio.NewReorderer(func(pcm []byte) {
		audioMu.Lock()
		defer audioMu.Unlock()
		if finished {
			return // a batch that raced with leaving the call
		}
		rec.Write(pcm)
		chunker.Write(pcm)
	})
	sttDone := make(chan struct{})
	go func() { defer close(sttDone); s.transcribeLoop(ctx, chunks) }()

	finishAudio := func() {
		audioMu.Lock()
		chunker.Flush()
		finished = true
		close(chunks)
		audioMu.Unlock()
		if err := rec.Close(); err != nil {
			s.log.Warn("closing recording", "err", err)
		}
		if rec.Bytes() > 0 {
			s.m.rep.AudioSaved(id, wavPath)
		} else {
			os.Remove(wavPath)
		}
	}

	if err := s.platform.Prepare(b.Ctx, order.Push); err != nil {
		finishAudio()
		<-sttDone
		s.m.rep.Fail(id, "chrome_crash", err.Error())
		return
	}

	// Join. A Stop during the lobby cancels callCtx.
	joinCtx, cancelJoin := context.WithCancel(b.Ctx)
	go func() {
		select {
		case <-callCtx.Done():
			cancelJoin()
		case <-joinCtx.Done():
		}
	}()
	err = s.platform.Join(joinCtx, JoinOptions{
		URL: s.job.URL, BotName: s.job.BotName, LobbyTimeout: cfg.LobbyTimeout,
		OnPhase: func(p Phase) {
			switch p {
			case PhaseJoining:
				s.status(StatusJoining)
			case PhaseLobby:
				s.status(StatusInLobby)
			case PhaseInCall:
				s.m.rep.Joined(id, time.Now().UTC())
				s.status(StatusInCall)
			}
		},
	})
	cancelJoin()
	if err != nil {
		code, msg := joinError(err)
		if callCtx.Err() != nil && ctx.Err() == nil {
			code, msg = "stopped", "Stopped before the bot was admitted."
		}
		s.screenshot(b.Ctx, code)
		closeBrowser()
		finishAudio()
		<-sttDone
		s.m.rep.Fail(id, code, msg)
		return
	}

	// In the call until it ends, we're alone, time runs out, or Stop.
	// chromedp actions need a context derived from the tab context, so the
	// watch context hangs off b.Ctx and is cancelled when callCtx is.
	watchCtx, cancelWatch := context.WithCancel(b.Ctx)
	go func() {
		select {
		case <-callCtx.Done():
			cancelWatch()
		case <-watchCtx.Done():
		}
	}()
	reason, werr := s.platform.WaitUntilEnded(watchCtx, WatchOptions{
		AloneTimeout: cfg.AloneTimeout, MaxDuration: cfg.MaxDuration, PollInterval: cfg.WatchPoll,
	})
	cancelWatch()
	if werr != nil {
		s.log.Warn("watching the call", "err", werr)
	}
	s.status(StatusLeaving)
	if err := s.platform.Leave(b.Ctx); err != nil {
		s.log.Debug("leave", "err", err)
	}
	s.m.rep.Ended(id, time.Now().UTC(), reason)
	closeBrowser()
	finishAudio()

	// Finish transcribing what's queued, then summarize.
	s.status(StatusTranscribing)
	select {
	case <-sttDone:
	case <-time.After(cfg.TranscribeWait):
		s.log.Warn("transcription did not finish in time")
	case <-ctx.Done():
		return
	}
	if ctx.Err() != nil {
		return
	}

	s.status(StatusSummarizing)
	s.mu.Lock()
	lines := append([]summarize.Line(nil), s.lines...)
	s.mu.Unlock()
	var dur time.Duration
	if n := len(lines); n > 0 {
		dur = lines[n-1].At
	}
	sum, err := s.m.sum.Summarize(ctx, lines, summarize.Meta{Date: s.job.Created, Duration: dur})
	if err != nil {
		s.m.rep.Fail(id, "summary_error", err.Error())
		return
	}
	s.m.rep.SetSummary(id, sum, s.m.sum.Name())
	s.status(StatusDone)
}

// transcribeLoop sends chunks to STT one at a time, in order, so transcript
// lines arrive in meeting order. Groq takes well under a second per 20 s chunk.
func (s *session) transcribeLoop(ctx context.Context, chunks <-chan audio.Chunk) {
	failures := 0
	for c := range chunks {
		if ctx.Err() != nil {
			continue // drain
		}
		if audio.IsSilent(c.PCM, audio.DefaultSilence) {
			continue // don't pay for (or hallucinate on) silence
		}
		res, err := s.m.stt.Transcribe(ctx, audio.EncodeWAV(c.PCM), s.m.cfg.Language)
		if err != nil {
			failures++
			s.log.Warn("transcription failed", "chunk", c.Index, "err", err)
			s.m.rep.Warn(s.job.ID, "stt_error", fmt.Sprintf("Could not transcribe audio at %s: %v", summarize.Clock(c.Offset), err))
			continue
		}
		for _, seg := range transcribe.Clean(res.Segments) {
			g := Segment{Start: c.Offset + seg.Start, End: c.Offset + seg.End, Text: seg.Text}
			s.mu.Lock()
			s.lines = append(s.lines, summarize.Line{At: g.Start, Text: g.Text})
			s.mu.Unlock()
			s.m.rep.AddSegment(s.job.ID, g)
		}
	}
	if failures > 0 {
		s.log.Warn("some chunks failed to transcribe", "failures", failures)
	}
}

func (s *session) screenshot(ctx context.Context, code string) {
	if s.m.cfg.DebugDir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	png, err := chromedp.Run(ctx, chromedp.FullScreenshot(80))
	if err != nil {
		return
	}
	os.MkdirAll(s.m.cfg.DebugDir, 0o755)
	p := filepath.Join(s.m.cfg.DebugDir, fmt.Sprintf("%s-%s.png", s.job.ID, code))
	if os.WriteFile(p, png, 0o644) == nil {
		s.log.Info("failure screenshot saved", "path", p)
	}
}

// joinError maps a join failure to an error_code and a message for people.
func joinError(err error) (code, msg string) {
	switch {
	case errors.Is(err, ErrDenied):
		return "denied", "The host denied the bot's request to join."
	case errors.Is(err, ErrLobbyTimeout):
		return "lobby_timeout", "Nobody admitted the bot from the lobby in time."
	case errors.Is(err, ErrJoinBlocked):
		return "join_blocked", "Google Meet did not let the bot ask to join. The meeting may be restricted to its organization."
	case errors.Is(err, ErrMeetingGone):
		return "meeting_not_found", "The meeting has ended or does not exist."
	case errors.Is(err, ErrJoinTimeout):
		return "join_timeout", "Could not find the join screen. Google Meet's page may have changed."
	case errors.Is(err, context.Canceled):
		return "stopped", "Stopped."
	default:
		return "chrome_crash", err.Error()
	}
}
