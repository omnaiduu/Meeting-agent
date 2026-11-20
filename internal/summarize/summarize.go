// Package summarize turns a finished transcript into a structured summary.
package summarize

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ActionItem is one follow-up task.
type ActionItem struct {
	Task  string  `json:"task"`
	Owner *string `json:"owner"`
	Due   *string `json:"due"`
}

// Summary is the JSON stored in summaries.content (docs/PLAN.md §6).
type Summary struct {
	Title         string       `json:"title"`
	Overview      string       `json:"overview"`
	KeyPoints     []string     `json:"key_points"`
	Decisions     []string     `json:"decisions"`
	ActionItems   []ActionItem `json:"action_items"`
	OpenQuestions []string     `json:"open_questions"`
}

// Line is one transcript line with its time in the meeting.
type Line struct {
	At   time.Duration
	Text string
}

// Meta is context for the model.
type Meta struct {
	MeetingCode string
	Date        time.Time
	Duration    time.Duration
}

// Summarizer writes a Summary from transcript lines.
type Summarizer interface {
	Summarize(ctx context.Context, lines []Line, meta Meta) (Summary, error)
	Name() string
}

// FormatTranscript renders "[mm:ss] text" lines.
func FormatTranscript(lines []Line) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "[%s] %s\n", Clock(l.At), l.Text)
	}
	return b.String()
}

// Clock formats a meeting offset as mm:ss or h:mm:ss.
func Clock(d time.Duration) string {
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// Empty is the summary for a meeting where nothing was said.
func Empty() Summary {
	return Summary{
		Title:    "Untitled meeting",
		Overview: "No speech was captured in this meeting.",
	}
}

// normalize makes nil slices empty so the JSON always has arrays.
func normalize(s Summary) Summary {
	if s.KeyPoints == nil {
		s.KeyPoints = []string{}
	}
	if s.Decisions == nil {
		s.Decisions = []string{}
	}
	if s.ActionItems == nil {
		s.ActionItems = []ActionItem{}
	}
	if s.OpenQuestions == nil {
		s.OpenQuestions = []string{}
	}
	if strings.TrimSpace(s.Title) == "" {
		s.Title = "Untitled meeting"
	}
	return s
}
