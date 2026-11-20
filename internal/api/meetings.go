package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/omnaiduu/meeting-agent/internal/events"
	"github.com/omnaiduu/meeting-agent/internal/meeting"
)

type createReq struct {
	MeetingURL string `json:"meeting_url"`
	BotName    string `json:"bot_name"`
}

func (h *handlers) create(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Body must be JSON: {\"meeting_url\": \"...\"}")
		return
	}
	m, err := h.svc.Create(r.Context(), req.MeetingURL, req.BotName)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 200 {
		limit = n
	}
	ms, err := h.svc.List(r.Context(), limit)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"meetings": ms})
}

func (h *handlers) get(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (h *handlers) transcript(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	segs, err := h.svc.Transcript(r.Context(), r.PathValue("id"), after)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"segments": segs})
}

func (h *handlers) stop(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Stop(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping"})
}

func (h *handlers) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) audio(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.AudioPath(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if _, err := os.Stat(p); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "No recording for this meeting.")
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="meeting-%s.wav"`, r.PathValue("id")))
	http.ServeFile(w, r, p)
}

// events streams a meeting's status, transcript and summary over SSE.
//
// On connect it sends the current status, then any transcript lines after
// Last-Event-ID (sent automatically by EventSource on reconnect, or ?after=),
// then the summary if there is one, then live events. Subscribing happens
// before reading the DB, and transcript events are de-duplicated by id, so
// nothing is lost or repeated in the gap between the two.
func (h *handlers) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ch, unsubscribe := h.svc.Subscribe(id)
	defer unsubscribe()

	m, err := h.svc.Get(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	var lastID int64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		lastID, _ = strconv.ParseInt(v, 10, 64)
	} else if v := r.URL.Query().Get("after"); v != "" {
		lastID, _ = strconv.ParseInt(v, 10, 64)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx: don't buffer the stream
	rc := http.NewResponseController(w)
	send := func(e events.Event) error {
		b, err := json.Marshal(e.Data)
		if err != nil {
			return err
		}
		if e.ID > 0 {
			fmt.Fprintf(w, "id: %d\n", e.ID)
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b); err != nil {
			return err
		}
		return rc.Flush()
	}

	st := meeting.StatusEvent{Status: m.Status, At: time.Now().UTC()}
	if m.ErrorCode != nil {
		st.ErrorCode = *m.ErrorCode
	}
	if m.ErrorMessage != nil {
		st.ErrorMessage = *m.ErrorMessage
	}
	fmt.Fprint(w, "retry: 3000\n\n")
	if err := send(events.Event{Type: events.TypeStatus, Data: st}); err != nil {
		return
	}
	segs, err := h.svc.Transcript(r.Context(), id, lastID)
	if err != nil {
		return
	}
	for _, s := range segs {
		if err := send(events.Event{Type: events.TypeTranscript, ID: s.ID, Data: s}); err != nil {
			return
		}
		lastID = s.ID
	}
	if m.Summary != nil {
		if err := send(events.Event{Type: events.TypeSummary, Data: m.Summary}); err != nil {
			return
		}
	}

	ping := time.NewTicker(h.heartbeat)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		case e, ok := <-ch:
			if !ok {
				return // we fell behind; the browser reconnects with Last-Event-ID
			}
			if e.Type == events.TypeTranscript {
				if e.ID <= lastID {
					continue
				}
				lastID = e.ID
			}
			if err := send(e); err != nil {
				return
			}
		}
	}
}

func (h *handlers) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, meeting.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Meeting not found.")
	case errors.Is(err, meeting.ErrInvalidURL):
		writeError(w, http.StatusBadRequest, "invalid_url", err.Error())
	case errors.Is(err, meeting.ErrNotActive):
		writeError(w, http.StatusConflict, "not_active", "No bot is running for this meeting.")
	default:
		h.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
}
