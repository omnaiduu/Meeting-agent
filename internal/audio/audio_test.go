package audio

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tone(seconds float64, amp float64) []byte {
	n := int(seconds * SampleRate)
	b := make([]byte, n*2)
	for i := 0; i < n; i++ {
		v := int16(amp * 32767 * math.Sin(2*math.Pi*440*float64(i)/SampleRate))
		binary.LittleEndian.PutUint16(b[2*i:], uint16(v))
	}
	return b
}

func TestReordererPutsBatchesInOrder(t *testing.T) {
	var got []byte
	r := NewReorderer(func(b []byte) { got = append(got, b...) })
	for _, seq := range []int{1, 0, 3, 2, 4} {
		r.Push(seq, []byte{byte(seq)})
	}
	if string(got) != string([]byte{0, 1, 2, 3, 4}) {
		t.Fatalf("got %v", got)
	}
}

func TestReordererSkipsLostBatch(t *testing.T) {
	var got []byte
	r := NewReorderer(func(b []byte) { got = append(got, b...) })
	r.MaxGap = 2
	for _, seq := range []int{0, 2, 3, 4} { // 1 never arrives
		r.Push(seq, []byte{byte(seq)})
	}
	if string(got) != string([]byte{0, 2, 3, 4}) {
		t.Fatalf("got %v", got)
	}
}

func TestChunkerOffsets(t *testing.T) {
	var chunks []Chunk
	c := NewChunker(10*time.Second, func(ch Chunk) { chunks = append(chunks, ch) })
	for i := 0; i < 25; i++ {
		c.Write(tone(1, 0.5))
	}
	c.Flush()
	if len(chunks) != 3 {
		t.Fatalf("want 3 chunks, got %d", len(chunks))
	}
	want := []time.Duration{0, 10 * time.Second, 20 * time.Second}
	for i, ch := range chunks {
		if ch.Offset != want[i] || ch.Index != i {
			t.Errorf("chunk %d: offset %v index %d", i, ch.Offset, ch.Index)
		}
	}
	if d := chunks[2].Duration(); d != 5*time.Second {
		t.Errorf("last chunk %v", d)
	}
}

func TestSilence(t *testing.T) {
	if !IsSilent(make([]byte, BytesPerSecond), DefaultSilence) {
		t.Error("zeros should be silent")
	}
	pcm := make([]byte, BytesPerSecond*5)
	copy(pcm[BytesPerSecond*2:], tone(0.2, 0.3)) // a short "yes" in 5 s of silence
	if IsSilent(pcm, DefaultSilence) {
		t.Error("short speech must not count as silence")
	}
}

func TestRecorderWritesPlayableWAV(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.wav")
	r, err := NewRecorder(p)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(tone(1, 0.5))
	r.Write(tone(0.5, 0.5))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	pcm, rate, ch, ok := DecodeWAV(b)
	if !ok || rate != SampleRate || ch != 1 || len(pcm) != BytesPerSecond*3/2 {
		t.Fatalf("ok=%v rate=%d ch=%d len=%d", ok, rate, ch, len(pcm))
	}
}
