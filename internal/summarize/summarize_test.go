package summarize

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroqMapReduce(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		json.Unmarshal(b, &req)
		user := req.Messages[1].Content
		if n == 3 && !strings.Contains(user, "Merge these partial notes") {
			t.Errorf("third call should merge, got %q", user[:80])
		}
		content, _ := json.Marshal(map[string]any{"title": "T", "overview": "O", "key_points": []string{"k"}, "decisions": []string{},
			"action_items": []map[string]any{{"task": "do", "owner": nil, "due": nil}}, "open_questions": []string{}})
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": string(content)}}}})
	}))
	defer srv.Close()

	g := &Groq{APIKey: "k", BaseURL: srv.URL, Model: "m", MaxChars: 60}
	lines := []Line{{0, "one two three four five"}, {time.Minute, "six seven eight nine ten"}, {2 * time.Minute, "eleven twelve"}}
	s, err := g.Summarize(context.Background(), lines, Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 { // 2 parts + merge
		t.Errorf("calls %d", calls.Load())
	}
	if s.Title != "T" || len(s.ActionItems) != 1 || s.ActionItems[0].Owner != nil {
		t.Errorf("%+v", s)
	}
}

func TestEmptyTranscriptSkipsLLM(t *testing.T) {
	g := &Groq{BaseURL: "http://invalid"}
	s, err := g.Summarize(context.Background(), nil, Meta{})
	if err != nil || s.Overview == "" {
		t.Fatal(s, err)
	}
}

func TestClock(t *testing.T) {
	if Clock(61*time.Second) != "01:01" || Clock(3725*time.Second) != "1:02:05" {
		t.Fatal(Clock(61*time.Second), Clock(3725*time.Second))
	}
}
