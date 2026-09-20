package runexec

import (
	"context"
	"fmt"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type AgentIssueDiscussionService interface {
	ListRecentIssueDiscussions(context.Context, string, string, int) ([]store.IssueDiscussionRoot, error)
	GetIssueDiscussionThread(context.Context, string, string, string, int) (store.IssueDiscussionThread, error)
	ListIssueDiscussionUpdates(context.Context, string, string, string, int) (app.IssueDiscussionUpdatePage, error)
}

type issueDiscussionReader struct {
	service AgentIssueDiscussionService
	safe    executioncontext.SafeContext
}

func newIssueDiscussionReader(service AgentIssueDiscussionService, safe executioncontext.SafeContext) engine.IssueDiscussionReader {
	if service == nil {
		return nil
	}
	return &issueDiscussionReader{service: service, safe: safe}
}

func (r *issueDiscussionReader) ReadIssueDiscussion(ctx context.Context, request engine.IssueDiscussionReadRequest) (engine.IssueDiscussionReadResult, error) {
	if r == nil || r.service == nil {
		return engine.IssueDiscussionReadResult{}, fmt.Errorf("run execution: Issue discussion capability is unavailable")
	}
	mode := strings.TrimSpace(request.Mode)
	switch mode {
	case engine.IssueDiscussionReadRecent:
		values, err := r.service.ListRecentIssueDiscussions(ctx, r.safe.Project.ID, r.safe.Issue.ID, request.Limit)
		if err != nil {
			return engine.IssueDiscussionReadResult{}, err
		}
		roots := make([]engine.IssueDiscussionRoot, 0, len(values))
		for _, value := range values {
			roots = append(roots, engine.IssueDiscussionRoot{
				Root: mapEngineIssueDiscussionComment(value.Root), ReplyCount: value.ReplyCount,
				LastActivityAt: value.LastActivityAt, Truncated: value.Truncated,
			})
		}
		return engine.IssueDiscussionReadResult{Mode: mode, Roots: roots}, nil
	case engine.IssueDiscussionReadThread:
		anchor := strings.TrimSpace(request.AnchorCommentID)
		if anchor == "" {
			return engine.IssueDiscussionReadResult{}, fmt.Errorf("run execution: thread read requires anchor comment id")
		}
		value, err := r.service.GetIssueDiscussionThread(ctx, r.safe.Project.ID, r.safe.Issue.ID, anchor, request.Limit)
		if err != nil {
			return engine.IssueDiscussionReadResult{}, err
		}
		comments := make([]engine.IssueDiscussionComment, 0, len(value.Comments))
		for _, comment := range value.Comments {
			comments = append(comments, mapEngineIssueDiscussionComment(comment))
		}
		thread := engine.IssueDiscussionThread{RootID: value.RootID, AnchorID: value.AnchorID, Comments: comments, Truncated: value.Truncated}
		return engine.IssueDiscussionReadResult{Mode: mode, Thread: &thread}, nil
	case engine.IssueDiscussionReadUpdates:
		value, err := r.service.ListIssueDiscussionUpdates(ctx, r.safe.Project.ID, r.safe.Issue.ID, strings.TrimSpace(request.Cursor), request.Limit)
		if err != nil {
			return engine.IssueDiscussionReadResult{}, err
		}
		comments := make([]engine.IssueDiscussionUpdate, 0, len(value.Comments))
		for _, item := range value.Comments {
			comments = append(comments, engine.IssueDiscussionUpdate{Comment: mapEngineIssueDiscussionComment(item.Comment), IsNew: item.IsNew})
		}
		updates := engine.IssueDiscussionUpdates{Comments: comments, NextCursor: value.NextCursor, HasMore: value.HasMore}
		return engine.IssueDiscussionReadResult{Mode: mode, Updates: &updates}, nil
	default:
		return engine.IssueDiscussionReadResult{}, fmt.Errorf("run execution: unsupported Issue discussion read mode %q", mode)
	}
}

func mapEngineIssueDiscussionComment(value store.IssueComment) engine.IssueDiscussionComment {
	var body *string
	if value.DeletedAt == nil {
		bodyValue := value.Body
		body = &bodyValue
	}
	var resolvedBy *engine.IssueDiscussionResolver
	if value.ResolvedByUserID != nil {
		resolvedBy = &engine.IssueDiscussionResolver{ID: *value.ResolvedByUserID, Name: value.ResolvedByName}
	}
	reactions := make([]engine.IssueDiscussionReaction, 0, len(value.Reactions))
	for _, reaction := range value.Reactions {
		reactions = append(reactions, engine.IssueDiscussionReaction{Reaction: reaction.Reaction, Count: reaction.Count})
	}
	return engine.IssueDiscussionComment{
		ID: value.ID, ParentCommentID: value.ParentCommentID, SourceRunID: value.SourceRunID,
		Author: engine.IssueDiscussionAuthor{Type: value.AuthorType, ID: value.AuthorID, Name: value.AuthorName},
		Body: body, DeletedAt: value.DeletedAt, ResolvedAt: value.ResolvedAt, ResolvedBy: resolvedBy,
		Reactions: reactions, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}
