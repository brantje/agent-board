package runner

import (
	"encoding/json"
)

const defaultMaxActiveSessions = 10

func MaxActiveSessions(capabilities json.RawMessage) int {
	if len(capabilities) == 0 {
		return defaultMaxActiveSessions
	}
	var payload struct {
		MaxActiveSessions int `json:"max_active_sessions"`
	}
	if err := json.Unmarshal(capabilities, &payload); err != nil {
		return defaultMaxActiveSessions
	}
	if payload.MaxActiveSessions > 0 {
		return payload.MaxActiveSessions
	}
	return defaultMaxActiveSessions
}
