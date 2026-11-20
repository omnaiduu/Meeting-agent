package summarize

import (
	"context"
	"fmt"
	"strings"
)

// Fake builds a summary without an LLM (offline dev and tests).
type Fake struct{}

func (Fake) Name() string { return "fake" }

func (Fake) Summarize(ctx context.Context, lines []Line, meta Meta) (Summary, error) {
	if len(lines) == 0 {
		return Empty(), nil
	}
	var points []string
	for i, l := range lines {
		if i == 5 {
			break
		}
		points = append(points, l.Text)
	}
	first := strings.Fields(lines[0].Text)
	title := strings.Join(first[:min(5, len(first))], " ")
	return normalize(Summary{
		Title:     title,
		Overview:  fmt.Sprintf("Offline summary (no LLM configured) of %d transcript lines.", len(lines)),
		KeyPoints: points,
	}), nil
}
