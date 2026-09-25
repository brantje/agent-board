package runexec

import (
	"context"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

// AgentIssueCommentService is the shared application boundary used by Run
// execution to publish deliberate Agent-authored Issue collaboration.
type AgentIssueCommentService interface {
	PublishAgentIssueComment(context.Context, string, string, string, string, []string) (store.IssueComment, error)
}

type typedAgentIssueCommentService interface {
	PublishAgentIssueCommentTargets(context.Context, string, string, string, string, []store.IssueCommentTarget) (store.IssueComment, error)
}

type issueCommentPublisher struct {
	service AgentIssueCommentService
	safe    executioncontext.SafeContext
}

func newIssueCommentPublisher(service AgentIssueCommentService, safe executioncontext.SafeContext) engine.IssueCommentPublisher {
	if service == nil {
		return nil
	}
	return &issueCommentPublisher{service: service, safe: safe}
}

func (p *issueCommentPublisher) PublishIssueComment(ctx context.Context, request engine.IssueCommentPublishRequest) (engine.PublishedIssueComment, error) {
	if p == nil || p.service == nil {
		return engine.PublishedIssueComment{}, fmt.Errorf("run execution: Issue comment capability is unavailable")
	}
	request.Body = strings.TrimSpace(request.Body)
	request.RequestKey = strings.TrimSpace(request.RequestKey)
	if request.Body == "" || request.RequestKey == "" {
		return engine.PublishedIssueComment{}, fmt.Errorf("run execution: Issue comment body and request key are required")
	}
	var comment store.IssueComment
	var err error
	if len(request.MentionTargets) != 0 {
		typed, ok := p.service.(typedAgentIssueCommentService)
		if !ok {
			return engine.PublishedIssueComment{}, fmt.Errorf("run execution: typed Issue comment capability is unavailable")
		}
		comment, err = typed.PublishAgentIssueCommentTargets(ctx, p.safe.Project.ID, p.safe.Run.ID, request.RequestKey, request.Body, request.MentionTargets)
	} else {
		comment, err = p.service.PublishAgentIssueComment(ctx, p.safe.Project.ID, p.safe.Run.ID, request.RequestKey, request.Body, request.MentionAgentIDs)
	}
	if err != nil {
		return engine.PublishedIssueComment{}, err
	}
	published := engine.PublishedIssueComment{ID: comment.ID}
	for _, mention := range comment.Mentions {
		if mention.Outcome != store.IssueCommentMentionOutcomeQueued {
			continue
		}
		if mention.DelegationID == nil || mention.DelegatedRunID == nil {
			return engine.PublishedIssueComment{}, fmt.Errorf("run execution: queued Issue comment mention is missing delegation lineage")
		}
		if published.Delegation != nil {
			return engine.PublishedIssueComment{}, fmt.Errorf("run execution: Agent-authored Issue comment queued multiple delegations")
		}
		published.Delegation = &engine.Delegation{ID: *mention.DelegationID, RunID: *mention.DelegatedRunID}
	}
	return published, nil
}
