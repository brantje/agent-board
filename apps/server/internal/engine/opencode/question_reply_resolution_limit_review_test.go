package opencode

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
)

type permanentResolveQuestioner struct {
	replyFinalizationQuestioner
}

func (q *permanentResolveQuestioner) Resolve(_ context.Context, questionID string) error {
	if q.resolveCalls == nil {
		q.resolveCalls = make(map[string]int)
	}
	q.resolveCalls[questionID]++
	return fmt.Errorf("permanent Resolve failure for %s", questionID)
}

func TestAcceptedNativeReplySurfacesPermanentResolutionFailureAtBound(t *testing.T) {
	questions := &permanentResolveQuestioner{replyFinalizationQuestioner: replyFinalizationQuestioner{
		accepted: []engine.AcceptedInteractiveQuestionReply{{
			QuestionID:     "canonical-1",
			CorrelationKey: "ses_1/que_1/0",
		}},
		resolved:     make(map[string]bool),
		resolveCalls: make(map[string]int),
	}}
	state := newRunState("ses_1", questions, nil)

	for attempt := 1; attempt <= acceptedReplyResolveFailureLimit; attempt++ {
		hadAccepted, err := state.reconcileAcceptedReplies(context.Background(), nil)
		if !hadAccepted {
			t.Fatalf("attempt %d did not report accepted binding", attempt)
		}
		if attempt < acceptedReplyResolveFailureLimit {
			if err != nil {
				t.Fatalf("attempt %d error=%v before retry bound", attempt, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("attempt %d unexpectedly succeeded at retry bound", attempt)
		}
		if !strings.Contains(err.Error(), "after 3 consecutive failures") || !strings.Contains(err.Error(), "permanent Resolve failure") {
			t.Fatalf("bounded resolution error=%v", err)
		}
	}

	if questions.resolveCalls["canonical-1"] != acceptedReplyResolveFailureLimit {
		t.Fatalf("resolve calls=%d want %d", questions.resolveCalls["canonical-1"], acceptedReplyResolveFailureLimit)
	}
	if failures := state.acceptedReplyResolveFailures["canonical-1"]; len(failures) != acceptedReplyResolveFailureLimit {
		t.Fatalf("recorded failures=%d want %d", len(failures), acceptedReplyResolveFailureLimit)
	}
}
