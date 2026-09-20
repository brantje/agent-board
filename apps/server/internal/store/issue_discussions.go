package store

import (
	"context"
	"time"
)

// IssueCommentCursor is the durable chronological position used by incremental
// discussion reads. It does not depend on the referenced comment continuing to
// exist, so deleting a leaf comment cannot invalidate a previously issued cursor.
type IssueCommentCursor struct {
	CreatedAt time.Time
	ID        string
}

// IssueDiscussionRoot is a bounded orientation record for one top-level
// discussion. Root remains the canonical durable comment; the metadata is a
// read projection only. CompactComments is populated only for resolved roots
// and contains a bounded root-to-latest-activity path, not a durable summary.
type IssueDiscussionRoot struct {
	Root            IssueComment
	ReplyCount      int
	LastActivityAt  time.Time
	CompactComments []IssueComment
	Truncated       bool
}

// IssueDiscussionThread contains one complete bounded traversal from a resolved
// root. Truncated is true when more descendants exist beyond the returned
// comment budget.
type IssueDiscussionThread struct {
	RootID    string
	AnchorID  string
	Comments  []IssueComment
	Truncated bool
}

// IssueDiscussionComment marks whether a returned comment is newer than the
// caller cursor or ancestor context restored solely to keep replies coherent.
type IssueDiscussionComment struct {
	Comment IssueComment
	IsNew   bool
}

// IssueDiscussionUpdates is an incremental collaboration page. NextCursor only
// advances across new comments that are actually present in Comments.
type IssueDiscussionUpdates struct {
	Comments   []IssueDiscussionComment
	NextCursor *IssueCommentCursor
	HasMore    bool
}

// IssueDiscussionStore owns canonical thread membership and bounded discussion
// traversal. UI, Engine and future MCP adapters must reuse these reads rather
// than reconstructing comment graphs independently.
type IssueDiscussionStore interface {
	ListIssueDiscussionRoots(context.Context, string, string, int) ([]IssueDiscussionRoot, error)
	GetIssueDiscussionThread(context.Context, string, string, string, int, int) (IssueDiscussionThread, error)
	ListIssueDiscussionUpdates(context.Context, string, string, *IssueCommentCursor, int, int, int) (IssueDiscussionUpdates, error)
}
