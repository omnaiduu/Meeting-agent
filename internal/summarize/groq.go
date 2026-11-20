package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Groq summarizes with a Groq chat model using strict JSON-schema output.
type Groq struct {
	APIKey  string
	BaseURL string
	Model   string // e.g. openai/gpt-oss-120b
	HTTP    *http.Client
	// MaxChars per request before switching to map/reduce. ~4 chars per token;
	// 80k chars ≈ 20k tokens, far under the model's context, leaving room to think.
	MaxChars int
}

func (g *Groq) Name() string { return g.Model }

const systemPrompt = `You are a precise meeting note-taker. You get an automatic transcript of a meeting
(speech-to-text, so expect small recognition errors; speakers are not labeled).
Write notes for people who missed the meeting.

Rules:
- Use only what the transcript says. Never invent names, dates, owners or numbers.
- title: 3-8 words naming the main topic.
- overview: 3-5 sentences.
- key_points: the important facts and discussion points, one short sentence each.
- decisions: only things the group actually agreed on.
- action_items: concrete follow-ups. owner and due are null unless stated in the transcript.
- open_questions: things raised but not resolved.
- Empty arrays are fine when nothing fits.`

var schema = json.RawMessage(`{"type":"object","additionalProperties":false,
"required":["title","overview","key_points","decisions","action_items","open_questions"],
"properties":{
 "title":{"type":"string"},"overview":{"type":"string"},
 "key_points":{"type":"array","items":{"type":"string"}},
 "decisions":{"type":"array","items":{"type":"string"}},
 "action_items":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["task","owner","due"],
   "properties":{"task":{"type":"string"},"owner":{"type":["string","null"]},"due":{"type":["string","null"]}}}},
 "open_questions":{"type":"array","items":{"type":"string"}}}}`)

// Summarize writes the summary; long transcripts are summarized in parts first.
func (g *Groq) Summarize(ctx context.Context, lines []Line, meta Meta) (Summary, error) {
	if len(lines) == 0 {
		return Empty(), nil
	}
	parts := split(lines, g.maxChars())
	if len(parts) == 1 {
		return g.ask(ctx, header(meta)+"Transcript:\n"+FormatTranscript(lines))
	}
	// Map: summarize each part. Reduce: merge the partial notes into one summary.
	var notes strings.Builder
	for i, p := range parts {
		s, err := g.ask(ctx, fmt.Sprintf("%sThis is part %d of %d of a long meeting.\nTranscript:\n%s", header(meta), i+1, len(parts), FormatTranscript(p)))
		if err != nil {
			return Summary{}, fmt.Errorf("part %d: %w", i+1, err)
		}
		b, _ := json.Marshal(s)
		fmt.Fprintf(&notes, "Notes for part %d (from %s):\n%s\n\n", i+1, Clock(p[0].At), b)
	}
	return g.ask(ctx, header(meta)+"The meeting was long, so it was summarized in parts. Merge these partial notes into one summary of the whole meeting, removing duplicates:\n\n"+notes.String())
}

func header(m Meta) string {
	var b strings.Builder
	if !m.Date.IsZero() {
		fmt.Fprintf(&b, "Meeting date: %s\n", m.Date.Format("Monday, 2 January 2006"))
	}
	if m.Duration > 0 {
		fmt.Fprintf(&b, "Length: %s\n", Clock(m.Duration))
	}
	return b.String()
}

func split(lines []Line, max int) [][]Line {
	var parts [][]Line
	var cur []Line
	size := 0
	for _, l := range lines {
		n := len(l.Text) + 10
		if size+n > max && len(cur) > 0 {
			parts = append(parts, cur)
			cur, size = nil, 0
		}
		cur = append(cur, l)
		size += n
	}
	if len(cur) > 0 {
		parts = append(parts, cur)
	}
	return parts
}

func (g *Groq) maxChars() int {
	if g.MaxChars > 0 {
		return g.MaxChars
	}
	return 80_000
}

func (g *Groq) ask(ctx context.Context, user string) (Summary, error) {
	reqBody, _ := json.Marshal(map[string]any{
		"model": g.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": user},
		},
		"temperature": 0.2,
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "meeting_summary", "strict": true, "schema": schema},
		},
	})
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return Summary{}, ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.BaseURL+"/chat/completions", bytes.NewReader(reqBody))
		if err != nil {
			return Summary{}, err
		}
		req.Header.Set("Authorization", "Bearer "+g.APIKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := g.client().Do(req)
		if err != nil {
			last = err
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			last = fmt.Errorf("groq: HTTP %d: %.300s", resp.StatusCode, b)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return Summary{}, fmt.Errorf("groq: HTTP %d: %.300s", resp.StatusCode, b)
		}
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(b, &out); err != nil || len(out.Choices) == 0 {
			last = fmt.Errorf("groq: bad response: %.300s", b)
			continue
		}
		var s Summary
		if err := json.Unmarshal([]byte(out.Choices[0].Message.Content), &s); err != nil {
			last = fmt.Errorf("groq: summary is not valid JSON: %w", err)
			continue
		}
		return normalize(s), nil
	}
	return Summary{}, last
}

func (g *Groq) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return &http.Client{Timeout: 3 * time.Minute}
}
