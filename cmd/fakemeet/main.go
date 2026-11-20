// Command fakemeet runs a local imitation of Google Meet for demos and UI work.
// A scripted stand-up (several espeak voices) plays once the bot is admitted;
// then everyone "leaves" and the bot should leave too.
//
//	go run ./cmd/fakemeet                       # http://localhost:9090/abc-defg-hij
//	ALLOWED_MEETING_HOSTS=meet.google.com,localhost:9090 CHROME_HEADLESS=true go run ./cmd/server
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/omnaiduu/meeting-agent/internal/audio"
	"github.com/omnaiduu/meeting-agent/internal/bot/meet/fakemeet"
)

func main() {
	addr := flag.String("addr", "localhost:9090", "listen address")
	admit := flag.Duration("admit", 4*time.Second, "admit the bot after this long in the lobby")
	others := flag.Int("others", 3, "people in the call besides the bot")
	deny := flag.Bool("deny", false, "deny the join request instead of admitting (demo the failure state)")
	flag.Parse()

	wav, ok := fakemeet.Dialogue(fakemeet.DemoDialogue)
	if !ok {
		log.Print("espeak-ng not found: playing tones instead of speech")
		wav, _ = fakemeet.Speech("")
	}
	pcm, rate, _, _ := audio.DecodeWAV(wav)
	length := time.Duration(len(pcm)/2) * time.Second / time.Duration(max(rate, 1))
	s := fakemeet.Script{AdmitAfter: *admit, Deny: *deny, Others: *others, Speech: wav, AloneAfter: length + 3*time.Second}
	log.Printf("fake Meet on http://%s/abc-defg-hij (dialogue %s, everyone leaves after %s)", *addr, length.Round(time.Second), s.AloneAfter.Round(time.Second))
	log.Fatal(http.ListenAndServe(*addr, fakemeet.Handler(s)))
}
