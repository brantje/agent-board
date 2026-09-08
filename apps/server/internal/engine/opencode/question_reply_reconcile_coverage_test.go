package opencode

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
)

func TestAcceptedReplyReconciliationSkipsForeignSessionAndPendingRequest(t *testing.T) {
	t.Run("foreign session", func(t *testing.T) {
		questions := &replyFinalizationQuestioner{
			accepted: []engine.AcceptedInteractiveQuestionReply{{
				QuestionID:     "canonical-foreign",
				CorrelationKey: "ses_other/que_1/0",
			}},
			resolved:     make(map[string]bool),
			resolveCalls: make(map[string]int),
		}
		state := &runState{sessionID: "ses_1", questions: questions}

		hadAccepted, err := state.reconcileAcceptedReplies(context.Background(), nil)
		if err != nil {
			t.Fatalf("reconcileAcceptedReplies() error=%v", err)
		}
		if hadAccepted {
			t.Fatal("foreign-session accepted binding was attributed to this Run")
		}
		if len(questions.resolveCalls) != 0 {
			t.Fatalf("foreign-session binding was resolved: %v", questions.resolveCalls)
		}
		if state.acceptedReplyResolveFailures == nil {
			t.Fatal("resolve failure tracker was not initialized")
		}
	})

	t.Run("native request still pending", func(t *testing.T) {
		questions := &replyFinalizationQuestioner{
			accepted: []engine.AcceptedInteractiveQuestionReply{{
				QuestionID:     "canonical-pending",
				CorrelationKey: "ses_1/que_1/0",
			}},
			resolved:     make(map[string]bool),
			resolveCalls: make(map[string]int),
		}
		state := newRunState("ses_1", questions, nil)
		pending := []client.QuestionRequest{{ID: "que_1", SessionID: "ses_1"}}

		hadAccepted, err := state.reconcileAcceptedReplies(context.Background(), pending)
		if err != nil {
			t.Fatalf("reconcileAcceptedReplies() error=%v", err)
		}
		if !hadAccepted {
			t.Fatal("same-session accepted binding was not detected")
		}
		if len(questions.resolveCalls) != 0 {
			t.Fatalf("binding resolved while native request was still pending: %v", questions.resolveCalls)
		}
	})
}
