package postgres

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const issueBoardOrderLockPrefix = "agent-board:issue-board:"

func lockIssueBoardOrder(ctx context.Context, tx pgx.Tx, projectID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, issueBoardOrderLockPrefix+projectID)
	return err
}

func newIssueBoardPosition(ctx context.Context, tx pgx.Tx, projectID, status, placement string) (int64, error) {
	switch placement {
	case store.ProjectNewIssuePlacementTop:
		if _, err := tx.Exec(ctx, `
			UPDATE issues
			SET board_position = board_position + 1
			WHERE project_id=$1 AND status=$2
		`, projectID, status); err != nil {
			return 0, err
		}
		return 0, nil
	case store.ProjectNewIssuePlacementBottom:
		var position int64
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(board_position), -1) + 1
			FROM issues
			WHERE project_id=$1 AND status=$2
		`, projectID, status).Scan(&position); err != nil {
			return 0, err
		}
		return position, nil
	default:
		return 0, store.ErrInvalidArgument
	}
}

func placementIndex(ids []string, beforeID, afterID *string) (int, error) {
	if beforeID == nil && afterID == nil {
		if len(ids) == 0 {
			return 0, nil
		}
		return 0, store.ErrConflict
	}
	if beforeID == nil {
		if len(ids) > 0 && afterID != nil && ids[0] == *afterID {
			return 0, nil
		}
		return 0, store.ErrConflict
	}
	if afterID == nil {
		if len(ids) > 0 && ids[len(ids)-1] == *beforeID {
			return len(ids), nil
		}
		return 0, store.ErrConflict
	}
	for index := 1; index < len(ids); index++ {
		if ids[index-1] == *beforeID && ids[index] == *afterID {
			return index, nil
		}
	}
	return 0, store.ErrConflict
}

func orderedIssueIDsTx(ctx context.Context, tx pgx.Tx, projectID, status, excludeID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text
		FROM issues
		WHERE project_id=$1 AND status=$2 AND id<>$3
		ORDER BY board_position, created_at, id
	`, projectID, status, excludeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func rewriteIssuePositionsTx(ctx context.Context, tx pgx.Tx, projectID string, ids []string) error {
	for index, id := range ids {
		if _, err := tx.Exec(ctx, `
			UPDATE issues SET board_position=$3
			WHERE project_id=$1 AND id=$2
		`, projectID, id, index); err != nil {
			return err
		}
	}
	return nil
}

func moveIssueToStatusTopTx(ctx context.Context, tx pgx.Tx, projectID, issueID, sourceStatus, destinationStatus string) error {
	if sourceStatus == destinationStatus {
		return nil
	}
	sourceIDs, err := orderedIssueIDsTx(ctx, tx, projectID, sourceStatus, issueID)
	if err != nil {
		return err
	}
	destinationIDs, err := orderedIssueIDsTx(ctx, tx, projectID, destinationStatus, issueID)
	if err != nil {
		return err
	}
	if err := rewriteIssuePositionsTx(ctx, tx, projectID, sourceIDs); err != nil {
		return err
	}
	destinationIDs = append([]string{issueID}, destinationIDs...)
	return rewriteIssuePositionsTx(ctx, tx, projectID, destinationIDs)
}

func (s *Store) PlaceIssue(ctx context.Context, input store.IssuePlacement, actor json.RawMessage) (store.IssueMutationResult, error) {
	if input.ProjectID == "" || input.IssueID == "" {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}
	if input.Status != nil && !store.ValidIssueStatus(*input.Status) {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}
	if input.BeforeID != nil && *input.BeforeID == input.IssueID {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}
	if input.AfterID != nil && *input.AfterID == input.IssueID {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}
	if input.BeforeID != nil && input.AfterID != nil && *input.BeforeID == *input.AfterID {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockIssueBoardOrder(ctx, tx, input.ProjectID); err != nil {
		return store.IssueMutationResult{}, err
	}

	previous, repositoryPath, defaultBranch, err := lockAssignmentIssue(ctx, tx, input.ProjectID, input.IssueID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	destinationStatus := previous.Status
	if input.Status != nil {
		destinationStatus = *input.Status
	}
	destinationIDs, err := orderedIssueIDsTx(ctx, tx, input.ProjectID, destinationStatus, input.IssueID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	index, err := placementIndex(destinationIDs, input.BeforeID, input.AfterID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if previous.Status != destinationStatus {
		sourceIDs, err := orderedIssueIDsTx(ctx, tx, input.ProjectID, previous.Status, input.IssueID)
		if err != nil {
			return store.IssueMutationResult{}, err
		}
		if err := rewriteIssuePositionsTx(ctx, tx, input.ProjectID, sourceIDs); err != nil {
			return store.IssueMutationResult{}, err
		}
	}
	destinationIDs = append(destinationIDs, "")
	copy(destinationIDs[index+1:], destinationIDs[index:])
	destinationIDs[index] = input.IssueID
	if err := rewriteIssuePositionsTx(ctx, tx, input.ProjectID, destinationIDs); err != nil {
		return store.IssueMutationResult{}, err
	}

	updated := previous
	updated.Status = destinationStatus
	result, err := s.applyIssueMutationTx(ctx, tx, issueMutationTxInput{
		Issue:          updated,
		Previous:       previous,
		RepositoryPath: repositoryPath,
		DefaultBranch:  defaultBranch,
		Actor:          actor,
	})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.IssueMutationResult{}, err
	}
	return result, nil
}
