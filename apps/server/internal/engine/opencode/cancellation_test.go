package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestCancelNativeSessionRejectsPendingQuestionsAndInterruptsSession(t *testing.T) {
	rejected := map[string]int{}
	interrupts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/session/ses_1/question/que_open/reject":
			rejected["que_open"]++
			w.WriteHeader(http.StatusNoContent)
		case "POST /api/session/ses_1/question/que_answered/reject":
			rejected["que_answered"]++
			w.WriteHeader(http.StatusNoContent)
		case "POST /api/session/ses_1/question/que_replied/reject":
			rejected["que_replied"]++
			w.WriteHeader(http.StatusNoContent)
		case "POST /api/session/ses_1/interrupt":
			interrupts++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	native, err := client.New(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	state := newRunState("ses_1", nil, nil)
	state.nativeQuestions["que_open"] = &nativeQuestionState{}
	state.nativeQuestions["que_answered"] = &nativeQuestionState{answered: true}
	state.nativeQuestions["que_replied"] = &nativeQuestionState{answered: true, replied: true}

	parent, cancel := context.WithCancel(context.Background())
	cancel()
	cancelNativeSession(parent, native, "ses_1", state)

	if rejected["que_open"] != 1 || rejected["que_answered"] != 1 {
		t.Fatalf("pending rejects=%v", rejected)
	}
	if rejected["que_replied"] != 0 {
		t.Fatalf("already replied Question was rejected: %v", rejected)
	}
	if interrupts != 1 {
		t.Fatalf("interrupts=%d want 1", interrupts)
	}
}
