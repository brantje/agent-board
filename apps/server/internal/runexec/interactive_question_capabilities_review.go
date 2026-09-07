package runexec

import "github.com/brantje/agent-board/apps/server/internal/engine"

// interactiveQuestionerWithoutReplyTracking preserves the interactive and batch
// capabilities while intentionally hiding InteractiveQuestionReplyTracker when
// durable run-event reads are unavailable.
type interactiveQuestionerWithoutReplyTracking struct {
	engine.InteractiveQuestioner
	engine.InteractiveQuestionBatcher
}

func exposeInteractiveQuestioner(questioner *interactiveQuestioner) engine.InteractiveQuestioner {
	if questioner == nil || questioner.eventReader != nil {
		return questioner
	}
	return interactiveQuestionerWithoutReplyTracking{
		InteractiveQuestioner: questioner,
		InteractiveQuestionBatcher: questioner,
	}
}

var _ engine.InteractiveQuestioner = interactiveQuestionerWithoutReplyTracking{}
var _ engine.InteractiveQuestionBatcher = interactiveQuestionerWithoutReplyTracking{}
