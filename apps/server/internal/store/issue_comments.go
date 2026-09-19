package store

import (
	"context"
	"time"
)

const (
	IssueTimelineKindComment  = "comment"
	IssueTimelineKindActivity = "activity"
)

// IssueComment is durable Issue-domain collaboration. AuthorName is a resolved
// presentation field and is never authoritative identity.
type IssueComment struct {
	ID              string
	IssueID         string
	ParentCommentID *string
	AuthorType      string
	AuthorID        string
	AuthorName      string
	Body            string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type IssueCommentMutationResult struct {
	Comment IssueComment
	Events  []Event
}

type IssueCommentStore interface {
	ListIssueComments(context.Context, string, string) ([]IssueComment, error)
	CreateIssueComment(context.Context, string, IssueComment) (IssueCommentMutationResult, error)
}

// IssueActivityStore exposes the existing durable Event history scoped to one
// Issue. It does not create another activity store or timeline persistence.
type IssueActivityStore interface {
	ListIssueEvents(context.Context, string, string) ([]Event, error)
}
