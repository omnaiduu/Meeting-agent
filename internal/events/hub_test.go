package events

import "testing"

func TestPublishSubscribe(t *testing.T) {
	h := NewHub()
	a, cancelA := h.Subscribe("m1")
	b, cancelB := h.Subscribe("m2")
	defer cancelB()
	h.Publish("m1", Event{Type: TypeStatus, Data: "in_call"})
	if e := <-a; e.Data != "in_call" {
		t.Fatal(e)
	}
	select {
	case e := <-b:
		t.Fatal("m2 got m1's event", e)
	default:
	}
	cancelA()
	if _, ok := <-a; ok {
		t.Fatal("channel should be closed")
	}
	cancelA() // idempotent
}

func TestSlowSubscriberDropped(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe("m")
	defer cancel()
	for i := 0; i < 100; i++ { // never read: buffer fills
		h.Publish("m", Event{Type: TypeTranscript})
	}
	if h.Subscribers("m") != 0 {
		t.Fatal("slow subscriber should be removed")
	}
	n := 0
	for range ch {
		n++
	}
	if n != 64 {
		t.Fatalf("buffered %d", n)
	}
}
