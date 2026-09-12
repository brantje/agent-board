package store

import "context"

type ReviewFilter struct {
	IssueID  *string
	Statuses []string
}

type BeginReviewApprovalCommand struct {
	ProjectID string
	ReviewID  string
	ActorID   *string
}

type BeginReviewApprovalResult struct {
	Review   Review
	Decision Decision
	Run      Run
}

type CompleteReviewApprovalCommand struct {
	ProjectID        string
	ReviewID         string
	AcceptedRevision string
	DeliveryComplete bool
}

type CompleteReviewApprovalResult struct {
	Review   Review
	Decision Decision
	Run      Run
	Issue    Issue
	Events   []Event
}

type FailReviewApprovalCommand struct {
	ProjectID string
	ReviewID  string
	Reason    string
}

type RequestReviewChangesCommand struct {
	ProjectID string
	ReviewID  string
	Feedback  string
	ActorID   *string
}

type RequestReviewChangesResult struct {
	Review   Review
	Decision Decision
	Run      Run
	Job      SchedulerJob
	Issue    Issue
	Events   []Event
}

type ReviewStore interface {
	GetReview(context.Context, string, string) (Review, error)
	GetReviewByRun(context.Context, string, string) (Review, error)
	ListReviews(context.Context, string, ReviewFilter) ([]Review, error)
	GetDecision(context.Context, string, string) (Decision, error)
	BeginReviewApproval(context.Context, BeginReviewApprovalCommand) (BeginReviewApprovalResult, error)
	CompleteReviewApproval(context.Context, CompleteReviewApprovalCommand) (CompleteReviewApprovalResult, error)
	FailReviewApproval(context.Context, FailReviewApprovalCommand) (Review, error)
	RequestReviewChanges(context.Context, RequestReviewChangesCommand) (RequestReviewChangesResult, error)
}

type ReviewStoreCapability interface {
	SupportsReviewStore() bool
}

func SupportsReviewStore(value any) bool {
	if value == nil {
		return false
	}
	if _, ok := value.(ReviewStore); !ok {
		return false
	}
	if capability, ok := value.(ReviewStoreCapability); ok {
		return capability.SupportsReviewStore()
	}
	return true
}
