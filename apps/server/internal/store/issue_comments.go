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

	IssueCommentTargetTypeAgent = "AGENT"
	IssueCommentTargetTypeSquad = "SQUAD"

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

// IssueCommentTarget identifies the durable collaboration target selected for
// a comment. A Squad target is resolved to an Agent only when work is
// dispatched; the Squad identity remains the target for history and display.
type IssueCommentTarget struct {
	Type string
	ID   string
}

func (t IssueCommentTarget) Valid() bool {
	return (t.Type == IssueCommentTargetTypeAgent || t.Type == IssueCommentTargetTypeSquad) && t.ID != ""
}

type IssueCommentMention struct {
	ID                string
	Target            IssueCommentTarget
	TargetName        string
	ResolvedAgentID   string
	ResolvedAgentName string
	TargetAgentID     string
	TargetAgentName   string
	Outcome           string
	ReasonCode        *string
	DelegationID      *string
	DelegatedRunID    *string
	WorkRequestID     *string
	CreatedAt         time.Time
}

type IssueCommentImplicitTrigger struct {
	Target            IssueCommentTarget
	TargetName        string
	ResolvedAgentID   string
	ResolvedAgentName string
	TargetAgentID     string
	TargetAgentName   string
	RoutingReason     string
	Outcome           string
	ReasonCode        *string
	DelegationID      *string
	DelegatedRunID    *string
	WorkRequestID     *string
	CreatedAt         time.Time
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
	Target            IssueCommentTarget
	TargetName        string
	ResolvedAgentID   string
	ResolvedAgentName string
	TargetAgentID     string
	TargetAgentName   string
	Eligible          bool
	ReasonCode        *string
}

type IssueCommentImplicitTriggerPreview struct {
	Target            IssueCommentTarget
	TargetName        string
	ResolvedAgentID   string
	ResolvedAgentName string
	TargetAgentID     string
	TargetAgentName   string
	RoutingReason     string
	Eligible          bool
	Suppressed        bool
	ReasonCode        *string
}

type IssueCommentTriggerRequest struct {
	MentionTargets   []IssueCommentTarget
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

// TypedIssueCommentMentionStore is the Squad-aware extension of the original
// Agent-only comment store contract. The legacy methods remain available for
// compatibility with existing test doubles and older callers.
type TypedIssueCommentMentionStore interface {
	CreateIssueCommentWithTargets(context.Context, string, IssueComment, []IssueCommentTarget) (IssueCommentMutationResult, error)
	PreviewIssueCommentTargets(context.Context, string, string, []IssueCommentTarget) ([]IssueCommentMentionPreview, error)
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
