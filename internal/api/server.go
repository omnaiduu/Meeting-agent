// Package api serves the REST API, the SSE stream and the embedded dashboard.
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/omnaiduu/meeting-agent/internal/meeting"
)

// Info is shown by the dashboard (which providers are active, default bot name).
type Info struct {
	BotName         string `json:"bot_name"`
	STTProvider     string `json:"stt_provider"`
	SummaryProvider string `json:"summary_provider"`
}

// Deps are the things the HTTP layer needs.
type Deps struct {
	Log       *slog.Logger
	DevCORS   bool
	Meetings  *meeting.Service
	Info      Info
	SPA       fs.FS         // built dashboard; nil = API only
	Heartbeat time.Duration // SSE keep-alive; default 15 s
}

type handlers struct {
	svc       *meeting.Service
	log       *slog.Logger
	heartbeat time.Duration
}

// NewHandler builds the full HTTP handler with middleware.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, d.Info)
	})
	if d.Meetings != nil {
		h := &handlers{svc: d.Meetings, log: d.Log, heartbeat: d.Heartbeat}
		if h.heartbeat == 0 {
			h.heartbeat = 15 * time.Second
		}
		mux.HandleFunc("POST /api/meetings", h.create)
		mux.HandleFunc("GET /api/meetings", h.list)
		mux.HandleFunc("GET /api/meetings/{id}", h.get)
		mux.HandleFunc("DELETE /api/meetings/{id}", h.delete)
		mux.HandleFunc("GET /api/meetings/{id}/transcript", h.transcript)
		mux.HandleFunc("POST /api/meetings/{id}/stop", h.stop)
		mux.HandleFunc("GET /api/meetings/{id}/audio", h.audio)
		mux.HandleFunc("GET /api/meetings/{id}/events", h.events)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "No such API endpoint.")
	})
	if d.SPA != nil {
		mux.Handle("/", spa(d.SPA))
	}

	var h http.Handler = mux
	if d.DevCORS {
		h = cors(h)
	}
	return recoverer(d.Log, logRequests(d.Log, h))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes {"error":{"code":..,"message":..}}.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach Flush on the real writer (needed for SSE).
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "dur", time.Since(start))
	})
}

func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic in handler", "err", v, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
