package httpapi

import (
	"fmt"
	"net/http"
	"time"
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
	if err != nil {
		writeAppError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	seen := make(map[string]struct{}, len(events))
	for _, event := range events {
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
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-live:
			if !ok {
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
