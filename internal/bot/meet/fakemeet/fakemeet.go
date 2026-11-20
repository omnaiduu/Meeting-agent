// Package fakemeet serves a small imitation of the Google Meet web UI for tests.
//
// We can't reach meet.google.com from CI (or this dev sandbox), so the bot is
// tested against this page instead. It copies the parts the bot relies on:
// the same aria-labels and texts on the pre-join screen, a lobby, the in-call
// toolbar and participant tiles, and real WebRTC remote audio (a loopback
// RTCPeerConnection pair inside the page that plays a speech clip). It also
// copies Meet's traps: the lobby shows a "Leave call" button and a "Return to
// home screen" button, and a pop-up must be dismissed before joining.
package fakemeet

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

//go:embed page.html
var pageHTML string

// Script describes how the fake meeting behaves after the bot asks to join.
type Script struct {
	AdmitAfter time.Duration // 0 = never admit automatically
	Deny       bool          // deny instead of admitting
	// Participants in the call besides the bot.
	Others int
	// AloneAfter: after this long in the call, everyone else leaves (count drops to 1, audio stops).
	AloneAfter time.Duration
	// EndAfter: after this long in the call, the call ends for everyone.
	EndAfter time.Duration
	// Speech is a WAV file played as remote audio once admitted (looped if Loop).
	Speech []byte
	Loop   bool
}

// Server is a running fake Meet.
type Server struct {
	*httptest.Server
	script Script

	mu       sync.Mutex
	knocked  string
	knockAt  time.Time
	state    string // prejoin, lobby, in_call, denied, ended, left
	joinedAt time.Time
}

// Start launches the fake Meet on a random local port.
func Start(s Script) *Server {
	f := &Server{script: s, state: "prejoin"}
	f.Server = httptest.NewServer(f.handler())
	return f
}

// Handler returns a fake Meet as a plain handler (for a long-running demo server).
func Handler(s Script) http.Handler {
	f := &Server{script: s, state: "prejoin"}
	return f.handler()
}

func (f *Server) handler() http.Handler {
	s := f.script
	mux := http.NewServeMux()
	mux.HandleFunc("GET /speech.wav", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(s.Speech)
	})
	mux.HandleFunc("POST /api/knock", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.knocked, f.knockAt, f.state = body.Name, time.Now(), "lobby"
		f.mu.Unlock()
	})
	mux.HandleFunc("POST /api/leave", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.state = "left"
		f.mu.Unlock()
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(f.snapshot())
	})
	mux.HandleFunc("GET /{code}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		if f.state != "lobby" && f.state != "in_call" {
			f.state = "prejoin" // a new visit starts a new meeting (demo server)
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// A strict CSP like Meet's, so the bot's CSP bypass is exercised too.
		w.Header().Set("Content-Security-Policy", "script-src 'self' 'unsafe-inline'; worker-src 'none'; require-trusted-types-for 'script'")
		w.Write([]byte(strings.ReplaceAll(pageHTML, "{{CODE}}", r.PathValue("code"))))
	})
	return mux
}

// MeetingURL returns a Meet-like URL on the fake server.
func (f *Server) MeetingURL() string { return f.URL + "/abc-defg-hij" }

// Knocked returns the name the bot typed, if it asked to join.
func (f *Server) Knocked() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.knocked
}

// State returns the server-side state (prejoin, lobby, in_call, denied, ended, left).
func (f *Server) State() string { return f.snapshot().State }

type snapshot struct {
	State        string `json:"state"`
	Participants int    `json:"participants"`
	AudioOn      bool   `json:"audioOn"`
	Loop         bool   `json:"loop"`
}

func (f *Server) snapshot() snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.script
	now := time.Now()
	if f.state == "lobby" && s.AdmitAfter > 0 && now.Sub(f.knockAt) >= s.AdmitAfter {
		if s.Deny {
			f.state = "denied"
		} else {
			f.state, f.joinedAt = "in_call", now
		}
	}
	out := snapshot{State: f.state, Participants: s.Others + 1, AudioOn: true, Loop: s.Loop}
	if f.state == "in_call" {
		in := now.Sub(f.joinedAt)
		if s.EndAfter > 0 && in >= s.EndAfter {
			f.state = "ended"
			out.State = "ended"
		} else if s.AloneAfter > 0 && in >= s.AloneAfter {
			out.Participants, out.AudioOn = 1, false
		}
	}
	return out
}
