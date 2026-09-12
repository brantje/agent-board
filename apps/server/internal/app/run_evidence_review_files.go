package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
)

type RunEvidenceReviewStore interface {
	GetReviewByRun(context.Context, string, string) (store.Review, error)
	GetWorkspace(context.Context, string, string) (store.Workspace, error)
}

type reviewGitDiff interface {
	ReviewFileChanges(context.Context, string, string, string) ([]workspace.ReviewFileChange, error)
}

func (s *RunEvidenceService) ConfigureReviewFileChanges(reviews RunEvidenceReviewStore, git reviewGitDiff) {
	s.reviewStore = reviews
	s.reviewGit = git
}

func (s *RunEvidenceService) enrichReviewFileChanges(ctx context.Context, run store.Run, events []store.Event) []store.Event {
	if s.reviewStore == nil || s.reviewGit == nil || hasPersistedFileChanges(events) {
		return events
	}
	review, err := s.reviewStore.GetReviewByRun(ctx, run.ProjectID, run.ID)
	if errors.Is(err, store.ErrNotFound) {
		return events
	}
	if err != nil {
		return events
	}
	baseRevision := strings.TrimSpace(review.BaseRevision)
	reviewRevision := strings.TrimSpace(review.ReviewRevision)
	if baseRevision == "" || reviewRevision == "" {
		return events
	}
	workspaceRecord, err := s.reviewStore.GetWorkspace(ctx, run.ProjectID, run.WorkspaceID)
	if err != nil {
		return events
	}
	changes, err := s.reviewGit.ReviewFileChanges(ctx, workspaceRecord.Path, baseRevision, reviewRevision)
	if err != nil || len(changes) == 0 {
		return events
	}
	derived := make([]store.Event, 0, len(changes))
	occurredAt := time.Now().UTC()
	if len(events) > 0 {
		occurredAt = events[len(events)-1].OccurredAt
	}
	for index, change := range changes {
		sequence := int64(index + 1)
		payload := map[string]any{
			"path":   change.Path,
			"added":  change.Added,
			"removed": change.Removed,
			"source": "git",
		}
		if change.OldPath != "" {
			payload["oldPath"] = change.OldPath
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		derived = append(derived, store.Event{
			ID:            "review-file:" + change.Path,
			SchemaVersion: 1,
			Type:          reviewFileEventType(change),
			OccurredAt:    occurredAt,
			ProjectID:     run.ProjectID,
			IssueID:       &run.IssueID,
			RunID:         &run.ID,
			Sequence:      &sequence,
			Actor:         store.EmptyObject,
			Payload:       encoded,
		})
	}
	return append(events, derived...)
}

func hasPersistedFileChanges(events []store.Event) bool {
	for _, event := range events {
		if strings.HasPrefix(event.Type, "file.") {
			return true
		}
	}
	return false
}

func reviewFileEventType(change workspace.ReviewFileChange) string {
	switch change.ChangeType {
	case "created":
		return "file.created"
	case "deleted":
		return "file.deleted"
	case "renamed":
		return "file.renamed"
	default:
		return "file.modified"
	}
}
