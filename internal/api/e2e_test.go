package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omnaiduu/meeting-agent/internal/api"
	"github.com/omnaiduu/meeting-agent/internal/bot"
	"github.com/omnaiduu/meeting-agent/internal/bot/meet"
	"github.com/omnaiduu/meeting-agent/internal/bot/meet/fakemeet"
	"github.com/omnaiduu/meeting-agent/internal/events"
	"github.com/omnaiduu/meeting-agent/internal/meeting"
	"github.com/omnaiduu/meeting-agent/internal/store"
	"github.com/omnaiduu/meeting-agent/internal/summarize"
	"github.com/omnaiduu/meeting-agent/internal/transcribe"
)

type sseEvent struct {
	Type, ID string
	Data     json.RawMessage
}

func readSSE(t *testing.T, body io.Reader, out chan<- sseEvent) {
	sc := bufio.NewScanner(body)
	var ev sseEvent
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			ev.Type = line[7:]
		case strings.HasPrefix(line, "id: "):
			ev.ID = line[4:]
		case strings.HasPrefix(line, "data: "):
			ev.Data = json.RawMessage(line[6:])
		case line == "":
			if ev.Type != "" {
				out <- ev
			}
			ev = sseEvent{}
		}
	}
	close(out)
}

// Full stack: HTTP → bot (headless Chrome) → fake Meet → STT → summary → SSE.
// Uses fake STT/summary unless E2E_GROQ=1 (then real Groq; needs GROQ_API_KEY).
func TestEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	if _, err := exec.LookPath("google-chrome"); err != nil && os.Getenv("CHROME_PATH") == "" {
		t.Skip("no Chrome")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	dir := t.TempDir()
	ctx := context.Background()

	wav, spoken := fakemeet.Speech("Good morning team. We agreed to launch the beta on Friday. Maria will send the release notes by Thursday.")
	fm := fakemeet.Start(fakemeet.Script{AdmitAfter: 2 * time.Second, Others: 2, Speech: wav, AloneAfter: 12 * time.Second})
	defer fm.Close()
	fmURL, _ := url.Parse(fm.MeetingURL())

	st, err := store.Open(ctx, filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var stt transcribe.Transcriber = transcribe.Fake{}
	var sum summarize.Summarizer = summarize.Fake{}
	real := os.Getenv("E2E_GROQ") == "1"
	if real {
		key := os.Getenv("GROQ_API_KEY")
		stt = &transcribe.Groq{APIKey: key, BaseURL: "https://api.groq.com/openai/v1", Model: "whisper-large-v3-turbo"}
		sum = &summarize.Groq{APIKey: key, BaseURL: "https://api.groq.com/openai/v1", Model: "openai/gpt-oss-120b"}
	}

	hub := events.NewHub()
	svc := meeting.New(meeting.Config{DataDir: dir, DefaultBotName: "Notetaker", AllowedHosts: []string{fmURL.Host}}, st, hub, log)
	mgr := bot.NewManager(bot.Config{
		MaxConcurrent: 1, LobbyTimeout: 30 * time.Second, AloneTimeout: 3 * time.Second, MaxDuration: time.Minute,
		ChunkSeconds: 10, DebugDir: filepath.Join(dir, "debug"), Browser: bot.BrowserOptions{Headless: true},
		WatchPoll: 500 * time.Millisecond,
	}, log, svc, stt, sum, map[string]func() bot.Platform{"google_meet": func() bot.Platform {
		m := meet.New(log)
		m.Poll = 200 * time.Millisecond
		return m
	}})
	svc.SetRunner(mgr)
	defer mgr.Shutdown(ctx)
	srv := httptest.NewServer(api.NewHandler(api.Deps{Log: log, Meetings: svc}))
	defer srv.Close()

	// Bad URL is rejected.
	resp, _ := http.Post(srv.URL+"/api/meetings", "application/json", strings.NewReader(`{"meeting_url":"https://zoom.us/j/1"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("zoom url: %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Post(srv.URL+"/api/meetings", "application/json", strings.NewReader(`{"meeting_url":"`+fm.MeetingURL()+`","bot_name":"E2E Bot"}`))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %v %v", err, resp.Status)
	}
	var m meeting.Meeting
	json.NewDecoder(resp.Body).Decode(&m)
	resp.Body.Close()

	sse, err := http.Get(srv.URL + "/api/meetings/" + m.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer sse.Body.Close()
	if ct := sse.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	evs := make(chan sseEvent, 100)
	go readSSE(t, sse.Body, evs)

	var statuses []string
	var lines []string
	var gotSummary bool
	deadline := time.After(90 * time.Second)
loop:
	for {
		select {
		case <-deadline:
			t.Fatalf("timeout; statuses so far %v", statuses)
		case e, ok := <-evs:
			if !ok {
				t.Fatal("stream closed")
			}
			switch e.Type {
			case "status":
				var s meeting.StatusEvent
				json.Unmarshal(e.Data, &s)
				if len(statuses) == 0 || statuses[len(statuses)-1] != string(s.Status) {
					statuses = append(statuses, string(s.Status))
				}
				if s.Status == bot.StatusFailed {
					t.Fatalf("failed: %s %s", s.ErrorCode, s.ErrorMessage)
				}
				if s.Status == bot.StatusDone {
					break loop
				}
			case "transcript":
				var seg meeting.Segment
				json.Unmarshal(e.Data, &seg)
				lines = append(lines, seg.Text)
			case "summary":
				gotSummary = true
			}
		}
	}
	t.Logf("statuses: %v", statuses)
	t.Logf("transcript: %q", lines)
	// The stream may connect after "queued" (or even "launching") has passed.
	want := "joining,in_lobby,in_call,leaving,transcribing,summarizing,done"
	if !strings.HasSuffix(strings.Join(statuses, ","), want) {
		t.Errorf("statuses %v\n want %v", statuses, want)
	}
	if len(lines) == 0 || !gotSummary {
		t.Fatalf("lines=%d summary=%v", len(lines), gotSummary)
	}
	if real && spoken && !strings.Contains(strings.ToLower(strings.Join(lines, " ")), "friday") {
		t.Errorf("real transcript missing 'friday': %q", lines)
	}

	// REST views agree with the stream.
	got, _ := http.Get(srv.URL + "/api/meetings/" + m.ID)
	var full meeting.Meeting
	json.NewDecoder(got.Body).Decode(&full)
	got.Body.Close()
	if full.Status != bot.StatusDone || full.Summary == nil || !full.HasAudio || full.JoinedAt == nil || full.EndReason == nil || *full.EndReason != "alone" {
		t.Errorf("meeting: %+v", full)
	}
	if real {
		b, _ := json.MarshalIndent(full.Summary, "", "  ")
		t.Logf("summary: %s", b)
	}

	// A reconnect with Last-Event-ID replays nothing it already had.
	req, _ := http.NewRequest("GET", srv.URL+"/api/meetings/"+m.ID+"/events", nil)
	req.Header.Set("Last-Event-ID", "999999")
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp2, err := http.DefaultClient.Do(req.WithContext(rctx))
	if err != nil {
		t.Fatal(err)
	}
	evs2 := make(chan sseEvent, 100)
	go readSSE(t, resp2.Body, evs2)
	for e := range evs2 {
		if e.Type == "transcript" {
			t.Error("replayed a transcript line after Last-Event-ID")
		}
	}

	// Audio download and delete.
	a, _ := http.Get(srv.URL + "/api/meetings/" + m.ID + "/audio")
	if a.StatusCode != 200 || a.Header.Get("Content-Type") != "audio/wav" {
		t.Errorf("audio: %d %s", a.StatusCode, a.Header.Get("Content-Type"))
	}
	a.Body.Close()
	dreq, _ := http.NewRequest("DELETE", srv.URL+"/api/meetings/"+m.ID, nil)
	d, _ := http.DefaultClient.Do(dreq)
	if d.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", d.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, "meetings", m.ID)); !os.IsNotExist(err) {
		t.Error("audio dir not removed")
	}
}
