package postgres

import (
	"testing"
)

func TestAgentWorkRequestOpenCompatibilityIsUniqueAndSealable(t *testing.T) {
	f := newDelegationFixture(t, true)
	ctx := t.Context()
	workspaceID := f.parentRun.WorkspaceID

	var issueRequestID string
	if err := f.store.pool.QueryRow(ctx, `
		INSERT INTO agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, authority_kind)
		VALUES ($1,$2,$3,$4,'ISSUE')
		RETURNING id::text
	`, f.project.ID, f.issue.ID, workspaceID, f.target.ID).Scan(&issueRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, authority_kind)
		VALUES ($1,$2,$3,$4,'ISSUE')
	`, f.project.ID, f.issue.ID, workspaceID, f.target.ID); err == nil {
		t.Fatal("duplicate open Issue-authority request unexpectedly succeeded")
	}
	if _, err := f.store.pool.Exec(ctx, `UPDATE agent_work_requests SET sealed_at=now() WHERE project_id=$1 AND id=$2`, f.project.ID, issueRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, authority_kind)
		VALUES ($1,$2,$3,$4,'ISSUE')
	`, f.project.ID, f.issue.ID, workspaceID, f.target.ID); err != nil {
		t.Fatalf("sealed request should allow a follow-up request: %v", err)
	}

	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, authority_kind, parent_run_id)
		VALUES ($1,$2,$3,$4,'PARENT_RUN',$5)
	`, f.project.ID, f.issue.ID, workspaceID, f.target.ID, f.parentRun.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO agent_work_requests (project_id, issue_id, workspace_id, target_agent_id, authority_kind, parent_run_id)
		VALUES ($1,$2,$3,$4,'PARENT_RUN',$5)
	`, f.project.ID, f.issue.ID, workspaceID, f.target.ID, f.parentRun.ID); err == nil {
		t.Fatal("duplicate open parent-authority request unexpectedly succeeded")
	}
}
