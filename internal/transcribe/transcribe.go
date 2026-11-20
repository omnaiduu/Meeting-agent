// Package transcribe turns audio chunks into timestamped text.
package transcribe

import (
	"context"
	"strings"
	"time"
)

// Segment is one piece of recognized speech, relative to the start of the audio sent.
type Segment struct {
	Start, End   time.Duration
	Text         string
	NoSpeechProb float64
}

// Result of one transcription request.
type Result struct {
	Text     string
	Language string
	Segments []Segment
}

// Transcriber converts a WAV file (16 kHz mono) to text.
type Transcriber interface {
	Transcribe(ctx context.Context, wav []byte, lang string) (Result, error)
	Name() string
}

// hallucinations are phrases Whisper is known to invent on silence or noise
// (it was trained on lots of subtitled video). Only dropped when they are the
// whole segment.
var hallucinations = map[string]bool{
	"thank you.": true, "thank you": true, "thanks for watching!": true, "thanks for watching.": true,
	"thank you for watching.": true, "thank you for watching!": true, "you": true, ".": true,
	"bye.": true, "subtitles by the amara.org community": true, "please subscribe.": true,
}

// Clean drops empty segments, likely-silence segments and known hallucinations.
func Clean(segs []Segment) []Segment {
	out := segs[:0:0]
	for _, s := range segs {
		t := strings.TrimSpace(s.Text)
		if t == "" || s.NoSpeechProb > 0.6 || hallucinations[strings.ToLower(t)] {
			continue
		}
		s.Text = t
		out = append(out, s)
	}
	return out
}
