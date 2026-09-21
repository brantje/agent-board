package postgres

import (
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

func TestSameIssueCommentImplicitRequest(t *testing.T) {
	suppressed := store.IssueComment{
		ImplicitTrigger: &store.IssueCommentImplicitTrigger{Outcome: store.IssueCommentImplicitOutcomeSuppressed},
	}
	queued := store.IssueComment{
		ImplicitTrigger: &store.IssueCommentImplicitTrigger{Outcome: store.IssueCommentImplicitOutcomeQueued},
	}

	tests := []struct {
		name                string
		existing            store.IssueComment
		enabled             bool
		suppress            bool
		persistedSuppress   bool
		hasExplicitMentions bool
		want                bool
	}{
		{name: "disabled accepts no trigger", want: true},
		{name: "disabled rejects persisted suppression", persistedSuppress: true},
		{name: "disabled rejects persisted trigger", existing: queued},
		{name: "explicit mentions accept normalized false suppression", enabled: true, hasExplicitMentions: true, want: true},
		{name: "explicit mentions reject persisted suppression", enabled: true, persistedSuppress: true, hasExplicitMentions: true},
		{name: "explicit mentions reject implicit trigger", existing: queued, enabled: true, hasExplicitMentions: true},
		{name: "enabled accepts unresolved route with matching false intent", enabled: true, want: true},
		{name: "enabled rejects changed suppression without route", enabled: true, suppress: true},
		{name: "enabled accepts suppressed unresolved route", enabled: true, suppress: true, persistedSuppress: true, want: true},
		{name: "suppressed replay matches suppressed outcome", existing: suppressed, enabled: true, suppress: true, persistedSuppress: true, want: true},
		{name: "suppressed replay rejects queued outcome", existing: queued, enabled: true, suppress: true, persistedSuppress: true},
		{name: "normal replay matches queued outcome", existing: queued, enabled: true, want: true},
		{name: "normal replay rejects suppressed outcome", existing: suppressed, enabled: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sameIssueCommentImplicitRequest(test.existing, test.enabled, test.suppress, test.persistedSuppress, test.hasExplicitMentions); got != test.want {
				t.Fatalf("sameIssueCommentImplicitRequest()=%v want %v", got, test.want)
			}
		})
	}
}

func TestLoadIssueCommentImplicitTriggersWithEmptyInputDoesNotQuery(t *testing.T) {
	if err := loadIssueCommentImplicitTriggersWith(t.Context(), nil, "project", "issue", nil); err != nil {
		t.Fatalf("empty load error=%v", err)
	}
}

func TestPreviewIssueCommentTriggersRejectsInvalidScopeBeforeOpeningTransaction(t *testing.T) {
	s := &Store{}
	if _, err := s.PreviewIssueCommentTriggers(t.Context(), "", "issue", nil, "", store.IssueCommentTriggerRequest{}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("empty project error=%v", err)
	}
	blankParent := " "
	if _, err := s.PreviewIssueCommentTriggers(t.Context(), "project", "issue", &blankParent, "", store.IssueCommentTriggerRequest{}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("blank parent error=%v", err)
	}
}
