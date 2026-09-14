package postgres

import (
	"context"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func cleanupTerminalInteractiveQuestions(ctx context.Context, tx pgx.Tx, run store.Run) error {
	rows, err := tx.Query(ctx, `
		UPDATE questions AS q
		SET status='CANCELLED', answered_at=NULL, updated_at=now()
		FROM engine_question_bindings AS binding
		WHERE binding.question_id=q.id
		  AND q.project_id=$1 AND q.run_id=$2
		  AND q.status='OPEN' AND q.blocking=true
		  AND binding.project_id=q.project_id AND binding.run_id=q.run_id
		  AND binding.state IN ('OPEN','DELIVERING')
		RETURNING q.id::text
	`, run.ProjectID, run.ID)
	if err != nil {
		return err
	}
	questionIDs := make([]string, 0)
	for rows.Next() {
		var questionID string
		if err := rows.Scan(&questionID); err != nil {
			rows.Close()
			return err
		}
		questionIDs = append(questionIDs, questionID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(questionIDs) == 0 {
		return nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE engine_question_bindings
		SET state='REJECTED', updated_at=now()
		WHERE project_id=$1 AND run_id=$2
		  AND question_id = ANY($3::uuid[])
		  AND state IN ('OPEN','DELIVERING')
	`, run.ProjectID, run.ID, questionIDs); err != nil {
		return err
	}
	for _, questionID := range questionIDs {
		if _, err := appendInteractiveQuestionEvent(ctx, tx, run, nil, "question.cancelled", map[string]any{
			"questionId": questionID,
			"reason":     "run_terminal",
		}); err != nil {
			return err
		}
	}
	return nil
}
