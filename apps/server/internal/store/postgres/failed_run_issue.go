package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// rollbackFailedRunIssueStatus applies the one system-owned Issue projection in
// the v0.1 workflow contract. The caller has already made run non-active. The
// Issue row lock serializes this check with assignment, explicit status changes,
// and all normal Issue enqueue paths. A zero Event means no Board change was made.
func rollbackFailedRunIssueStatus(ctx context.Context, tx pgx.Tx, run store.Run) (store.Event, error) {
	issue, _, _, err := lockAssignmentIssue(ctx, tx, run.ProjectID, run.IssueID)
	if err != nil {
		return store.Event{}, err
	}
	if issue.Status != "IN_PROGRESS" {
		return store.Event{}, nil
	}

	var otherActive bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM runs
			WHERE project_id=$1 AND issue_id=$2 AND id<>$3
			  AND status=ANY($4::text[])
		)
	`, run.ProjectID, run.IssueID, run.ID, activeRunStatuses).Scan(&otherActive); err != nil {
		return store.Event{}, err
	}
	if otherActive {
		return store.Event{}, nil
	}

	updated, err := scanIssueJoined(tx.QueryRow(ctx, `
		UPDATE issues AS i SET status='TODO', updated_at=now()
		FROM projects AS p
		WHERE i.project_id=$1 AND i.id=$2 AND i.status='IN_PROGRESS' AND p.id=i.project_id
		RETURNING `+issueSelectColumns+`
	`, run.ProjectID, run.IssueID))
	if err != nil {
		return store.Event{}, err
	}
	updated.PreviousStatus = "IN_PROGRESS"
	event, err := store.NewIssueUpdatedEventWithActor(updated, "IN_PROGRESS", store.EmptyObject)
	if err != nil {
		return store.Event{}, err
	}
	event.RunID = &run.ID
	event.AgentID = run.AgentID
	event.WorkspaceID = &run.WorkspaceID
	return appendEventTx(ctx, tx, event)
}
