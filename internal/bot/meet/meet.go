// Package meet is the Google Meet adapter: join as a guest, wait in the lobby,
// detect the end of the call, and leave. Audio capture is injected JS.
package meet

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/omnaiduu/meeting-agent/internal/bot"
)

//go:embed inject/capture.js
var captureJS string

//go:embed inject/probe.js
var probeJS string

// Meet implements bot.Platform for Google Meet.
type Meet struct {
	Log      *slog.Logger
	Patterns Patterns
	// PrejoinTimeout is how long to look for the name field / join button.
	PrejoinTimeout time.Duration
	// Poll is the join-loop interval.
	Poll time.Duration

	wasInCall bool
}

// New returns a Meet adapter with default patterns.
func New(log *slog.Logger) *Meet {
	if log == nil {
		log = slog.Default()
	}
	return &Meet{Log: log, Patterns: DefaultPatterns, PrejoinTimeout: 90 * time.Second, Poll: 700 * time.Millisecond}
}

var _ bot.Platform = (*Meet)(nil)

// State is what the probe reports.
type State struct {
	Phase        string `json:"phase"` // prejoin, lobby, in_call, denied, blocked, ended, unknown
	Match        string `json:"match"`
	Participants int    `json:"participants"` // -1 = unknown
	Name         bool   `json:"name"`
	Join         bool   `json:"join"`
	Dismiss      bool   `json:"dismiss"`
	Leave        bool   `json:"leave"`
	Audio        struct {
		Tracks       int    `json:"tracks"`
		Live         int    `json:"live"`
		MsSinceAudio int64  `json:"msSinceAudio"` // -1 = never
		Mode         string `json:"mode"`
	} `json:"audio"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type audioMsg struct {
	Seq int    `json:"seq"`
	PCM string `json:"pcm"`
}

// Prepare injects the capture script and the Go audio callback before navigation.
func (m *Meet) Prepare(ctx context.Context, onAudio bot.AudioFunc) error {
	return chromedp.Do(ctx,
		chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			// Meet's Content Security Policy and Trusted Types would block our
			// AudioWorklet blob URL. Bypass CSP for this tab only.
			_, err := chromedp.Call(ctx, page.SetBypassCSP, page.SetBypassCSPParams{Enabled: true})
			return err
		}),
		chromedp.ExposeFunc("__meetAgentAudio", func(ctx context.Context, a audioMsg) (chromedp.Void, error) {
			pcm, err := base64.StdEncoding.DecodeString(a.PCM)
			if err != nil {
				return chromedp.Void{}, err
			}
			onAudio(a.Seq, pcm)
			return chromedp.Void{}, nil
		}),
		chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := chromedp.Call(ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: captureJS})
			return err
		}),
	)
}

// Probe runs the in-page probe once.
func (m *Meet) Probe(ctx context.Context) (State, error) {
	pj, _ := json.Marshal(m.Patterns)
	expr := fmt.Sprintf("(%s)(%s, %t)", probeJS, pj, m.wasInCall)
	return chromedp.Run(ctx, chromedp.Evaluate[State](expr))
}

// Join opens the meeting, types the bot name, asks to join and waits for admission.
func (m *Meet) Join(ctx context.Context, o bot.JoinOptions) error {
	target, err := withEnglish(o.URL)
	if err != nil {
		return err
	}
	if err := chromedp.Do(ctx, chromedp.Navigate(target)); err != nil {
		return fmt.Errorf("open meeting: %w", err)
	}

	phase := func(p bot.Phase) {
		if o.OnPhase != nil {
			o.OnPhase(p)
		}
	}
	var (
		start      = time.Now()
		named      bool
		askedAt    time.Time
		lobbySince time.Time
		last       string
	)
	tick := time.NewTicker(m.Poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
		st, err := m.Probe(ctx)
		if err != nil {
			m.Log.Debug("probe failed", "err", err)
			continue // page navigating; try again
		}
		if st.Phase != last {
			m.Log.Info("meet phase", "phase", st.Phase, "match", st.Match, "title", st.Title)
			last = st.Phase
		}
		switch st.Phase {
		case "in_call":
			m.Log.Info("admitted", "audio_tracks", st.Audio.Tracks, "capture_mode", st.Audio.Mode, "participants", st.Participants)
			m.wasInCall = true
			phase(bot.PhaseInCall)
			return nil
		case "denied":
			return bot.ErrDenied
		case "blocked":
			return fmt.Errorf("%w: %q", bot.ErrJoinBlocked, st.Match)
		case "ended":
			return fmt.Errorf("%w: %q", bot.ErrMeetingGone, st.Match)
		case "lobby":
			if lobbySince.IsZero() {
				lobbySince = time.Now()
				phase(bot.PhaseLobby)
			}
			if o.LobbyTimeout > 0 && time.Since(lobbySince) > o.LobbyTimeout {
				return bot.ErrLobbyTimeout
			}
			continue
		}

		// Pre-join (or unknown while the page loads).
		if st.Dismiss {
			m.click(ctx, "dismiss")
			continue
		}
		if st.Name && !named {
			if err := m.typeName(ctx, o.BotName); err != nil {
				m.Log.Warn("typing name failed", "err", err)
				continue
			}
			named = true
			continue
		}
		if st.Join && (named || !st.Name) && (askedAt.IsZero() || time.Since(askedAt) > 15*time.Second) {
			if askedAt.IsZero() {
				phase(bot.PhaseJoining)
			}
			if m.click(ctx, "join") {
				askedAt = time.Now()
			}
			continue
		}
		if askedAt.IsZero() && time.Since(start) > m.PrejoinTimeout {
			return fmt.Errorf("%w (page title %q)", bot.ErrJoinTimeout, st.Title)
		}
		// Asked, but neither lobby nor call yet: wait. Lobby timeout covers the rest.
		if !askedAt.IsZero() && o.LobbyTimeout > 0 && time.Since(askedAt) > o.LobbyTimeout+30*time.Second {
			return bot.ErrLobbyTimeout
		}
	}
}

// WaitUntilEnded polls until the meeting is over.
func (m *Meet) WaitUntilEnded(ctx context.Context, o bot.WatchOptions) (bot.EndReason, error) {
	poll := o.PollInterval
	if poll <= 0 {
		poll = 5 * time.Second
	}
	start := time.Now()
	var aloneSince time.Time
	failures := 0
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return bot.EndStopped, nil
		case <-tick.C:
		}
		if o.MaxDuration > 0 && time.Since(start) > o.MaxDuration {
			return bot.EndMaxDuration, nil
		}
		st, err := m.Probe(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return bot.EndStopped, nil
			}
			failures++
			if failures >= 6 { // ~30 s of a dead page
				return bot.EndMeetingOver, fmt.Errorf("page not responding: %w", err)
			}
			continue
		}
		failures = 0
		if st.Phase == "ended" || st.Phase == "denied" {
			m.Log.Info("meeting over", "match", st.Match)
			return bot.EndMeetingOver, nil
		}
		// Alone = the People count says only us AND nobody has made a sound for a
		// while. Two independent signals, so a UI glitch alone can't end the recording.
		quiet := st.Audio.MsSinceAudio < 0 || time.Duration(st.Audio.MsSinceAudio)*time.Millisecond > o.AloneTimeout/2
		alone := st.Participants >= 0 && st.Participants <= 1 && quiet
		if alone {
			if aloneSince.IsZero() {
				aloneSince = time.Now()
				m.Log.Info("bot seems alone", "participants", st.Participants)
			}
			if time.Since(aloneSince) >= o.AloneTimeout {
				return bot.EndAlone, nil
			}
		} else {
			aloneSince = time.Time{}
		}
	}
}

// Leave clicks "Leave call" (and a confirmation if Meet asks).
func (m *Meet) Leave(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void]("window.__meetAgentFlush && window.__meetAgentFlush()"))
	for i := 0; i < 3; i++ {
		st, err := m.Probe(ctx)
		if err != nil {
			return err
		}
		if st.Phase == "ended" || !st.Leave {
			return nil
		}
		m.click(ctx, "leave")
		time.Sleep(time.Second)
	}
	return nil
}

func (m *Meet) click(ctx context.Context, mark string) bool {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := chromedp.Do(cctx, chromedp.Click(chromedp.CSS("[data-ma-"+mark+"]"), chromedp.NodeVisible))
	if err != nil {
		m.Log.Debug("click failed", "mark", mark, "err", err)
		return false
	}
	m.Log.Info("clicked", "mark", mark)
	return true
}

func (m *Meet) typeName(ctx context.Context, name string) error {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	sel := chromedp.CSS("[data-ma-name]")
	return chromedp.Do(cctx,
		chromedp.Click(sel, chromedp.NodeVisible),
		chromedp.Evaluate[chromedp.Void](`(() => { const el = document.querySelector('[data-ma-name]'); if (el) el.select(); })()`),
		chromedp.SendKeys(sel, name),
	)
}

// withEnglish adds hl=en so Meet renders the English UI our texts expect.
func withEnglish(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("bad meeting url: %w", err)
	}
	q := u.Query()
	if q.Get("hl") == "" {
		q.Set("hl", "en")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// MeetingCode extracts "abc-defg-hij" from a Meet URL.
func MeetingCode(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.Trim(u.Path, "/")
}
