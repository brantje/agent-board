package store

import (
	"context"
	"time"
)

const (
	NotificationKindCommentReply = "COMMENT_REPLY"
	NotificationKindIssueComment = "ISSUE_COMMENT"
)

// IssueSubscription is the durable per-User follow state for one Issue
// discussion. It is intentionally narrower than a general subscription
// system.
type IssueSubscription struct {
	ProjectID string
	IssueID   string
	UserID    string
	CreatedAt time.Time
}

// UserNotification is a read-time projection of a durable collaboration
// record. The comment body is never copied into the notification row.
type UserNotification struct {
	ID                string
	RecipientUserID   string
	ProjectID         string
	ProjectName       string
	IssueID           string
	IssueKey          string
	IssueTitle        string
	SourceCommentID   string
	CommentAuthorType string
	CommentAuthorID   string
	CommentAuthorName string
	Preview           string
	Kind              string
	CreatedAt         time.Time
	ReadAt            *time.Time
}

type UserNotificationPage struct {
	Notifications []UserNotification
	UnreadCount   int
}

// NotificationStore contains only the concrete Issue discussion notification
// and subscription operations required by the v0.1 inbox.
type NotificationStore interface {
	CreateIssueCommentNotifications(context.Context, string, string) error
	ListUserNotifications(context.Context, string) (UserNotificationPage, error)
	SetNotificationRead(context.Context, string, string, bool) error
	MarkAllNotificationsRead(context.Context, string) (int, error)
	GetIssueSubscription(context.Context, string, string, string) (bool, error)
	SetIssueSubscription(context.Context, string, string, string, bool) error
}
