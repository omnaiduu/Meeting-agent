// Package audio handles the PCM stream coming out of the browser: put batches
// back in order, record them to a WAV file, cut them into chunks for
// speech-to-text, and skip silent chunks.
//
// Format everywhere: 16 kHz, mono, signed 16-bit little-endian PCM.
package audio

import (
	"encoding/binary"
	"math"
	"sync"
	"time"
)

const (
	SampleRate     = 16000
	BytesPerSample = 2
	BytesPerSecond = SampleRate * BytesPerSample
)

// Duration of n bytes of PCM.
func Duration(n int) time.Duration {
	return time.Duration(n/BytesPerSample) * time.Second / SampleRate
}

// Reorderer releases batches in sequence order. chromedp handles every call
// from the page in its own goroutine, so batch 7 can arrive before batch 6.
type Reorderer struct {
	mu      sync.Mutex
	next    int
	pending map[int][]byte
	out     func([]byte)
	// MaxGap: if this many later batches are waiting, give up on the missing one.
	MaxGap int
}

// NewReorderer calls out with batches in order.
func NewReorderer(out func([]byte)) *Reorderer {
	return &Reorderer{pending: map[int][]byte{}, out: out, MaxGap: 8}
}

// Push adds batch seq.
func (r *Reorderer) Push(seq int, pcm []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if seq < r.next {
		return // late duplicate or already skipped
	}
	r.pending[seq] = pcm
	r.drain()
	if len(r.pending) > r.MaxGap { // a batch got lost; skip ahead
		min := -1
		for s := range r.pending {
			if min < 0 || s < min {
				min = s
			}
		}
		r.next = min
		r.drain()
	}
}

func (r *Reorderer) drain() {
	for {
		b, ok := r.pending[r.next]
		if !ok {
			return
		}
		delete(r.pending, r.next)
		r.next++
		r.out(b)
	}
}

// Chunk is a slice of the meeting's audio with its position in the meeting.
type Chunk struct {
	Index  int
	Offset time.Duration // from the start of the recording
	PCM    []byte
}

// Duration of the chunk.
func (c Chunk) Duration() time.Duration { return Duration(len(c.PCM)) }

// Chunker cuts a continuous PCM stream into fixed-length chunks.
type Chunker struct {
	size  int
	buf   []byte
	total int // bytes emitted before buf
	index int
	emit  func(Chunk)
}

// NewChunker emits chunks of length d.
func NewChunker(d time.Duration, emit func(Chunk)) *Chunker {
	size := int(d.Seconds()*SampleRate) * BytesPerSample
	return &Chunker{size: size, emit: emit}
}

// Write appends PCM and emits every full chunk. Not safe for concurrent use.
func (c *Chunker) Write(pcm []byte) {
	c.buf = append(c.buf, pcm...)
	for len(c.buf) >= c.size {
		c.send(c.buf[:c.size])
		c.buf = c.buf[c.size:]
	}
}

// Flush emits whatever is left (the end of the meeting).
func (c *Chunker) Flush() {
	if len(c.buf) > 0 {
		c.send(c.buf)
		c.buf = nil
	}
}

func (c *Chunker) send(pcm []byte) {
	cp := append([]byte(nil), pcm...)
	c.emit(Chunk{Index: c.index, Offset: Duration(c.total), PCM: cp})
	c.total += len(cp)
	c.index++
}

// RMS of the PCM on a 0..1 scale.
func RMS(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) / 32768
		sum += v * v
	}
	return math.Sqrt(sum / float64(n))
}

// IsSilent reports whether no 100 ms window in pcm rises above threshold.
// Using windows (not the whole-chunk RMS) keeps one short "yes" from being dropped.
func IsSilent(pcm []byte, threshold float64) bool {
	win := SampleRate / 10 * BytesPerSample
	for i := 0; i < len(pcm); i += win {
		end := min(i+win, len(pcm))
		if RMS(pcm[i:end]) > threshold {
			return false
		}
	}
	return true
}

// DefaultSilence is the RMS threshold for "nobody is talking" (~-46 dBFS).
const DefaultSilence = 0.005
