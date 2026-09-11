package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Store) lockRunnerCandidate(ctx context.Context, tx pgx.Tx, projectID, agentID string) (string, error) {
	var engine string
	if err := tx.QueryRow(ctx, `SELECT engine FROM agents WHERE id=$1`, agentID).Scan(&engine); err != nil {
		return "", err
	}
	ids := []string{}
	if s.runnerCandidates != nil {
		ids = s.runnerCandidates(engine)
	}
	var id string
	err := tx.QueryRow(ctx, `
 SELECT runner.id::text FROM runners runner JOIN projects project ON project.id=$1
 WHERE runner.id=ANY($2::uuid[]) AND runner.deleted_at IS NULL AND runner.revoked_at IS NULL
 AND ((runner.internal AND project.allow_internal_runner) OR
      (NOT runner.internal AND (NOT EXISTS(SELECT 1 FROM project_runners WHERE project_id=$1)
       OR EXISTS(SELECT 1 FROM project_runners WHERE project_id=$1 AND runner_id=runner.id))))
 AND (SELECT count(*) FROM scheduler_capacity_reservations WHERE resource_kind='RUNNER' AND resource_id=runner.id)
     < COALESCE(CASE WHEN (runner.capabilities->>'max_active_sessions') ~ '^[1-9][0-9]*$' THEN (runner.capabilities->>'max_active_sessions')::int END, 5)
 ORDER BY runner.internal,runner.created_at,runner.id
 FOR UPDATE OF runner SKIP LOCKED LIMIT 1`, projectID, ids).Scan(&id)
	return id, notFound(err)
}
