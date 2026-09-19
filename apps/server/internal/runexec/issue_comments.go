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
	PublishAgentIssueComment(context.Context, string, string, string, string) (store.IssueComment, error)
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
	comment, err := p.service.PublishAgentIssueComment(
		ctx,
		p.safe.Project.ID,
		p.safe.Run.ID,
		request.RequestKey,
		request.Body,
	)
	if err != nil {
		return engine.PublishedIssueComment{}, err
	}
	return engine.PublishedIssueComment{ID: comment.ID}, nil
}
