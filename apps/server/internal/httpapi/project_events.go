package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
)

func (a *api) streamProjectEvents(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	afterID, ok := parseAfterID(w, r)
	if !ok {
		return
	}
	if a.eventHub == nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "streaming is unavailable")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "streaming is unavailable")
		return
	}

	live, unsubscribe := a.eventHub.SubscribeProject(r.Context(), projectID)
	defer unsubscribe()

	events, err := a.service.ListProjectEventsAfter(r.Context(), projectID, afterID)
	resync := false
	if err != nil {
		if apiErr, ok := app.AsError(err); ok && apiErr.Code == "replay_truncated" {
			resync = true
		} else {
			writeAppError(w, err)
			return
		}
	}
	if !a.projectStreamAccessCurrent(r, projectID) {
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	seen := make(map[string]struct{}, len(events))
	if resync {
		if err := writeProjectSSEResync(w, flusher); err != nil {
			return
		}
	} else {
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
			}
			if err := writeRunSSEEvent(w, flusher, event); err != nil {
				return
			}
		}
	}
}

func writeProjectSSEResync(w http.ResponseWriter, flusher http.Flusher) error {
	if _, err := fmt.Fprint(w, "event: resync\ndata: {}\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func parseAfterID(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := r.URL.Query().Get("afterId")
	if raw == "" {
		return "", true
	}
	if !validUUID(raw) {
		writeError(w, http.StatusBadRequest, "invalid_request", "afterId must be a UUID")
		return "", false
	}
	return raw, true
}
