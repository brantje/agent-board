package runner

import (
	"encoding/json"
)

const defaultMaxActiveSessions = 5

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

func mergeJSONObject(base json.RawMessage, patch map[string]any) json.RawMessage {
	current := map[string]any{}
	if len(base) > 0 {
		_ = json.Unmarshal(base, &current)
	}
	for key, value := range patch {
		current[key] = value
	}
	out, _ := json.Marshal(current)
	return out
}

func WithConfiguredMaxActiveSessions(capabilities json.RawMessage, sessions int) json.RawMessage {
	return mergeJSONObject(capabilities, map[string]any{
		"max_active_sessions":            sessions,
		"max_active_sessions_configured": true,
	})
}

func MergeObservedCapabilities(existing, observed json.RawMessage) json.RawMessage {
	existingMap := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &existingMap)
	}
	observedMap := map[string]any{}
	if len(observed) > 0 {
		_ = json.Unmarshal(observed, &observedMap)
	}
	configured, _ := existingMap["max_active_sessions_configured"].(bool)
	preservedSessions := existingMap["max_active_sessions"]
	for key, value := range observedMap {
		if key == "max_active_sessions" && configured {
			continue
		}
		existingMap[key] = value
	}
	if configured {
		existingMap["max_active_sessions"] = preservedSessions
		existingMap["max_active_sessions_configured"] = true
	}
	return mergeJSONObject(nil, existingMap)
}
