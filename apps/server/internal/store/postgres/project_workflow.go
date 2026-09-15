package postgres

import "encoding/json"

const strictOrderSetting = "strictOrder"

func newProjectWorkflowSettings(value json.RawMessage) json.RawMessage {
	value = objectJSON(value)
	settings := make(map[string]any)
	if err := json.Unmarshal(value, &settings); err != nil || settings == nil {
		return value
	}
	if _, exists := settings[strictOrderSetting]; exists {
		return value
	}
	settings[strictOrderSetting] = true
	encoded, err := json.Marshal(settings)
	if err != nil {
		return value
	}
	return encoded
}
