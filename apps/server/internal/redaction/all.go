package redaction

import "encoding/json"

func (r *Registry) RedactAllJSON(raw json.RawMessage) (json.RawMessage, error) {
	values := r.AllValues()
	if len(raw) == 0 || len(values) == 0 {
		return append(json.RawMessage(nil), raw...), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	value = redactJSONValue(value, values)
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
