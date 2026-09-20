package engine

import (
	"context"
	"time"
)

const (
	IssueDiscussionReadRecent  = "recent"
	IssueDiscussionReadThread  = "thread"
	IssueDiscussionReadUpdates = "updates"
)

type IssueDiscussionReadRequest struct {
	Mode            string `json:"mode"`
	AnchorCommentID string `json:"anchorCommentId,omitempty"`
	Cursor          string `json:"cursor,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

type IssueDiscussionAuthor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type IssueDiscussionResolver struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type IssueDiscussionReaction struct {
	Reaction string `json:"reaction"`
	Count    int    `json:"count"`
}

type IssueDiscussionComment struct {
	ID              string                    `json:"id"`
	ParentCommentID *string                   `json:"parentCommentId"`
	SourceRunID     *string                   `json:"sourceRunId"`
	Author          IssueDiscussionAuthor     `json:"author"`
	Body            *string                   `json:"body"`
	DeletedAt       *time.Time                `json:"deletedAt"`
	ResolvedAt      *time.Time                `json:"resolvedAt"`
	ResolvedBy      *IssueDiscussionResolver  `json:"resolvedBy"`
	Reactions       []IssueDiscussionReaction `json:"reactions"`
	CreatedAt       time.Time                 `json:"createdAt"`
	UpdatedAt       time.Time                 `json:"updatedAt"`
}

type IssueDiscussionRoot struct {
	Root           IssueDiscussionComment `json:"root"`
	ReplyCount     int                    `json:"replyCount"`
	LastActivityAt time.Time              `json:"lastActivityAt"`
	Truncated      bool                   `json:"truncated"`
}

type IssueDiscussionThread struct {
	RootID    string                   `json:"rootId"`
	AnchorID  string                   `json:"anchorId"`
	Comments  []IssueDiscussionComment `json:"comments"`
	Truncated bool                     `json:"truncated"`
}

type IssueDiscussionUpdate struct {
	Comment IssueDiscussionComment `json:"comment"`
	IsNew   bool                   `json:"isNew"`
}

type IssueDiscussionUpdates struct {
	Comments   []IssueDiscussionUpdate `json:"comments"`
	NextCursor string                  `json:"nextCursor,omitempty"`
	HasMore    bool                    `json:"hasMore"`
}

type IssueDiscussionReadResult struct {
	Mode    string                  `json:"mode"`
	Roots   []IssueDiscussionRoot   `json:"roots,omitempty"`
	Thread  *IssueDiscussionThread  `json:"thread,omitempty"`
	Updates *IssueDiscussionUpdates `json:"updates,omitempty"`
}

// IssueDiscussionReader is the narrow trusted read capability available to an
// executing Agent. Project and Issue scope are always supplied by Agent Board,
// never by tool input.
type IssueDiscussionReader interface {
	ReadIssueDiscussion(context.Context, IssueDiscussionReadRequest) (IssueDiscussionReadResult, error)
}
