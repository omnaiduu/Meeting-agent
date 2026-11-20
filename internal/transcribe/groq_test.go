package transcribe

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroqParsesSegmentsAndRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if r.FormValue("model") != "whisper-large-v3-turbo" || r.FormValue("response_format") != "verbose_json" {
			t.Errorf("form %v", r.MultipartForm.Value)
		}
		f, _, _ := r.FormFile("file")
		b, _ := io.ReadAll(f)
		if string(b) != "WAVDATA" {
			t.Errorf("file %q", b)
		}
		io.WriteString(w, `{"text":" Hi.","language":"English","segments":[{"start":0.5,"end":2.25,"text":" Hi.","no_speech_prob":0.01}]}`)
	}))
	defer srv.Close()

	g := &Groq{APIKey: "k", BaseURL: srv.URL, Model: "whisper-large-v3-turbo"}
	res, err := g.Transcribe(context.Background(), []byte("WAVDATA"), "en")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls %d, want retry after 429", calls.Load())
	}
	if len(res.Segments) != 1 || res.Segments[0].Start != 500*time.Millisecond || res.Segments[0].End != 2250*time.Millisecond {
		t.Fatalf("%+v", res.Segments)
	}
}

func TestGroqDoesNotRetry400(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":"bad"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	_, err := (&Groq{APIKey: "k", BaseURL: srv.URL, Model: "m"}).Transcribe(context.Background(), nil, "")
	if err == nil || !strings.Contains(err.Error(), "400") || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestClean(t *testing.T) {
	in := []Segment{{Text: " Thank you."}, {Text: " Ship it Friday. "}, {Text: "noise", NoSpeechProb: 0.9}, {Text: "  "}}
	out := Clean(in)
	if len(out) != 1 || out[0].Text != "Ship it Friday." {
		t.Fatalf("%+v", out)
	}
}
