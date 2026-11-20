package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"
)

// Groq calls Groq's OpenAI-compatible Whisper endpoint.
type Groq struct {
	APIKey  string
	BaseURL string // e.g. https://api.groq.com/openai/v1
	Model   string // e.g. whisper-large-v3-turbo
	HTTP    *http.Client
	// MaxAttempts on 429/5xx. Default 4.
	MaxAttempts int
}

func (g *Groq) Name() string { return "groq:" + g.Model }

type verboseJSON struct {
	Text     string `json:"text"`
	Language string `json:"language"`
	Segments []struct {
		Start        float64 `json:"start"`
		End          float64 `json:"end"`
		Text         string  `json:"text"`
		NoSpeechProb float64 `json:"no_speech_prob"`
	} `json:"segments"`
}

// Transcribe uploads one WAV and returns segments with timestamps.
func (g *Groq) Transcribe(ctx context.Context, wav []byte, lang string) (Result, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("model", g.Model)
	_ = mw.WriteField("response_format", "verbose_json")
	_ = mw.WriteField("temperature", "0")
	if lang != "" {
		_ = mw.WriteField("language", lang)
	}
	fw, err := mw.CreateFormFile("file", "chunk.wav")
	if err != nil {
		return Result{}, err
	}
	fw.Write(wav)
	mw.Close()

	raw, err := doWithRetry(ctx, g.client(), g.attempts(), func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.BaseURL+"/audio/transcriptions", bytes.NewReader(body.Bytes()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+g.APIKey)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		return req, nil
	})
	if err != nil {
		return Result{}, err
	}
	var v verboseJSON
	if err := json.Unmarshal(raw, &v); err != nil {
		return Result{}, fmt.Errorf("decode transcription: %w", err)
	}
	res := Result{Text: v.Text, Language: v.Language}
	for _, s := range v.Segments {
		res.Segments = append(res.Segments, Segment{
			Start: secs(s.Start), End: secs(s.End), Text: s.Text, NoSpeechProb: s.NoSpeechProb,
		})
	}
	return res, nil
}

func (g *Groq) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

func (g *Groq) attempts() int {
	if g.MaxAttempts > 0 {
		return g.MaxAttempts
	}
	return 4
}

func secs(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }

// APIError is a non-2xx response.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("groq: HTTP %d: %s", e.Status, e.Body) }

// doWithRetry retries 429 and 5xx with exponential backoff, honoring Retry-After.
func doWithRetry(ctx context.Context, c *http.Client, attempts int, mk func() (*http.Request, error)) ([]byte, error) {
	var last error
	for i := 0; i < attempts; i++ {
		req, err := mk()
		if err != nil {
			return nil, err
		}
		resp, err := c.Do(req)
		var wait time.Duration
		if err != nil {
			last = err
		} else {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
			resp.Body.Close()
			if resp.StatusCode/100 == 2 {
				return b, nil
			}
			last = &APIError{Status: resp.StatusCode, Body: truncate(string(b), 300)}
			if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
				return nil, last // 4xx other than 429 won't fix itself
			}
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
				wait = time.Duration(s) * time.Second
			}
		}
		if i == attempts-1 {
			break
		}
		if wait == 0 {
			wait = time.Duration(1<<i)*time.Second + time.Duration(rand.IntN(500))*time.Millisecond
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, last
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
