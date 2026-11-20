package fakemeet

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
)

// Line is one utterance in a scripted conversation.
type Line struct {
	Voice string // espeak-ng voice, e.g. "en-us", "en-gb+f3"
	Text  string
	Pause float64 // seconds of silence after the line
}

// DemoDialogue is a short product-team stand-up used for demos and screenshots.
var DemoDialogue = []Line{
	{"en-us", "Morning everyone. Let's do a quick launch check-in for the mobile app.", 1.2},
	{"en-gb+f3", "Sure. The beta build passed QA yesterday, except one crash on older Android phones.", 1.0},
	{"en-us+m3", "I can take that crash. I think it's the image cache. I'll have a fix by Wednesday.", 1.2},
	{"en-us", "Great. So are we still shipping the public beta on Friday?", 1.0},
	{"en-gb+f3", "Yes, if the crash fix lands by Wednesday, Friday works.", 1.0},
	{"en-us", "Okay, decision: public beta ships Friday. Maria, can you write the release notes?", 1.0},
	{"en-gb+f3", "I'll have the release notes ready by Thursday afternoon.", 1.2},
	{"en-us+m3", "One open question. Do we turn on the new onboarding flow for everyone, or only ten percent of users?", 1.0},
	{"en-us", "Let's decide that after we see Thursday's numbers. Thanks all, that's it for today.", 2.0},
}

// Dialogue renders lines with espeak-ng into one 22.05 kHz mono WAV.
// Returns ok=false if espeak-ng is missing.
func Dialogue(lines []Line) (wav []byte, ok bool) {
	p, err := exec.LookPath("espeak-ng")
	if err != nil {
		return nil, false
	}
	dir, err := os.MkdirTemp("", "dialogue-")
	if err != nil {
		return nil, false
	}
	defer os.RemoveAll(dir)
	const rate = 22050
	var pcm []byte
	for i, l := range lines {
		out := filepath.Join(dir, "l.wav")
		if exec.Command(p, "-v", l.Voice, "-s", "155", "-w", out, l.Text).Run() != nil {
			return nil, false
		}
		b, err := os.ReadFile(out)
		if err != nil || len(b) < 44 {
			return nil, false
		}
		pcm = append(pcm, b[44:]...) // espeak writes a plain 44-byte header
		pcm = append(pcm, make([]byte, int(l.Pause*rate)*2)...)
		_ = i
	}
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(pcm)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], rate)
	binary.LittleEndian.PutUint32(h[28:], rate*2)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(pcm)))
	return append(h, pcm...), true
}
