package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const defaultRunEventHeartbeat = 15 * time.Second

// runEventHeartbeat is the SSE comment interval. Tests may shorten it.
var runEventHeartbeat = defaultRunEventHeartbeat

func (a *api) streamRunEvents(w http.ResponseWriter, r *http.Request) {
	projectID, runID, ok := evidenceRunPath(w, r)
	if !ok {
		return
	}
	afterSequence, ok := parseAfterSequence(w, r)
	if !ok {
		return
	}
	if _, err := a.runEvidence.RequireRun(r.Context(), projectID, runID); err != nil {
		writeAppError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "streaming is unavailable")
		return
	}

	live, unsubscribe := a.eventHub.Subscribe(r.Context(), runID)
	defer unsubscribe()
	if !a.projectStreamAccessCurrent(r, projectID) {
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, err := a.runEvidence.ListEventsAfter(r.Context(), projectID, runID, afterSequence)
	if err != nil {
		return
	}
	seen := make(map[string]struct{}, len(events))
	for _, event := range events {
		if !a.projectStreamAccessCurrent(r, projectID) {
			return
		}
		if err := writeRunSSEEvent(w, flusher, event); err != nil {
			return
		}
		if event.ID != "" {
			seen[event.ID] = struct{}{}
		}
	}

	heartbeat := runEventHeartbeat
	if heartbeat <= 0 {
		heartbeat = defaultRunEventHeartbeat
	}
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !a.projectStreamAccessCurrent(r, projectID) {
				return
			}
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-live:
			if !ok {
				return
			}
			if !a.projectStreamAccessCurrent(r, projectID) {
				return
			}
			if event.ID != "" {
				if _, dup := seen[event.ID]; dup {
					continue
				}
				seen[event.ID] = struct{}{}
			}
			if err := writeRunSSEEvent(w, flusher, event); err != nil {
				return
			}
		}
	}
}

func parseAfterSequence(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("afterSequence")
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "afterSequence must be a non-negative integer")
		return 0, false
	}
	return value, true
}

func writeRunSSEEvent(w http.ResponseWriter, flusher http.Flusher, event store.Event) error {
	payload, err := json.Marshal(eventEvidenceDTO(event))
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %s\ndata: %s\n\n", event.ID, payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
