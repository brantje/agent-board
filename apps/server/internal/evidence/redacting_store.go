package evidence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brantje/agent-board/apps/server/internal/redaction"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type runtimeRunnerStatusStore interface {
	UpdateRuntimeInstanceRunnerStatusIfStatus(context.Context, string, string, string, string) (store.RuntimeInstance, error)
}

type runtimeRunnerGenerationStore interface {
	ClaimRuntimeInstanceRunnerGeneration(context.Context, string, string) (int64, error)
	UpdateRuntimeInstanceRunnerStatusGeneration(context.Context, string, string, string, int64) (store.RuntimeInstance, error)
	UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(context.Context, string, string, string, int64, string) (store.RuntimeInstance, error)
}

type workspaceBootstrapLockStore interface {
	AcquireWorkspaceBootstrapLock(context.Context, string) (store.WorkspaceBootstrapLock, error)
}

type workspaceBootstrapReadyStore interface {
	MarkWorkspaceBootstrapReady(context.Context, string, string, string, string, string, string, string, string) (store.Workspace, error)
}

type runnerLookupStore interface {
	GetRunner(context.Context, string) (store.Runner, error)
}

// RedactingStore keeps the full control-plane store contract while overriding
// every current durable evidence write with a final server-side sanitizer.
type RedactingStore struct {
	store.ControlPlaneStore
	registry *redaction.Registry
}

func NewRedactingStore(base store.ControlPlaneStore, registry *redaction.Registry) *RedactingStore {
	return &RedactingStore{ControlPlaneStore: base, registry: registry}
}

func (s *RedactingStore) PutRunProvenance(ctx context.Context, projectID, runID string, snapshot json.RawMessage) error {
	redacted, err := s.registry.RedactJSON(runID, snapshot)
	if err != nil {
		return err
	}
	return s.ControlPlaneStore.PutRunProvenance(ctx, projectID, runID, redacted)
}

func (s *RedactingStore) AppendEvent(ctx context.Context, input store.Event) (store.Event, error) {
	var err error
	if input.RunID == nil {
		input.Actor, err = s.registry.RedactAllJSON(input.Actor)
	} else {
		input.Actor, err = s.registry.RedactJSON(*input.RunID, input.Actor)
	}
	if err != nil {
		return store.Event{}, err
	}
	if input.RunID == nil {
		input.Payload, err = s.registry.RedactAllJSON(input.Payload)
	} else {
		input.Payload, err = s.registry.RedactJSON(*input.RunID, input.Payload)
	}
	if err != nil {
		return store.Event{}, err
	}
	return s.ControlPlaneStore.AppendEvent(ctx, input)
}

func (s *RedactingStore) TransitionAdmittedJob(ctx context.Context, input store.SchedulerTransition) (store.Run, error) {
	if input.FailureReason != nil {
		value := s.registry.RedactString(input.RunID, *input.FailureReason)
		input.FailureReason = &value
	}
	return s.ControlPlaneStore.TransitionAdmittedJob(ctx, input)
}

func (s *RedactingStore) CreateRawOutputChunk(ctx context.Context, input store.RawOutputChunk) (store.RawOutputChunk, error) {
	input.StorageRef = s.registry.RedactString(input.RunID, input.StorageRef)
	if input.Digest != nil {
		value := s.registry.RedactString(input.RunID, *input.Digest)
		input.Digest = &value
	}
	return s.ControlPlaneStore.CreateRawOutputChunk(ctx, input)
}

func (s *RedactingStore) CreateArtifact(ctx context.Context, input store.Artifact) (store.Artifact, error) {
	input.Name = s.registry.RedactString(input.RunID, input.Name)
	input.Kind = s.registry.RedactString(input.RunID, input.Kind)
	input.StorageRef = s.registry.RedactString(input.RunID, input.StorageRef)
	if input.MediaType != nil {
		value := s.registry.RedactString(input.RunID, *input.MediaType)
		input.MediaType = &value
	}
	if input.Digest != nil {
		value := s.registry.RedactString(input.RunID, *input.Digest)
		input.Digest = &value
	}
	redacted, err := s.registry.RedactJSON(input.RunID, input.SafeMetadata)
	if err != nil {
		return store.Artifact{}, err
	}
	input.SafeMetadata = redacted
	return s.ControlPlaneStore.CreateArtifact(ctx, input)
}

func (s *RedactingStore) SetIssueStatus(ctx context.Context, input store.IssueStatusMutation) (store.IssueMutationResult, error) {
	base, ok := s.ControlPlaneStore.(store.IssueStatusMutationStore)
	if !ok {
		return store.IssueMutationResult{}, fmt.Errorf("redacting store base does not support Issue status mutations")
	}
	return base.SetIssueStatus(ctx, input)
}

func (s *RedactingStore) UpdateIssueMutationWithActor(ctx context.Context, input store.Issue, actor json.RawMessage) (store.IssueMutationResult, error) {
	base, ok := s.ControlPlaneStore.(store.IssueMutationActorStore)
	if !ok {
		return store.IssueMutationResult{}, fmt.Errorf("redacting store base does not support actor-aware Issue mutations")
	}
	return base.UpdateIssueMutationWithActor(ctx, input, actor)
}

func (s *RedactingStore) PlaceIssue(ctx context.Context, input store.IssuePlacement, actor json.RawMessage) (store.IssueMutationResult, error) {
	base, ok := s.ControlPlaneStore.(store.IssuePlacementStore)
	if !ok {
		return store.IssueMutationResult{}, fmt.Errorf("redacting store base does not support Issue placement")
	}
	return base.PlaceIssue(ctx, input, actor)
}

func (s *RedactingStore) UpdateRuntimeInstanceRunnerStatusIfStatus(ctx context.Context, projectID, instanceID, status, expectedStatus string) (store.RuntimeInstance, error) {
	base, ok := s.ControlPlaneStore.(runtimeRunnerStatusStore)
	if !ok {
		return store.RuntimeInstance{}, fmt.Errorf("redacting store base does not support lifecycle-fenced runner status updates")
	}
	return base.UpdateRuntimeInstanceRunnerStatusIfStatus(ctx, projectID, instanceID, status, expectedStatus)
}

func (s *RedactingStore) ClaimRuntimeInstanceRunnerGeneration(ctx context.Context, projectID, instanceID string) (int64, error) {
	base, ok := s.ControlPlaneStore.(runtimeRunnerGenerationStore)
	if !ok {
		return 0, fmt.Errorf("redacting store base does not support runner connection generations")
	}
	return base.ClaimRuntimeInstanceRunnerGeneration(ctx, projectID, instanceID)
}

func (s *RedactingStore) UpdateRuntimeInstanceRunnerStatusGeneration(ctx context.Context, projectID, instanceID, status string, generation int64) (store.RuntimeInstance, error) {
	base, ok := s.ControlPlaneStore.(runtimeRunnerGenerationStore)
	if !ok {
		return store.RuntimeInstance{}, fmt.Errorf("redacting store base does not support runner connection generations")
	}
	return base.UpdateRuntimeInstanceRunnerStatusGeneration(ctx, projectID, instanceID, status, generation)
}

func (s *RedactingStore) UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(ctx context.Context, projectID, instanceID, status string, generation int64, expectedStatus string) (store.RuntimeInstance, error) {
	base, ok := s.ControlPlaneStore.(runtimeRunnerGenerationStore)
	if !ok {
		return store.RuntimeInstance{}, fmt.Errorf("redacting store base does not support runner connection generations")
	}
	return base.UpdateRuntimeInstanceRunnerStatusGenerationIfStatus(ctx, projectID, instanceID, status, generation, expectedStatus)
}

func (s *RedactingStore) AcquireWorkspaceBootstrapLock(ctx context.Context, workspaceID string) (store.WorkspaceBootstrapLock, error) {
	base, ok := s.ControlPlaneStore.(workspaceBootstrapLockStore)
	if !ok {
		return nil, fmt.Errorf("redacting store base does not support workspace bootstrap locks")
	}
	return base.AcquireWorkspaceBootstrapLock(ctx, workspaceID)
}

func (s *RedactingStore) MarkWorkspaceBootstrapReady(ctx context.Context, projectID, issueID, workspaceID, path, repositoryPath, baseBranch, baseRevision, workingBranch string) (store.Workspace, error) {
	base, ok := s.ControlPlaneStore.(workspaceBootstrapReadyStore)
	if !ok {
		return store.Workspace{}, fmt.Errorf("redacting store base does not support workspace ready transitions")
	}
	return base.MarkWorkspaceBootstrapReady(ctx, projectID, issueID, workspaceID, path, repositoryPath, baseBranch, baseRevision, workingBranch)
}

func (s *RedactingStore) AcquireWorkspaceExecutionLock(ctx context.Context, workspaceID, executionSessionID string) (store.WorkspaceBootstrapLock, error) {
	base, ok := s.ControlPlaneStore.(store.WorkspaceExecutionLockStore)
	if !ok {
		return nil, fmt.Errorf("redacting store base does not support workspace execution locks")
	}
	return base.AcquireWorkspaceExecutionLock(ctx, workspaceID, executionSessionID)
}

func (s *RedactingStore) GetWorkspaceCurrentRevision(ctx context.Context, projectID, workspaceID string) (string, error) {
	base, ok := s.ControlPlaneStore.(store.WorkspaceRevisionStore)
	if !ok {
		return "", fmt.Errorf("redacting store base does not support workspace revision persistence")
	}
	return base.GetWorkspaceCurrentRevision(ctx, projectID, workspaceID)
}

func (s *RedactingStore) UpdateWorkspaceCurrentRevision(ctx context.Context, projectID, workspaceID, revision string) (string, error) {
	base, ok := s.ControlPlaneStore.(store.WorkspaceRevisionStore)
	if !ok {
		return "", fmt.Errorf("redacting store base does not support workspace revision persistence")
	}
	return base.UpdateWorkspaceCurrentRevision(ctx, projectID, workspaceID, revision)
}

func (s *RedactingStore) GetRunner(ctx context.Context, id string) (store.Runner, error) {
	base, ok := s.ControlPlaneStore.(runnerLookupStore)
	if !ok {
		return store.Runner{}, fmt.Errorf("redacting store base does not support runners")
	}
	return base.GetRunner(ctx, id)
}

func (s *RedactingStore) RequestDelegation(ctx context.Context, input store.RequestDelegationCommand) (store.RequestDelegationResult, error) {
	base, ok := s.ControlPlaneStore.(store.DelegationStore)
	if !ok {
		return store.RequestDelegationResult{}, fmt.Errorf("redacting store base does not support delegation")
	}
	return base.RequestDelegation(ctx, input)
}

func (s *RedactingStore) GetDelegationByRun(ctx context.Context, projectID, runID string) (store.Delegation, error) {
	base, ok := s.ControlPlaneStore.(store.DelegationStore)
	if !ok {
		return store.Delegation{}, fmt.Errorf("redacting store base does not support delegation")
	}
	return base.GetDelegationByRun(ctx, projectID, runID)
}

func (s *RedactingStore) ListDelegationsByParentRun(ctx context.Context, projectID, parentRunID string) ([]store.Delegation, error) {
	base, ok := s.ControlPlaneStore.(store.DelegationStore)
	if !ok {
		return nil, fmt.Errorf("redacting store base does not support delegation")
	}
	return base.ListDelegationsByParentRun(ctx, projectID, parentRunID)
}

func (s *RedactingStore) CancelInactiveRun(ctx context.Context, projectID, runID string) (store.RunCancellationResult, error) {
	base, ok := s.ControlPlaneStore.(store.InactiveRunCancellationStore)
	if !ok {
		return store.RunCancellationResult{}, fmt.Errorf("redacting store base does not support inactive Run cancellation")
	}
	return base.CancelInactiveRun(ctx, projectID, runID)
}

var _ store.IssueStatusMutationStore = (*RedactingStore)(nil)
var _ store.IssueMutationActorStore = (*RedactingStore)(nil)
var _ store.IssuePlacementStore = (*RedactingStore)(nil)
var _ store.WorkspaceRevisionStore = (*RedactingStore)(nil)
var _ store.DelegationStore = (*RedactingStore)(nil)
