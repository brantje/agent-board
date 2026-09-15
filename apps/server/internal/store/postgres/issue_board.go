package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

type boardIssue struct {
	ID       string
	Status   string
	Position int64
}

func (s *Store) PlaceIssue(ctx context.Context, input store.IssueBoardPlacement) (store.IssueMutationResult, error) {
	if input.ProjectID == "" || input.IssueID == "" || !store.ValidIssueStatus(input.Status) {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}
	if input.BeforeIssueID != nil && *input.BeforeIssueID == input.IssueID {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	previous, repositoryPath, defaultBranch, err := lockAssignmentIssue(ctx, tx, input.ProjectID, input.IssueID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	issues, err := lockProjectBoard(ctx, tx, input.ProjectID)
	if err != nil {
		return store.IssueMutationResult{}, err
	}

	destination := make([]string, 0, len(issues))
	beforeFound := input.BeforeIssueID == nil
	for _, issue := range issues {
		if issue.ID == input.IssueID || issue.Status != input.Status {
			continue
		}
		if input.BeforeIssueID != nil && issue.ID == *input.BeforeIssueID {
			destination = append(destination, input.IssueID)
			beforeFound = true
		}
		destination = append(destination, issue.ID)
	}
	if !beforeFound {
		return store.IssueMutationResult{}, store.ErrInvalidArgument
	}
	if input.BeforeIssueID == nil {
		destination = append(destination, input.IssueID)
	}

	position := int64(0)
	for index, issueID := range destination {
		if issueID == input.IssueID {
			position = int64(index)
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE issues SET board_position=$3 WHERE project_id=$1 AND id=$2`, input.ProjectID, issueID, int64(index)); err != nil {
			return store.IssueMutationResult{}, err
		}
	}
	if previous.Status != input.Status {
		if err := normalizeBoardStatus(ctx, tx, input.ProjectID, previous.Status, input.IssueID); err != nil {
			return store.IssueMutationResult{}, err
		}
	}

	updated := previous
	updated.Status = input.Status
	updated.BoardPosition = position
	result, err := s.applyIssueMutationTx(ctx, tx, issueMutationTxInput{
		Issue:            updated,
		Previous:         previous,
		RepositoryPath:   repositoryPath,
		DefaultBranch:    defaultBranch,
		Actor:            input.Actor,
		BoardPositionSet: true,
	})
	if err != nil {
		return store.IssueMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.IssueMutationResult{}, err
	}
	return result, nil
}

func lockProjectBoard(ctx context.Context, tx pgx.Tx, projectID string) ([]boardIssue, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, status, board_position
		FROM issues
		WHERE project_id=$1
		ORDER BY status, board_position, id
		FOR UPDATE
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []boardIssue
	for rows.Next() {
		var issue boardIssue
		if err := rows.Scan(&issue.ID, &issue.Status, &issue.Position); err != nil {
			return nil, err
		}
		out = append(out, issue)
	}
	return out, rows.Err()
}

func normalizeBoardStatus(ctx context.Context, tx pgx.Tx, projectID, status, excludedIssueID string) error {
	rows, err := tx.Query(ctx, `
		SELECT id::text
		FROM issues
		WHERE project_id=$1 AND status=$2 AND id<>$3
		ORDER BY board_position, id
	`, projectID, status, excludedIssueID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE issues SET board_position=$3 WHERE project_id=$1 AND id=$2`, projectID, id, int64(index)); err != nil {
			return err
		}
	}
	return nil
}
