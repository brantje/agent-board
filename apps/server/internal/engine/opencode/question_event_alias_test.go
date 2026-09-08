package opencode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestNativeQuestionEventNamesUseSameDecoder(t *testing.T) {
	state := newRunState("ses_1", nil, nil)
	for _, eventType := range []string{"question.asked", "question.v2.asked"} {
		err := state.handleEvent(context.Background(), nil, client.Event{
			Type:       eventType,
			Properties: json.RawMessage("{"),
		})
		if err == nil || !strings.Contains(err.Error(), "decode native Question event") {
			t.Fatalf("%s error=%v", eventType, err)
		}
	}
}
