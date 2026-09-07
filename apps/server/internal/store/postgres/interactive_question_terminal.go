package postgres

import (
	"context"
	"encoding/json"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func cleanupTerminalInteractiveQuestions(ctx context.Context, tx pgx.Tx, run store.Run) error {
	rows, err := tx.Query(ctx, `
		UPDATE questions
		SET status='CANCELLED'
		WHERE project_id=$1 AND run_id=$2 AND status='OPEN'
		RETURNING id::text
	`, run.ProjectID, run.ID)
	if err != nil {
		return err
	}
	cancelledQuestionIDs := make([]string, 0)
	for rows.Next() {
		var questionID string
		if err := rows.Scan(&questionID); err != nil {
			rows.Close()
			return err
		}
		cancelledQuestionIDs = append(cancelledQuestionIDs, questionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if _, err := tx.Exec(ctx, `
		UPDATE engine_question_bindings
		SET state='CANCELLED', updated_at=now()
		WHERE project_id=$1 AND run_id=$2 AND state IN ('OPEN', 'ANSWERED')
	`, run.ProjectID, run.ID); err != nil {
		return err
	}

	issueID, runID, workspaceID := run.IssueID, run.ID, run.WorkspaceID
	for _, questionID := range cancelledQuestionIDs {
		payload, err := json.Marshal(map[string]any{"questionId": questionID})
		if err != nil {
			return err
		}
		if _, err := appendEventTx(ctx, tx, store.Event{
			Type:        "question.cancelled",
			ProjectID:   run.ProjectID,
			IssueID:     &issueID,
			RunID:       &runID,
			AgentID:     run.AgentID,
			WorkspaceID: &workspaceID,
			Actor:       store.EmptyObject,
			Payload:     payload,
		}); err != nil {
			return err
		}
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
