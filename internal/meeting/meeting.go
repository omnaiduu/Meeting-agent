// Package meeting is the application service: create, list, stop and delete
// meetings, and record everything bots report (DB write + live event).
package meeting

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/omnaiduu/meeting-agent/internal/bot"
	"github.com/omnaiduu/meeting-agent/internal/events"
	"github.com/omnaiduu/meeting-agent/internal/store"
	"github.com/omnaiduu/meeting-agent/internal/store/db"
	"github.com/omnaiduu/meeting-agent/internal/summarize"
)

// Meeting is the API view of a meeting.
type Meeting struct {
	ID           string             `json:"id"`
	Platform     string             `json:"platform"`
	MeetingURL   string             `json:"meeting_url"`
	MeetingCode  string             `json:"meeting_code"`
	BotName      string             `json:"bot_name"`
	Title        *string            `json:"title"`
	Status       bot.Status         `json:"status"`
	ErrorCode    *string            `json:"error_code"`
	ErrorMessage *string            `json:"error_message"`
	EndReason    *string            `json:"end_reason"`
	CreatedAt    time.Time          `json:"created_at"`
	JoinedAt     *time.Time         `json:"joined_at"`
	EndedAt      *time.Time         `json:"ended_at"`
	HasAudio     bool               `json:"has_audio"`
	Summary      *summarize.Summary `json:"summary,omitempty"`
	SummaryModel *string            `json:"summary_model,omitempty"`
}

// Segment is one transcript line.
type Segment struct {
	ID      int64  `json:"id"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

// StatusEvent is the payload of an SSE "status" event.
type StatusEvent struct {
	Status       bot.Status `json:"status"`
	At           time.Time  `json:"at"`
	ErrorCode    string     `json:"error_code,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
}

// WarnEvent is the payload of an SSE "error" event (non-fatal).
type WarnEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Errors returned to the API.
var (
	ErrNotFound   = errors.New("meeting not found")
	ErrInvalidURL = errors.New("invalid meeting URL")
	ErrNotActive  = errors.New("no bot is running for this meeting")
)

// Config for the service.
type Config struct {
	DataDir        string
	DefaultBotName string
	AllowedHosts   []string // meet.google.com (+ local fake hosts in tests)
}

// Service implements the use cases and bot.Reporter.
type Service struct {
	cfg    Config
	st     *store.Store
	hub    *events.Hub
	runner bot.Runner
	log    *slog.Logger
}

var _ bot.Reporter = (*Service)(nil)

// New creates the service. Call SetRunner before Create.
func New(cfg Config, st *store.Store, hub *events.Hub, log *slog.Logger) *Service {
	return &Service{cfg: cfg, st: st, hub: hub, log: log}
}

// SetRunner wires the bot runner (it needs the service as its Reporter).
func (s *Service) SetRunner(r bot.Runner) { s.runner = r }

// RecoverAfterRestart fails meetings a crash left in progress.
func (s *Service) RecoverAfterRestart(ctx context.Context) error {
	ids, err := s.st.FailUnfinished(ctx)
	if len(ids) > 0 {
		s.log.Warn("marked interrupted meetings as failed", "count", len(ids))
	}
	return err
}

var meetCode = regexp.MustCompile(`^[a-z]{3}-[a-z]{4}-[a-z]{3}$`)

// ParseURL validates a meeting link and returns its code.
func (s *Service) ParseURL(raw string) (clean, code string, err error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("%w: paste a link like https://meet.google.com/abc-defg-hij", ErrInvalidURL)
	}
	allowed := false
	for _, h := range s.cfg.AllowedHosts {
		if strings.EqualFold(u.Host, h) {
			allowed = true
		}
	}
	if !allowed {
		return "", "", fmt.Errorf("%w: only Google Meet links are supported (got %s)", ErrInvalidURL, u.Host)
	}
	code = strings.ToLower(strings.Trim(u.Path, "/"))
	if strings.EqualFold(u.Host, "meet.google.com") {
		if u.Scheme != "https" || !meetCode.MatchString(code) {
			return "", "", fmt.Errorf("%w: expected https://meet.google.com/abc-defg-hij", ErrInvalidURL)
		}
	}
	if code == "" {
		return "", "", fmt.Errorf("%w: the link has no meeting code", ErrInvalidURL)
	}
	u.Fragment = ""
	return u.String(), code, nil
}

// Create stores a meeting and starts a bot for it.
func (s *Service) Create(ctx context.Context, rawURL, botName string) (Meeting, error) {
	clean, code, err := s.ParseURL(rawURL)
	if err != nil {
		return Meeting{}, err
	}
	botName = strings.TrimSpace(botName)
	if botName == "" {
		botName = s.cfg.DefaultBotName
	}
	if len([]rune(botName)) > 60 {
		botName = string([]rune(botName)[:60])
	}
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	now := time.Now().UTC()
	row, err := s.st.CreateMeeting(ctx, db.CreateMeetingParams{
		ID: id, Platform: "google_meet", MeetingUrl: clean, MeetingCode: code, BotName: botName,
		Status: string(bot.StatusQueued), CreatedAt: now,
	})
	if err != nil {
		return Meeting{}, err
	}
	err = s.runner.Start(bot.Job{ID: id, Platform: "google_meet", URL: clean, BotName: botName, AudioDir: s.audioDir(id), Created: now})
	if err != nil {
		s.Fail(id, "start_failed", err.Error())
		return s.Get(ctx, id)
	}
	return toMeeting(row), nil
}

// List returns recent meetings, newest first.
func (s *Service) List(ctx context.Context, limit int) ([]Meeting, error) {
	rows, err := s.st.ListMeetings(ctx, int64(limit))
	if err != nil {
		return nil, err
	}
	out := make([]Meeting, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMeeting(r))
	}
	return out, nil
}

// Get returns one meeting with its summary, if any.
func (s *Service) Get(ctx context.Context, id string) (Meeting, error) {
	row, err := s.st.GetMeeting(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Meeting{}, ErrNotFound
	}
	if err != nil {
		return Meeting{}, err
	}
	m := toMeeting(row)
	if sr, err := s.st.GetSummary(ctx, id); err == nil {
		var sum summarize.Summary
		if json.Unmarshal([]byte(sr.Content), &sum) == nil {
			m.Summary, m.SummaryModel = &sum, &sr.Model
		}
	}
	return m, nil
}

// Transcript returns segments after afterID (0 = all).
func (s *Service) Transcript(ctx context.Context, id string, afterID int64) ([]Segment, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.st.ListSegmentsAfter(ctx, db.ListSegmentsAfterParams{MeetingID: id, ID: afterID})
	if err != nil {
		return nil, err
	}
	out := make([]Segment, 0, len(rows))
	for _, r := range rows {
		out = append(out, Segment{ID: r.ID, StartMs: r.StartMs, EndMs: r.EndMs, Text: r.Text})
	}
	return out, nil
}

// Stop asks the bot to leave; the transcript and summary are still produced.
func (s *Service) Stop(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if err := s.runner.Stop(id); err != nil {
		return ErrNotActive
	}
	return nil
}

// Delete stops any bot and removes the meeting, transcript and audio.
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if s.runner.Active(id) {
		_ = s.runner.Stop(id)
		deadline := time.Now().Add(15 * time.Second)
		for s.runner.Active(id) && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if err := s.st.DeleteMeeting(ctx, id); err != nil {
		return err
	}
	return os.RemoveAll(s.audioDir(id))
}

// AudioPath returns the recording path, if one exists.
func (s *Service) AudioPath(ctx context.Context, id string) (string, error) {
	row, err := s.st.GetMeeting(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !row.AudioPath.Valid {
		return "", ErrNotFound
	}
	return row.AudioPath.String, nil
}

// Subscribe to a meeting's live events.
func (s *Service) Subscribe(id string) (<-chan events.Event, func()) { return s.hub.Subscribe(id) }

func (s *Service) audioDir(id string) string { return filepath.Join(s.cfg.DataDir, "meetings", id) }

// ---- bot.Reporter: every call writes to the DB, then publishes an event ----

func bg() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func (s *Service) SetStatus(id string, st bot.Status) {
	ctx, cancel := bg()
	defer cancel()
	n, err := s.st.UpdateStatus(ctx, db.UpdateStatusParams{Status: string(st), ID: id})
	if err != nil {
		s.log.Error("update status", "meeting", id, "err", err)
		return
	}
	if n > 0 {
		s.hub.Publish(id, events.Event{Type: events.TypeStatus, Data: StatusEvent{Status: st, At: time.Now().UTC()}})
	}
}

func (s *Service) Fail(id, code, message string) {
	ctx, cancel := bg()
	defer cancel()
	now := time.Now().UTC()
	n, err := s.st.SetFailed(ctx, db.SetFailedParams{
		ErrorCode: sql.NullString{String: code, Valid: true}, ErrorMessage: sql.NullString{String: message, Valid: true},
		EndedAt: sql.NullTime{Time: now, Valid: true}, ID: id,
	})
	if err != nil {
		s.log.Error("set failed", "meeting", id, "err", err)
		return
	}
	s.log.Warn("meeting failed", "meeting", id, "code", code, "msg", message)
	if n > 0 {
		s.hub.Publish(id, events.Event{Type: events.TypeStatus, Data: StatusEvent{Status: bot.StatusFailed, At: now, ErrorCode: code, ErrorMessage: message}})
	}
}

func (s *Service) Warn(id, code, message string) {
	s.hub.Publish(id, events.Event{Type: events.TypeError, Data: WarnEvent{Code: code, Message: message}})
}

func (s *Service) Joined(id string, at time.Time) {
	ctx, cancel := bg()
	defer cancel()
	if err := s.st.SetJoined(ctx, db.SetJoinedParams{JoinedAt: sql.NullTime{Time: at, Valid: true}, ID: id}); err != nil {
		s.log.Error("set joined", "meeting", id, "err", err)
	}
}

func (s *Service) Ended(id string, at time.Time, reason bot.EndReason) {
	ctx, cancel := bg()
	defer cancel()
	err := s.st.SetEnded(ctx, db.SetEndedParams{EndedAt: sql.NullTime{Time: at, Valid: true}, EndReason: sql.NullString{String: string(reason), Valid: true}, ID: id})
	if err != nil {
		s.log.Error("set ended", "meeting", id, "err", err)
	}
}

func (s *Service) AudioSaved(id, path string) {
	ctx, cancel := bg()
	defer cancel()
	if err := s.st.SetAudioPath(ctx, db.SetAudioPathParams{AudioPath: sql.NullString{String: path, Valid: true}, ID: id}); err != nil {
		s.log.Error("set audio path", "meeting", id, "err", err)
	}
}

func (s *Service) AddSegment(id string, g bot.Segment) {
	ctx, cancel := bg()
	defer cancel()
	row, err := s.st.InsertSegment(ctx, db.InsertSegmentParams{
		MeetingID: id, StartMs: g.Start.Milliseconds(), EndMs: g.End.Milliseconds(), Text: g.Text, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		s.log.Error("insert segment", "meeting", id, "err", err)
		return
	}
	seg := Segment{ID: row.ID, StartMs: row.StartMs, EndMs: row.EndMs, Text: row.Text}
	s.hub.Publish(id, events.Event{Type: events.TypeTranscript, ID: row.ID, Data: seg})
}

func (s *Service) SetSummary(id string, sum summarize.Summary, model string) {
	ctx, cancel := bg()
	defer cancel()
	b, _ := json.Marshal(sum)
	err := s.st.UpsertSummary(ctx, db.UpsertSummaryParams{MeetingID: id, Model: model, Content: string(b), CreatedAt: time.Now().UTC()})
	if err != nil {
		s.log.Error("save summary", "meeting", id, "err", err)
		return
	}
	if sum.Title != "" {
		_ = s.st.SetTitle(ctx, db.SetTitleParams{Title: sql.NullString{String: sum.Title, Valid: true}, ID: id})
	}
	s.hub.Publish(id, events.Event{Type: events.TypeSummary, Data: sum})
}

func toMeeting(r db.Meeting) Meeting {
	str := func(n sql.NullString) *string {
		if n.Valid {
			return &n.String
		}
		return nil
	}
	tm := func(n sql.NullTime) *time.Time {
		if n.Valid {
			t := n.Time.UTC()
			return &t
		}
		return nil
	}
	return Meeting{
		ID: r.ID, Platform: r.Platform, MeetingURL: r.MeetingUrl, MeetingCode: r.MeetingCode, BotName: r.BotName,
		Title: str(r.Title), Status: bot.Status(r.Status), ErrorCode: str(r.ErrorCode), ErrorMessage: str(r.ErrorMessage),
		EndReason: str(r.EndReason), CreatedAt: r.CreatedAt.UTC(), JoinedAt: tm(r.JoinedAt), EndedAt: tm(r.EndedAt),
		HasAudio: r.AudioPath.Valid,
	}
}
