package runexec

import (
	"context"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type recordingAgentIssueCommentService struct {
	projectID  string
	runID      string
	requestKey string
	body            string
	mentionAgentIDs []string
	comment         store.IssueComment
	err             error
}

func (s *recordingAgentIssueCommentService) PublishAgentIssueComment(_ context.Context, projectID, runID, requestKey, body string, mentionAgentIDs []string) (store.IssueComment, error) {
	s.projectID, s.runID, s.requestKey, s.body = projectID, runID, requestKey, body
	s.mentionAgentIDs = append([]string(nil), mentionAgentIDs...)
	if s.err != nil {
		return store.IssueComment{}, s.err
	}
	if s.comment.ID != "" {
		return s.comment, nil
	}
	return store.IssueComment{ID: "comment-1"}, nil
}

func TestIssueCommentPublisherUsesTrustedRunContext(t *testing.T) {
	service := &recordingAgentIssueCommentService{}
	safe := executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Run:     executioncontext.RunContext{ID: "run-1"},
	}
	publisher := newIssueCommentPublisher(service, safe)
	if publisher == nil {
		t.Fatal("Issue comment publisher is unavailable")
	}
	result, err := publisher.PublishIssueComment(t.Context(), engine.IssueCommentPublishRequest{
		Body:            "  concise finding  ",
		RequestKey:      "  tool-call-1  ",
		MentionAgentIDs: []string{"agent-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "comment-1" || service.projectID != "project-1" || service.runID != "run-1" ||
		service.requestKey != "tool-call-1" || service.body != "concise finding" ||
		len(service.mentionAgentIDs) != 1 || service.mentionAgentIDs[0] != "agent-2" {
		t.Fatalf("result=%+v service=%+v", result, service)
	}
}

func TestIssueCommentPublisherRejectsUntrustedEmptyIntent(t *testing.T) {
	service := &recordingAgentIssueCommentService{}
	publisher := newIssueCommentPublisher(service, executioncontext.SafeContext{})
	for name, request := range map[string]engine.IssueCommentPublishRequest{
		"blank body": {RequestKey: "call-1"},
		"blank key":  {Body: "finding"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := publisher.PublishIssueComment(t.Context(), request); err == nil {
				t.Fatalf("%s unexpectedly succeeded", name)
			}
		})
	}
	if newIssueCommentPublisher(nil, executioncontext.SafeContext{}) != nil {
		t.Fatal("nil application service unexpectedly exposed comment capability")
	}
}

func TestIssueCommentPublisherReturnsQueuedMentionDelegationForWorkspaceHandoff(t *testing.T) {
	delegationID, childRunID := "delegation-1", "child-run-1"
	service := &recordingAgentIssueCommentService{comment: store.IssueComment{
		ID: "comment-1",
		Mentions: []store.IssueCommentMention{{
			Outcome: store.IssueCommentMentionOutcomeQueued,
			DelegationID: &delegationID, DelegatedRunID: &childRunID,
		}},
	}}
	publisher := newIssueCommentPublisher(service, executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Run: executioncontext.RunContext{ID: "run-1"},
	})
	result, err := publisher.PublishIssueComment(t.Context(), engine.IssueCommentPublishRequest{
		Body: "delegate through a structured mention", RequestKey: "tool-call-mention", MentionAgentIDs: []string{"agent-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Delegation == nil || result.Delegation.ID != delegationID || result.Delegation.RunID != childRunID {
		t.Fatalf("published result=%+v", result)
	}
}

func TestIssueCommentPublisherRejectsInvalidDurableMentionResults(t *testing.T) {
	if _, err := (*issueCommentPublisher)(nil).PublishIssueComment(t.Context(), engine.IssueCommentPublishRequest{
		Body: "body", RequestKey: "key",
	}); err == nil {
		t.Fatal("nil publisher unexpectedly succeeded")
	}

	sentinel := errors.New("publish failed")
	failing := newIssueCommentPublisher(&recordingAgentIssueCommentService{err: sentinel}, executioncontext.SafeContext{
		Project: executioncontext.ProjectContext{ID: "project-1"},
		Run: executioncontext.RunContext{ID: "run-1"},
	})
	if _, err := failing.PublishIssueComment(t.Context(), engine.IssueCommentPublishRequest{
		Body: "body", RequestKey: "key",
	}); !errors.Is(err, sentinel) {
		t.Fatalf("service error=%v want sentinel", err)
	}

	delegationID, runID := "delegation-1", "run-1"
	cases := []struct {
		name     string
		mentions []store.IssueCommentMention
	}{
		{
			name: "missing lineage",
			mentions: []store.IssueCommentMention{{
				Outcome: store.IssueCommentMentionOutcomeQueued,
			}},
		},
		{
			name: "multiple queued handoffs",
			mentions: []store.IssueCommentMention{
				{Outcome: store.IssueCommentMentionOutcomeBlocked},
				{Outcome: store.IssueCommentMentionOutcomeQueued, DelegationID: &delegationID, DelegatedRunID: &runID},
				{Outcome: store.IssueCommentMentionOutcomeQueued, DelegationID: &delegationID, DelegatedRunID: &runID},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			publisher := newIssueCommentPublisher(&recordingAgentIssueCommentService{comment: store.IssueComment{
				ID: "comment-1", Mentions: tc.mentions,
			}}, executioncontext.SafeContext{
				Project: executioncontext.ProjectContext{ID: "project-1"},
				Run: executioncontext.RunContext{ID: "parent-run"},
			})
			if _, err := publisher.PublishIssueComment(t.Context(), engine.IssueCommentPublishRequest{
				Body: "body", RequestKey: "key",
			}); err == nil {
				t.Fatal("invalid durable mention result unexpectedly succeeded")
			}
		})
	}
}
