package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/omnaiduu/meeting-agent/internal/summarize"
	"github.com/omnaiduu/meeting-agent/internal/transcribe"
)

// Status is a meeting's lifecycle state (docs/PLAN.md §3).
type Status string

const (
	StatusQueued       Status = "queued"
	StatusLaunching    Status = "launching"
	StatusJoining      Status = "joining"
	StatusInLobby      Status = "in_lobby"
	StatusInCall       Status = "in_call"
	StatusLeaving      Status = "leaving"
	StatusTranscribing Status = "transcribing"
	StatusSummarizing  Status = "summarizing"
	StatusDone         Status = "done"
	StatusFailed       Status = "failed"
)

// Terminal reports whether no more changes will happen.
func (s Status) Terminal() bool { return s == StatusDone || s == StatusFailed }

// Segment is a transcript line placed on the meeting timeline.
type Segment struct {
	Start, End time.Duration
	Text       string
}

// Reporter receives everything a bot session produces. The meeting service
// implements it: each call writes to the DB and publishes an SSE event.
type Reporter interface {
	SetStatus(id string, s Status)
	Joined(id string, at time.Time)
	Ended(id string, at time.Time, reason EndReason)
	AudioSaved(id string, path string)
	AddSegment(id string, seg Segment)
	SetSummary(id string, s summarize.Summary, model string)
	Fail(id string, code, message string)
	// Warn reports a non-fatal problem (e.g. one chunk failed to transcribe).
	Warn(id string, code, message string)
}

// Job is one meeting for a bot to attend.
type Job struct {
	ID       string
	Platform string // "google_meet"
	URL      string
	BotName  string
	AudioDir string // where to write audio.wav
	Created  time.Time
}

// Config for the manager.
type Config struct {
	MaxConcurrent  int
	LobbyTimeout   time.Duration
	AloneTimeout   time.Duration
	MaxDuration    time.Duration
	ChunkSeconds   int
	Language       string
	DebugDir       string // failure screenshots
	Browser        BrowserOptions
	WatchPoll      time.Duration
	ShutdownWait   time.Duration
	TranscribeWait time.Duration // max time for STT after the call ends
}

// Runner starts and stops bots. In-process today; a container-per-meeting
// runner could implement the same interface later.
type Runner interface {
	Start(job Job) error
	Stop(id string) error
	Active(id string) bool
	Shutdown(ctx context.Context)
}

// Manager runs one goroutine (and one Chrome) per meeting, at most
// MaxConcurrent at once; the rest wait as "queued".
type Manager struct {
	cfg        Config
	log        *slog.Logger
	rep        Reporter
	stt        transcribe.Transcriber
	sum        summarize.Summarizer
	platforms  map[string]func() Platform
	sem        chan struct{}
	root       context.Context
	cancelRoot context.CancelFunc

	mu       sync.Mutex
	sessions map[string]*session
	closed   bool
	wg       sync.WaitGroup
}

var _ Runner = (*Manager)(nil)

// NewManager builds a manager. platforms maps a platform name to an adapter factory.
func NewManager(cfg Config, log *slog.Logger, rep Reporter, stt transcribe.Transcriber, sum summarize.Summarizer, platforms map[string]func() Platform) *Manager {
	if cfg.MaxConcurrent < 1 {
		cfg.MaxConcurrent = 1
	}
	if cfg.ShutdownWait == 0 {
		cfg.ShutdownWait = 30 * time.Second
	}
	if cfg.TranscribeWait == 0 {
		cfg.TranscribeWait = 5 * time.Minute
	}
	root, cancel := context.WithCancel(context.Background())
	return &Manager{
		cfg: cfg, log: log, rep: rep, stt: stt, sum: sum, platforms: platforms,
		sem: make(chan struct{}, cfg.MaxConcurrent), root: root, cancelRoot: cancel,
		sessions: map[string]*session{},
	}
}

// ErrShuttingDown is returned by Start during shutdown.
var ErrShuttingDown = errors.New("server is shutting down")

// Start launches a bot for job in the background.
func (m *Manager) Start(job Job) error {
	newPlatform, ok := m.platforms[job.Platform]
	if !ok {
		return fmt.Errorf("unsupported platform %q", job.Platform)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrShuttingDown
	}
	if _, dup := m.sessions[job.ID]; dup {
		return fmt.Errorf("meeting %s already has a bot", job.ID)
	}
	s := &session{m: m, job: job, platform: newPlatform(), stop: make(chan struct{}), log: m.log.With("meeting", job.ID)}
	m.sessions[job.ID] = s
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() {
			m.mu.Lock()
			delete(m.sessions, job.ID)
			m.mu.Unlock()
		}()
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("bot panicked", "panic", v, "stack", string(debug.Stack()))
				m.rep.Fail(job.ID, "internal", fmt.Sprint("bot crashed: ", v))
			}
		}()
		// Wait for a free slot; the meeting stays "queued" meanwhile.
		select {
		case m.sem <- struct{}{}:
		case <-s.stop:
			m.rep.Fail(job.ID, "stopped", "Stopped before the bot started.")
			return
		case <-m.root.Done():
			return
		}
		defer func() { <-m.sem }()
		s.run(m.root)
	}()
	return nil
}

// Stop asks a bot to leave now. The transcript and summary are still produced.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no active bot for meeting %s", id)
	}
	s.requestStop()
	return nil
}

// Active reports whether a bot is running (or queued) for id.
func (m *Manager) Active(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[id]
	return ok
}

// Shutdown tells every bot to leave, waits up to ShutdownWait (or ctx), then
// kills whatever is left. Killed meetings are marked "interrupted" on next start.
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	m.closed = true
	for _, s := range m.sessions {
		s.requestStop()
	}
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	t := time.NewTimer(m.cfg.ShutdownWait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	case <-ctx.Done():
	}
	m.cancelRoot()
	<-done
}
