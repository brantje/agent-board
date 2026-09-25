package store

import (
	"context"
	"time"
)

const (
	IssueTimelineKindComment  = "comment"
	IssueTimelineKindActivity = "activity"

	IssueCommentReactionThumbsUp   = "THUMBS_UP"
	IssueCommentReactionThumbsDown = "THUMBS_DOWN"
	IssueCommentReactionLaugh      = "LAUGH"
	IssueCommentReactionHooray     = "HOORAY"
	IssueCommentReactionConfused   = "CONFUSED"
	IssueCommentReactionHeart      = "HEART"
	IssueCommentReactionRocket     = "ROCKET"
	IssueCommentReactionEyes       = "EYES"

	IssueCommentMentionOutcomeQueued    = "QUEUED"
	IssueCommentMentionOutcomeCoalesced = "COALESCED"
	IssueCommentMentionOutcomeDeferred  = "DEFERRED"
	IssueCommentMentionOutcomeBlocked   = "BLOCKED"

	IssueCommentMentionReasonTargetUnavailable = "TARGET_UNAVAILABLE"
	IssueCommentMentionReasonTargetBusy        = "TARGET_BUSY"
	IssueCommentMentionReasonDelegationBlocked = "DELEGATION_BLOCKED"

	MaxIssueCommentMentions = 10
)

var issueCommentReactions = map[string]struct{}{
	IssueCommentReactionThumbsUp:   {},
	IssueCommentReactionThumbsDown: {},
	IssueCommentReactionLaugh:      {},
	IssueCommentReactionHooray:     {},
	IssueCommentReactionConfused:   {},
	IssueCommentReactionHeart:      {},
	IssueCommentReactionRocket:     {},
	IssueCommentReactionEyes:       {},
}

func ValidIssueCommentReaction(value string) bool {
	_, ok := issueCommentReactions[value]
	return ok
}

type IssueCommentReactionSummary struct {
	Reaction string
	Count    int
	ActorIDs []string
}

type IssueCommentMention struct {
	ID              string
	TargetAgentID   string
	TargetAgentName string
	Outcome         string
	ReasonCode      *string
	DelegationID    *string
	DelegatedRunID  *string
	WorkRequestID   *string
	CreatedAt       time.Time
}

type IssueCommentImplicitTrigger struct {
	TargetAgentID   string
	TargetAgentName string
	RoutingReason   string
	Outcome         string
	ReasonCode      *string
	DelegationID    *string
	DelegatedRunID  *string
	WorkRequestID   *string
	CreatedAt       time.Time
}

// IssueComment is durable Issue-domain collaboration. Presentation names are
// resolved fields and are never authoritative identity.
type IssueComment struct {
	ID               string
	IssueID          string
	ParentCommentID  *string
	AuthorType       string
	AuthorID         string
	AuthorName       string
	SourceRunID      *string
	SourceActionKey  *string
	Body             string
	DeletedAt        *time.Time
	ResolvedAt       *time.Time
	ResolvedByUserID *string
	ResolvedByName   string
	Reactions        []IssueCommentReactionSummary
	Mentions         []IssueCommentMention
	ImplicitTrigger  *IssueCommentImplicitTrigger
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type IssueCommentMutationResult struct {
	Comment IssueComment
	Events  []Event
}

type IssueCommentDeleteResult struct {
	Events []Event
}

type IssueCommentMentionPreview struct {
	TargetAgentID   string
	TargetAgentName string
	Eligible        bool
	ReasonCode      *string
}

type IssueCommentImplicitTriggerPreview struct {
	TargetAgentID   string
	TargetAgentName string
	RoutingReason   string
	Eligible        bool
	Suppressed      bool
	ReasonCode      *string
}

type IssueCommentTriggerRequest struct {
	MentionAgentIDs  []string
	SuppressImplicit bool
}

type IssueCommentTriggerPreview struct {
	Mentions []IssueCommentMentionPreview
	Implicit *IssueCommentImplicitTriggerPreview
}

type IssueCommentTriggerStore interface {
	CreateIssueCommentWithTriggers(context.Context, string, IssueComment, IssueCommentTriggerRequest) (IssueCommentMutationResult, error)
	PreviewIssueCommentTriggers(context.Context, string, string, *string, string, IssueCommentTriggerRequest) (IssueCommentTriggerPreview, error)
}

type IssueCommentMentionStore interface {
	CreateIssueCommentWithMentions(context.Context, string, IssueComment, []string) (IssueCommentMutationResult, error)
	PreviewIssueCommentMentions(context.Context, string, string, []string) ([]IssueCommentMentionPreview, error)
}

type IssueCommentStore interface {
	ListIssueComments(context.Context, string, string) ([]IssueComment, error)
	GetIssueComment(context.Context, string, string, string) (IssueComment, error)
	CreateIssueComment(context.Context, string, IssueComment) (IssueCommentMutationResult, error)
	UpdateIssueComment(context.Context, string, string, string, string, string) (IssueCommentMutationResult, error)
	DeleteIssueComment(context.Context, string, string, string, string) (IssueCommentDeleteResult, error)
	ResolveIssueComment(context.Context, string, string, string, string) (IssueCommentMutationResult, error)
	ReopenIssueComment(context.Context, string, string, string, string) (IssueCommentMutationResult, error)
	AddIssueCommentReaction(context.Context, string, string, string, string, string) ([]Event, error)
	RemoveIssueCommentReaction(context.Context, string, string, string, string, string) ([]Event, error)
}

// IssueActivityStore exposes the existing durable Event history scoped to one
// Issue. It does not create another activity store or timeline persistence.
type IssueActivityStore interface {
	ListIssueTimelineEvents(context.Context, string, string) ([]Event, error)
}
