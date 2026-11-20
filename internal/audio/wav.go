package audio

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"sync"
)

// header returns a 44-byte PCM WAV header for dataLen bytes.
func header(dataLen int) []byte {
	var b bytes.Buffer
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + dataLen))
	b.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1)) // PCM
	w(uint16(1)) // mono
	w(uint32(SampleRate))
	w(uint32(BytesPerSecond))
	w(uint16(BytesPerSample))
	w(uint16(16))
	b.WriteString("data")
	w(uint32(dataLen))
	return b.Bytes()
}

// EncodeWAV wraps PCM in a WAV container (for uploading a chunk).
func EncodeWAV(pcm []byte) []byte {
	return append(header(len(pcm)), pcm...)
}

// Recorder streams PCM into a WAV file and fixes the header sizes on Close,
// so a crash leaves a playable (if under-reported) file.
type Recorder struct {
	mu sync.Mutex
	f  *os.File
	n  int
}

// NewRecorder creates path.
func NewRecorder(path string) (*Recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(header(0)); err != nil {
		f.Close()
		return nil, err
	}
	return &Recorder{f: f}, nil
}

// Write appends PCM.
func (r *Recorder) Write(pcm []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, err := r.f.Write(pcm)
	r.n += n
	return n, err
}

// Bytes written so far (PCM only).
func (r *Recorder) Bytes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// Close patches the header and closes the file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.f.Seek(0, io.SeekStart); err != nil {
		r.f.Close()
		return err
	}
	if _, err := r.f.Write(header(r.n)); err != nil {
		r.f.Close()
		return err
	}
	return r.f.Close()
}

// DecodeWAV returns the PCM data of a 16-bit WAV (used by tests and the fake meeting).
func DecodeWAV(b []byte) (pcm []byte, rate int, channels int, ok bool) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, 0, false
	}
	for i := 12; i+8 <= len(b); {
		id := string(b[i : i+4])
		size := int(binary.LittleEndian.Uint32(b[i+4:]))
		body := i + 8
		switch id {
		case "fmt ":
			if body+16 <= len(b) {
				channels = int(binary.LittleEndian.Uint16(b[body+2:]))
				rate = int(binary.LittleEndian.Uint32(b[body+4:]))
			}
		case "data":
			end := min(body+size, len(b))
			return b[body:end], rate, channels, true
		}
		i = body + size + size%2
	}
	return nil, 0, 0, false
}
