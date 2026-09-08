package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) OpenInteractiveQuestion(ctx context.Context, input store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionResult, error) {
	batch, err := s.OpenInteractiveQuestions(ctx, []store.OpenInteractiveQuestionCommand{input})
	if err != nil {
		return store.OpenInteractiveQuestionResult{}, err
	}
	if len(batch.Questions) != 1 {
		return store.OpenInteractiveQuestionResult{}, store.ErrConflict
	}
	result := batch.Questions[0]
	result.Events = append([]store.Event(nil), batch.Events...)
	return result, nil
}

func (s *Store) OpenInteractiveQuestions(ctx context.Context, inputs []store.OpenInteractiveQuestionCommand) (store.OpenInteractiveQuestionsResult, error) {
	if len(inputs) == 0 {
		return store.OpenInteractiveQuestionsResult{}, store.ErrInvalidArgument
	}
	first := inputs[0]
	seenCorrelations := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if err := validateOpenInteractiveQuestion(input); err != nil {
			return store.OpenInteractiveQuestionsResult{}, err
		}
		if input.Question.ProjectID != first.Question.ProjectID || input.Question.IssueID != first.Question.IssueID ||
			input.Question.RunID != first.Question.RunID || input.Engine != first.Engine ||
			input.RuntimeInstanceID != first.RuntimeInstanceID {
			return store.OpenInteractiveQuestionsResult{}, store.ErrInvalidArgument
		}
		if _, duplicate := seenCorrelations[input.CorrelationKey]; duplicate {
			return store.OpenInteractiveQuestionsResult{}, store.ErrInvalidArgument
		}
		seenCorrelations[input.CorrelationKey] = struct{}{}
	}

	questionInput := first.Question
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.OpenInteractiveQuestionsResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, questionInput.ProjectID, questionInput.RunID))
	if err != nil {
		return store.OpenInteractiveQuestionsResult{}, err
	}
	if run.IssueID != questionInput.IssueID {
		return store.OpenInteractiveQuestionsResult{}, store.ErrConflict
	}

	results := make([]store.OpenInteractiveQuestionResult, len(inputs))
	missing := make([]int, 0, len(inputs))
	for index, input := range inputs {
		binding, bindingErr := scanInteractiveQuestionBinding(tx.QueryRow(ctx, `
			SELECT question_id::text, project_id::text, run_id::text, engine, correlation_key, state, created_at, updated_at
			FROM engine_question_bindings
			WHERE project_id=$1 AND run_id=$2 AND engine=$3 AND correlation_key=$4
		`, input.Question.ProjectID, input.Question.RunID, input.Engine, input.CorrelationKey))
		if bindingErr == nil {
			question, getErr := scanQuestion(tx.QueryRow(ctx, `
				SELECT id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, custom, blocking, status, created_at, answered_at
				FROM questions
				WHERE project_id=$1 AND id=$2
			`, binding.ProjectID, binding.QuestionID))
			if getErr != nil {
				return store.OpenInteractiveQuestionsResult{}, getErr
			}
			results[index] = store.OpenInteractiveQuestionResult{Question: question, Binding: binding}
			continue
		}
		if !errors.Is(bindingErr, store.ErrNotFound) {
			return store.OpenInteractiveQuestionsResult{}, bindingErr
		}
		missing = append(missing, index)
	}

	if len(missing) > 0 {
		if run.Status != "RUNNING" && run.Status != "WAITING_FOR_INPUT" {
			return store.OpenInteractiveQuestionsResult{}, store.ErrConflict
		}
		live, liveErr := hasLiveClaim(ctx, tx, questionInput.ProjectID, questionInput.RunID)
		if liveErr != nil {
			return store.OpenInteractiveQuestionsResult{}, liveErr
		}
		if !live {
			return store.OpenInteractiveQuestionsResult{}, store.ErrConflict
		}

		for _, index := range missing {
			input := inputs[index]
			kind := input.Question.Kind
			if kind == "" {
				kind = "TEXT"
			}
			status := input.Question.Status
			if status == "" {
				status = "OPEN"
			}
			question, insertErr := scanQuestion(tx.QueryRow(ctx, `
				INSERT INTO questions (project_id, issue_id, run_id, prompt, kind, options, recommendation, custom, blocking, status)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true, $9)
				RETURNING id::text, project_id::text, issue_id::text, run_id::text, prompt, kind, options, recommendation, custom, blocking, status, created_at, answered_at
			`, input.Question.ProjectID, input.Question.IssueID, input.Question.RunID, input.Question.Prompt, kind, arrayJSON(input.Question.Options), input.Question.Recommendation, input.Question.Custom, status))
			if insertErr != nil {
				return store.OpenInteractiveQuestionsResult{}, insertErr
			}

			binding, insertErr := scanInteractiveQuestionBinding(tx.QueryRow(ctx, `
				INSERT INTO engine_question_bindings (question_id, project_id, run_id, engine, correlation_key, state)
				VALUES ($1, $2, $3, $4, $5, 'OPEN')
				RETURNING question_id::text, project_id::text, run_id::text, engine, correlation_key, state, created_at, updated_at
			`, question.ID, question.ProjectID, question.RunID, input.Engine, input.CorrelationKey))
			if insertErr != nil {
				return store.OpenInteractiveQuestionsResult{}, insertErr
			}
			results[index] = store.OpenInteractiveQuestionResult{
				Question: question,
				Binding:  binding,
				Created:  true,
			}
		}
	}

	persistedEvents := make([]store.Event, 0, len(results)+1)
	for index := range results {
		event, ensureErr := ensureInteractiveQuestionCreatedEvent(ctx, tx, run, results[index].Question, first.RuntimeInstanceID)
		if ensureErr != nil {
			return store.OpenInteractiveQuestionsResult{}, ensureErr
		}
		results[index].Events = []store.Event{event}
		persistedEvents = append(persistedEvents, event)
	}

	enteredWaiting := len(missing) > 0 && run.Status == "RUNNING"
	if enteredWaiting {
		run, err = scanRun(tx.QueryRow(ctx, `
			UPDATE runs
			SET status='WAITING_FOR_INPUT', queue_reason=NULL, updated_at=now()
			WHERE project_id=$1 AND id=$2 AND status='RUNNING'
			RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		`, questionInput.ProjectID, questionInput.RunID))
		if err != nil {
			return store.OpenInteractiveQuestionsResult{}, err
		}
		command, execErr := tx.Exec(ctx, `
			UPDATE issues
			SET status=CASE WHEN status='DONE' THEN status ELSE 'BLOCKED' END,
			    updated_at=CASE WHEN status='DONE' THEN updated_at ELSE now() END
			WHERE project_id=$1 AND id=$2
		`, questionInput.ProjectID, questionInput.IssueID)
		if execErr != nil {
			return store.OpenInteractiveQuestionsResult{}, execErr
		}
		if command.RowsAffected() != 1 {
			return store.OpenInteractiveQuestionsResult{}, store.ErrConflict
		}
		results[missing[0]].EnteredWaiting = true

		waitingEvent, eventErr := appendInteractiveQuestionEvent(ctx, tx, run, first.RuntimeInstanceID, "run.waiting_for_input", map[string]any{
			"questionId": results[missing[0]].Question.ID,
		})
		if eventErr != nil {
			return store.OpenInteractiveQuestionsResult{}, eventErr
		}
		persistedEvents = append(persistedEvents, waitingEvent)
	}

	for index := range results {
		results[index].Run = run
	}
	if err := tx.Commit(ctx); err != nil {
		return store.OpenInteractiveQuestionsResult{}, err
	}
	return store.OpenInteractiveQuestionsResult{
		Questions:      results,
		Run:            run,
		EnteredWaiting: enteredWaiting,
		Events:         persistedEvents,
	}, nil
}

func ensureInteractiveQuestionCreatedEvent(ctx context.Context, tx pgx.Tx, run store.Run, question store.Question, runtimeInstanceID string) (store.Event, error) {
	existing, err := scanEvent(tx.QueryRow(ctx, `
		SELECT id::text, schema_version, type, occurred_at, project_id::text, issue_id::text,
		       run_id::text, agent_id::text, workspace_id::text, runtime_instance_id::text,
		       correlation_id::text, parent_event_id::text, sequence, actor, payload, created_at
		FROM events
		WHERE project_id=$1 AND run_id=$2 AND type='question.created' AND payload->>'questionId'=$3
		ORDER BY sequence
		LIMIT 1
	`, question.ProjectID, question.RunID, question.ID))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Event{}, err
	}

	var options []store.QuestionOption
	if len(question.Options) != 0 {
		if err := json.Unmarshal(question.Options, &options); err != nil {
			return store.Event{}, err
		}
	}
	return appendInteractiveQuestionEvent(ctx, tx, run, runtimeInstanceID, "question.created", map[string]any{
		"questionId": question.ID,
		"prompt":     question.Prompt,
		"kind":       question.Kind,
		"options":    options,
		"blocking":   true,
	})
}

func appendInteractiveQuestionEvent(ctx context.Context, tx pgx.Tx, run store.Run, runtimeInstanceID, eventType string, payload any) (store.Event, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return store.Event{}, err
	}
	issueID, runID, workspaceID := run.IssueID, run.ID, run.WorkspaceID
	var runtimeID *string
	if strings.TrimSpace(runtimeInstanceID) != "" {
		runtimeID = &runtimeInstanceID
	}
	return appendEventTx(ctx, tx, store.Event{
		Type:              eventType,
		ProjectID:         run.ProjectID,
		IssueID:           &issueID,
		RunID:             &runID,
		AgentID:           run.AgentID,
		WorkspaceID:       &workspaceID,
		RuntimeInstanceID: runtimeID,
		Actor:             store.EmptyObject,
		Payload:           encoded,
	})
}

func validateOpenInteractiveQuestion(input store.OpenInteractiveQuestionCommand) error {
	questionInput := input.Question
	if strings.TrimSpace(questionInput.ProjectID) == "" || strings.TrimSpace(questionInput.IssueID) == "" ||
		strings.TrimSpace(questionInput.RunID) == "" || strings.TrimSpace(questionInput.Prompt) == "" ||
		strings.TrimSpace(input.Engine) == "" || strings.TrimSpace(input.CorrelationKey) == "" || !questionInput.Blocking {
		return store.ErrInvalidArgument
	}
	return nil
}

func (s *Store) ResolveInteractiveQuestion(ctx context.Context, projectID, questionID string) (store.ResolveInteractiveQuestionResult, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(questionID) == "" {
		return store.ResolveInteractiveQuestionResult{}, store.ErrInvalidArgument
	}

	var runID string
	if err := s.pool.QueryRow(ctx, `
		SELECT run_id::text
		FROM engine_question_bindings
		WHERE project_id=$1 AND question_id=$2
	`, projectID, questionID).Scan(&runID); err != nil {
		return store.ResolveInteractiveQuestionResult{}, notFound(err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		FROM runs
		WHERE project_id=$1 AND id=$2
		FOR UPDATE
	`, projectID, runID))
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	binding, err := scanInteractiveQuestionBinding(tx.QueryRow(ctx, `
		SELECT question_id::text, project_id::text, run_id::text, engine, correlation_key, state, created_at, updated_at
		FROM engine_question_bindings
		WHERE project_id=$1 AND question_id=$2
		FOR UPDATE
	`, projectID, questionID))
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	if binding.RunID != run.ID {
		return store.ResolveInteractiveQuestionResult{}, store.ErrConflict
	}
	if binding.State == store.InteractiveQuestionResolved {
		event, err := scanEvent(tx.QueryRow(ctx, `
			SELECT id::text, schema_version, type, occurred_at, project_id::text, issue_id::text,
			       run_id::text, agent_id::text, workspace_id::text, runtime_instance_id::text,
			       correlation_id::text, parent_event_id::text, sequence, actor, payload, created_at
			FROM events
			WHERE project_id=$1 AND run_id=$2 AND type='engine.question_binding_resolved'
			  AND payload->>'questionId'=$3
			ORDER BY sequence LIMIT 1
		`, projectID, run.ID, questionID))
		if err != nil {
			return store.ResolveInteractiveQuestionResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return store.ResolveInteractiveQuestionResult{}, err
		}
		return store.ResolveInteractiveQuestionResult{Binding: binding, Run: run, Events: []store.Event{event}}, nil
	}
	if binding.State != store.InteractiveQuestionAnswered {
		return store.ResolveInteractiveQuestionResult{}, store.ErrConflict
	}

	binding, err = scanInteractiveQuestionBinding(tx.QueryRow(ctx, `
		UPDATE engine_question_bindings
		SET state='RESOLVED', updated_at=now()
		WHERE project_id=$1 AND question_id=$2 AND state='ANSWERED'
		RETURNING question_id::text, project_id::text, run_id::text, engine, correlation_key, state, created_at, updated_at
	`, projectID, questionID))
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}

	var unresolved int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM engine_question_bindings
		WHERE project_id=$1 AND run_id=$2 AND state IN ('OPEN', 'ANSWERED')
	`, projectID, run.ID).Scan(&unresolved); err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}

	resumed := false
	if unresolved == 0 {
		if run.Status != "WAITING_FOR_INPUT" {
			return store.ResolveInteractiveQuestionResult{}, store.ErrConflict
		}
		live, err := hasLiveClaim(ctx, tx, projectID, run.ID)
		if err != nil {
			return store.ResolveInteractiveQuestionResult{}, err
		}
		if !live {
			return store.ResolveInteractiveQuestionResult{}, store.ErrConflict
		}
		run, err = scanRun(tx.QueryRow(ctx, `
			UPDATE runs
			SET status='RUNNING', queue_reason=NULL, updated_at=now()
			WHERE project_id=$1 AND id=$2 AND status='WAITING_FOR_INPUT'
			RETURNING id::text, project_id::text, issue_id::text, workspace_id::text, agent_id::text, attempt, status, queue_reason, failure_reason, created_at, started_at, completed_at, updated_at
		`, projectID, run.ID))
		if err != nil {
			return store.ResolveInteractiveQuestionResult{}, err
		}
		command, err := tx.Exec(ctx, `
			UPDATE issues
			SET status=CASE WHEN status='DONE' THEN status ELSE 'IN_PROGRESS' END,
			    updated_at=CASE WHEN status='DONE' THEN updated_at ELSE now() END
			WHERE project_id=$1 AND id=$2
		`, projectID, run.IssueID)
		if err != nil {
			return store.ResolveInteractiveQuestionResult{}, err
		}
		if command.RowsAffected() != 1 {
			return store.ResolveInteractiveQuestionResult{}, store.ErrConflict
		}
		resumed = true
	}

	// Resolution, resume and the reply journal must commit together. A failed
	// event write leaves the accepted binding available for reconciliation.
	resolvedEvent, err := appendInteractiveQuestionEvent(ctx, tx, run, "", "engine.question_binding_resolved", map[string]any{
		"engine": binding.Engine, "questionId": questionID,
	})
	if err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	events := []store.Event{resolvedEvent}
	if resumed {
		resumedEvent, err := appendInteractiveQuestionEvent(ctx, tx, run, "", "run.resumed", map[string]any{"questionId": questionID})
		if err != nil {
			return store.ResolveInteractiveQuestionResult{}, err
		}
		events = append(events, resumedEvent)
	}
	if err := tx.Commit(ctx); err != nil {
		return store.ResolveInteractiveQuestionResult{}, err
	}
	return store.ResolveInteractiveQuestionResult{Binding: binding, Run: run, Resumed: resumed, Events: events}, nil
}

func hasLiveClaim(ctx context.Context, tx pgx.Tx, projectID, runID string) (bool, error) {
	var live bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM scheduler_jobs AS job
			JOIN scheduler_leases AS lease ON lease.job_id=job.id
			WHERE job.project_id=$1 AND job.run_id=$2 AND job.state='CLAIMED' AND lease.expires_at > now()
		)
	`, projectID, runID).Scan(&live)
	return live, err
}

func scanInteractiveQuestionBinding(row pgx.Row) (store.InteractiveQuestionBinding, error) {
	var value store.InteractiveQuestionBinding
	if err := row.Scan(&value.QuestionID, &value.ProjectID, &value.RunID, &value.Engine, &value.CorrelationKey, &value.State, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return store.InteractiveQuestionBinding{}, notFound(err)
	}
	return value, nil
}

var _ store.InteractiveQuestionStore = (*Store)(nil)
var _ store.InteractiveQuestionBatchStore = (*Store)(nil)
