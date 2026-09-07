package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func cleanupTerminalInteractiveQuestions(ctx context.Context, tx pgx.Tx, run store.Run) error {
	if _, err := tx.Exec(ctx, `
		UPDATE questions
		SET status='CANCELLED'
		WHERE project_id=$1 AND run_id=$2 AND status='OPEN'
	`, run.ProjectID, run.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE engine_question_bindings
		SET state='CANCELLED', updated_at=now()
		WHERE project_id=$1 AND run_id=$2 AND state IN ('OPEN', 'ANSWERED')
	`, run.ProjectID, run.ID); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		UPDATE issues
		SET status=CASE WHEN status='BLOCKED' THEN 'IN_PROGRESS' ELSE status END,
		    updated_at=CASE WHEN status='BLOCKED' THEN now() ELSE updated_at END
		WHERE project_id=$1 AND id=$2
	`, run.ProjectID, run.IssueID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return store.ErrConflict
	}
	return nil
}
