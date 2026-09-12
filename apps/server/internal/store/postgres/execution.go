package postgres

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateWorkspace(ctx context.Context, input store.Workspace) (store.Workspace, error) {
	status := input.BootstrapStatus
	if status == "" {
		status = "PENDING"
	}
	return scanWorkspace(s.pool.QueryRow(ctx, `
		INSERT INTO workspaces (project_id, issue_id, path, repository_path, base_branch, base_revision, working_branch, current_branch, bootstrap_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8)
		RETURNING id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, current_branch, bootstrap_status, created_at, updated_at
	`, input.ProjectID, input.IssueID, input.Path, input.RepositoryPath, input.BaseBranch, input.BaseRevision, input.WorkingBranch, status))
}

func (s *Store) GetWorkspaceByIssue(ctx context.Context, projectID, issueID string) (store.Workspace, error) {
	return scanWorkspace(s.pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, current_branch, bootstrap_status, created_at, updated_at
		FROM workspaces WHERE project_id = $1 AND issue_id = $2
	`, projectID, issueID))
}

func (s *Store) UpdateWorkspaceCurrentBranch(ctx context.Context, projectID, workspaceID, currentBranch string) (store.Workspace, error) {
	currentBranch = strings.TrimSpace(currentBranch)
	if currentBranch == "" {
		return store.Workspace{}, store.ErrInvalidArgument
	}
	return scanWorkspace(s.pool.QueryRow(ctx, `
		UPDATE workspaces
		SET current_branch=$3, updated_at=now()
		WHERE project_id=$1 AND id=$2
		RETURNING id::text, project_id::text, issue_id::text, path, repository_path, base_branch, base_revision, working_branch, current_branch, bootstrap_status, created_at, updated_at
	`, projectID, workspaceID, currentBranch))
}

const runSelectColumns = `
	r.id::text,
	r.project_id::text,
	r.issue_id::text,
	r.workspace_id::text,
	r.agent_id::text,
	r.attempt,
	r.status,
	r.queue_reason,
	r.failure_reason,
	r.created_at,
	r.started_at,
	r.completed_at,
	r.updated_at,
	COALESCE(w.current_branch, w.working_branch) AS current_branch
`

const runWorkspaceJoin = `
LEFT JOIN workspaces AS w ON w.project_id = r.project_id AND w.id = r.workspace_id
`

func (s *Store) CreateRun(ctx context.Context, input store.Run) (store.Run, error) {
	status := input.Status
	if status == "" {
		status = "QUEUED"
	}
	return scanRun(s.pool.QueryRow(ctx, `
		INSERT INTO runs (project_id, issue_id, workspace_id, agent_id, attempt, status, queue_reason, failure_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
	`, input.ProjectID, input.IssueID, input.WorkspaceID, input.AgentID, input.Attempt, status, input.QueueReason, input.FailureReason))
}

func (s *Store) GetRun(ctx context.Context, projectID, runID string) (store.Run, error) {
	return scanRunWithBranch(s.pool.QueryRow(ctx, `
		SELECT `+runSelectColumns+`
		FROM runs AS r
		`+runWorkspaceJoin+`
		WHERE r.project_id = $1 AND r.id = $2
	`, projectID, runID))
}

func (s *Store) CreateExecutionSession(ctx context.Context, input store.ExecutionSession) (store.ExecutionSession, error) {
	if strings.TrimSpace(input.RunnerID) == "" {
		return store.ExecutionSession{}, store.ErrInvalidArgument
	}
	status := input.Status
	if status == "" {
		status = "PENDING"
	}
	cwd := input.CWD
	if cwd == "" {
		cwd = "/workspace"
	}

	// Execution Session admission participates in the same advisory-lock
	// namespace as Workspace filesystem writers. Resolve the immutable Run ->
	// Workspace binding first, then acquire the transaction-scoped advisory lock
	// before taking the Workspace row lock. This prevents a new Runner writer
	// from crossing an in-flight bootstrap/snapshot/apply operation without
	// keeping a PostgreSQL connection checked out for the execution lifetime.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.ExecutionSession{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var workspaceID string
	if err := tx.QueryRow(ctx, `
		SELECT run.workspace_id::text
		FROM runs AS run
		WHERE run.project_id = $1 AND run.id = $2
	`, input.ProjectID, input.RunID).Scan(&workspaceID); err != nil {
		return store.ExecutionSession{}, notFound(err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, workspaceBootstrapLockPrefix+workspaceID); err != nil {
		return store.ExecutionSession{}, err
	}
	if err := tx.QueryRow(ctx, `
		SELECT workspace.id::text
		FROM runs AS run
		JOIN workspaces AS workspace
		  ON workspace.project_id = run.project_id
		 AND workspace.id = run.workspace_id
		WHERE run.project_id = $1
		  AND run.id = $2
		  AND workspace.id = $3::uuid
		FOR UPDATE OF workspace
	`, input.ProjectID, input.RunID, workspaceID).Scan(&workspaceID); err != nil {
		return store.ExecutionSession{}, notFound(err)
	}

	var writerActive bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM execution_sessions AS session
			JOIN runs AS run
			  ON run.project_id = session.project_id
			 AND run.id = session.run_id
			WHERE run.project_id = $1
			  AND run.workspace_id = $2
			  AND session.status IN ('PENDING', 'STARTING', 'RUNNING')
		)
	`, input.ProjectID, workspaceID).Scan(&writerActive); err != nil {
		return store.ExecutionSession{}, err
	}
	if writerActive {
		return store.ExecutionSession{}, store.ErrConflict
	}

	created, err := scanExecutionSession(tx.QueryRow(ctx, `
		INSERT INTO execution_sessions (project_id, run_id, runner_id, status, cwd, command_argv, exit_code)
		VALUES ($1, $2, $3::uuid, $4, $5, $6, $7)
		RETURNING id::text, project_id::text, run_id::text, runner_id::text, status, cwd, command_argv, exit_code, created_at, started_at, completed_at, updated_at
	`, input.ProjectID, input.RunID, input.RunnerID, status, cwd, arrayJSON(input.CommandArgv), input.ExitCode))
	if err != nil {
		return store.ExecutionSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.ExecutionSession{}, err
	}
	return created, nil
}

func (s *Store) CreateQuestion(ctx context.Context, input store.Question) (store.Question, error) {
	kind := input.Kind
	if kind == "" {
		kind = "TEXT"
	}
	status := input.Status
	if status == "" {
		status = "OPEN"
	}
	return scanQuestion(s.pool.QueryRow(ctx, `
		INSERT INTO questions (project_id, issue_id, run_id, prompt, kind, options, recommendation, custom, blocking, status)
		SELECT $1, run.issue_id, run.id, $4, $5, $6, $7, $8, $9, $10
		FROM runs AS run
		WHERE run.project_id = $1 AND run.id = $3 AND run.issue_id = $2
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, custom, blocking, status, created_at, answered_at
	`, input.ProjectID, input.IssueID, input.RunID, input.Prompt, kind, arrayJSON(input.Options), input.Recommendation, input.Custom, input.Blocking, status))
}

func (s *Store) CreateDecision(ctx context.Context, input store.Decision) (store.Decision, error) {
	return scanDecision(s.pool.QueryRow(ctx, `
		INSERT INTO decisions (project_id, issue_id, run_id, question_id, kind, outcome, actor_type, actor_id, safe_details)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9
		WHERE
			($2::uuid IS NULL OR EXISTS (
				SELECT 1 FROM issues AS issue WHERE issue.project_id = $1 AND issue.id = $2
			))
			AND
			($3::uuid IS NULL OR EXISTS (
				SELECT 1 FROM runs AS run
				WHERE run.project_id = $1 AND run.id = $3 AND ($2::uuid IS NULL OR run.issue_id = $2)
			))
			AND
			($4::uuid IS NULL OR EXISTS (
				SELECT 1 FROM questions AS question
				WHERE question.project_id = $1
				  AND question.id = $4
				  AND ($2::uuid IS NULL OR question.issue_id = $2)
				  AND ($3::uuid IS NULL OR question.run_id = $3)
			))
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, question_id::text, kind, outcome, actor_type, actor_id, safe_details, created_at
	`, input.ProjectID, input.IssueID, input.RunID, input.QuestionID, input.Kind, input.Outcome, input.ActorType, input.ActorID, objectJSON(input.SafeDetails)))
}

func (s *Store) CreateReview(ctx context.Context, input store.Review) (store.Review, error) {
	status := input.Status
	if status == "" {
		status = "PENDING"
	}
	return scanReview(s.pool.QueryRow(ctx, `
		INSERT INTO reviews (project_id, issue_id, run_id, status, decision_id, base_revision, review_revision)
		SELECT $1, run.issue_id, run.id, $4, $5, NULLIF($6, ''), NULLIF($7, '')
		FROM runs AS run
		WHERE run.project_id = $1
		  AND run.id = $3
		  AND run.issue_id = $2
		  AND ($5::uuid IS NULL OR EXISTS (
			SELECT 1 FROM decisions AS decision
			WHERE decision.project_id = $1
			  AND decision.id = $5
			  AND (decision.issue_id IS NULL OR decision.issue_id = run.issue_id)
			  AND (decision.run_id IS NULL OR decision.run_id = run.id)
		  ))
		RETURNING id::text, project_id::text, issue_id::text, run_id::text, status, decision_id::text,
		          COALESCE(base_revision, ''), COALESCE(review_revision, ''), requested_at, decided_at, created_at, updated_at
	`, input.ProjectID, input.IssueID, input.RunID, status, input.DecisionID, strings.TrimSpace(input.BaseRevision), strings.TrimSpace(input.ReviewRevision)))
}

func scanWorkspace(row pgx.Row) (store.Workspace, error) {
	var value store.Workspace
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.Path, &value.RepositoryPath, &value.BaseBranch, &value.BaseRevision, &value.WorkingBranch, &value.CurrentBranch, &value.BootstrapStatus, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Workspace{}, notFound(err)
	}
	return value, nil
}

func scanRun(row pgx.Row) (store.Run, error) {
	var value store.Run
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.WorkspaceID, &value.AgentID, &value.Attempt, &value.Status, &value.QueueReason, &value.FailureReason, &value.CreatedAt, &value.StartedAt, &value.CompletedAt, &value.UpdatedAt); err != nil {
		return store.Run{}, notFound(err)
	}
	return value, nil
}

func scanRunWithBranch(row pgx.Row) (store.Run, error) {
	var value store.Run
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.WorkspaceID, &value.AgentID, &value.Attempt, &value.Status, &value.QueueReason, &value.FailureReason, &value.CreatedAt, &value.StartedAt, &value.CompletedAt, &value.UpdatedAt, &value.CurrentBranch); err != nil {
		return store.Run{}, notFound(err)
	}
	return value, nil
}

func scanExecutionSession(row pgx.Row) (store.ExecutionSession, error) {
	var value store.ExecutionSession
	if err := row.Scan(&value.ID, &value.ProjectID, &value.RunID, &value.RunnerID, &value.Status, &value.CWD, &value.CommandArgv, &value.ExitCode, &value.CreatedAt, &value.StartedAt, &value.CompletedAt, &value.UpdatedAt); err != nil {
		return store.ExecutionSession{}, notFound(err)
	}
	return value, nil
}

func scanQuestion(row pgx.Row) (store.Question, error) {
	var value store.Question
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.RunID, &value.Prompt, &value.Kind, &value.Options, &value.Recommendation, &value.Custom, &value.Blocking, &value.Status, &value.CreatedAt, &value.AnsweredAt); err != nil {
		return store.Question{}, notFound(err)
	}
	return value, nil
}

func scanDecision(row pgx.Row) (store.Decision, error) {
	var value store.Decision
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.RunID, &value.QuestionID, &value.Kind, &value.Outcome, &value.ActorType, &value.ActorID, &value.SafeDetails, &value.CreatedAt); err != nil {
		return store.Decision{}, notFound(err)
	}
	return value, nil
}

func scanReview(row pgx.Row) (store.Review, error) {
	var value store.Review
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IssueID, &value.RunID, &value.Status, &value.DecisionID, &value.BaseRevision, &value.ReviewRevision, &value.RequestedAt, &value.DecidedAt, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.Review{}, notFound(err)
	}
	return value, nil
}
