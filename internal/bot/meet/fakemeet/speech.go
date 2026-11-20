package fakemeet

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
)

// Speech returns a WAV of text spoken by espeak-ng if it is installed,
// otherwise a few seconds of tones (still real audio, just not words).
func Speech(text string) (wav []byte, spoken bool) {
	if p, err := exec.LookPath("espeak-ng"); err == nil {
		dir, err := os.MkdirTemp("", "fakemeet-")
		if err == nil {
			defer os.RemoveAll(dir)
			out := filepath.Join(dir, "s.wav")
			if exec.Command(p, "-s", "150", "-w", out, text).Run() == nil {
				if b, err := os.ReadFile(out); err == nil {
					return b, true
				}
			}
		}
	}
	return tones(), false
}

// tones: 4 s of beeps at 16 kHz.
func tones() []byte {
	const rate = 16000
	n := rate * 4
	pcm := make([]byte, n*2)
	for i := 0; i < n; i++ {
		on := (i/(rate/2))%2 == 0
		v := 0.0
		if on {
			v = 0.4 * math.Sin(2*math.Pi*440*float64(i)/rate)
		}
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(v*32767)))
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
	return append(h, pcm...)
}
