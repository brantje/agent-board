package evidence

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
)

func TestRedactingStorePreservesQuestionCapability(t *testing.T) {
	var nilStore *RedactingStore
	if nilStore.SupportsQuestionStore() {
		t.Fatal("nil redacting store unexpectedly supports Questions")
	}

	unsupported := NewRedactingStore(&noQuestionControlPlane{}, redaction.NewRegistry())
	if unsupported.SupportsQuestionStore() {
		t.Fatal("decorator must preserve missing QuestionStore capability")
	}
	if nested := NewRedactingStore(unsupported, redaction.NewRegistry()); nested.SupportsQuestionStore() {
		t.Fatal("nested decorator must preserve missing QuestionStore capability")
	}

	supported := NewRedactingStore(&questionRedactionStore{}, redaction.NewRegistry())
	if !supported.SupportsQuestionStore() {
		t.Fatal("decorator must preserve available QuestionStore capability")
	}
}
