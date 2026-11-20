package transcribe

import (
	"context"
	"fmt"

	"github.com/omnaiduu/meeting-agent/internal/audio"
)

// Fake returns a placeholder line per non-silent chunk. Used without an API
// key and in tests, so the whole app runs offline.
type Fake struct{}

func (Fake) Name() string { return "fake" }

func (Fake) Transcribe(ctx context.Context, wav []byte, lang string) (Result, error) {
	pcm, _, _, _ := audio.DecodeWAV(wav)
	d := audio.Duration(len(pcm))
	text := fmt.Sprintf("[fake transcript: %.0f s of audio]", d.Seconds())
	return Result{Text: text, Segments: []Segment{{Start: 0, End: d, Text: text}}}, nil
}
