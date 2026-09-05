package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type Service struct {
	store store.ControlPlaneStore
}

func New(controlPlaneStore store.ControlPlaneStore) *Service {
	return &Service{store: controlPlaneStore}
}

func (s *Service) ListProjects(ctx context.Context) ([]store.Project, error) {
	return s.store.ListProjects(ctx)
}

func (s *Service) CreateProject(ctx context.Context, input store.Project) (store.Project, error) {
	if err := validateProject(input); err != nil {
		return store.Project{}, err
	}
	value, err := s.store.CreateProject(ctx, input)
	return value, translateStoreError(err, "project")
}

func (s *Service) GetProject(ctx context.Context, id string) (store.Project, error) {
	value, err := s.store.GetProject(ctx, id)
	return value, translateStoreError(err, "project")
}

func (s *Service) UpdateProject(ctx context.Context, input store.Project) (store.Project, error) {
	if err := validateProject(input); err != nil {
		return store.Project{}, err
	}
	value, err := s.store.UpdateProject(ctx, input)
	return value, translateStoreError(err, "project")
}

func (s *Service) ListProviders(ctx context.Context) ([]store.Provider, error) {
	return s.store.ListProviders(ctx)
}
func (s *Service) GetProvider(ctx context.Context, id string) (store.Provider, error) {
	value, err := s.store.GetProvider(ctx, id)
	return value, translateStoreError(err, "provider")
}
func (s *Service) CreateProvider(ctx context.Context, input store.Provider) (store.Provider, error) {
	if err := validateProvider(input); err != nil {
		return store.Provider{}, err
	}
	value, err := s.store.CreateProvider(ctx, input)
	return value, translateStoreError(err, "provider")
}
func (s *Service) UpdateProvider(ctx context.Context, input store.Provider) (store.Provider, error) {
	if err := validateProvider(input); err != nil {
		return store.Provider{}, err
	}
	value, err := s.store.UpdateProvider(ctx, input)
	return value, translateStoreError(err, "provider")
}

func (s *Service) ensureScope(ctx context.Context, scope *string) error {
	if scope == nil {
		return nil
	}
	_, err := s.GetProject(ctx, *scope)
	return err
}

func (s *Service) ListModelProfiles(ctx context.Context, scope *string) ([]store.ModelProfile, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return nil, err
	}
	return s.store.ListModelProfiles(ctx, scope)
}
func (s *Service) GetModelProfile(ctx context.Context, scope *string, id string) (store.ModelProfile, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return store.ModelProfile{}, err
	}
	value, err := s.store.GetModelProfile(ctx, scope, id)
	return value, translateStoreError(err, "model_profile")
}
func (s *Service) CreateModelProfile(ctx context.Context, input store.ModelProfile) (store.ModelProfile, error) {
	if err := s.ensureScope(ctx, input.ProjectID); err != nil {
		return store.ModelProfile{}, err
	}
	if err := validateModelProfile(input); err != nil {
		return store.ModelProfile{}, err
	}
	if _, err := s.GetProvider(ctx, input.ProviderID); err != nil {
		return store.ModelProfile{}, err
	}
	value, err := s.store.CreateModelProfile(ctx, input)
	return value, translateStoreError(err, "model_profile")
}
func (s *Service) UpdateModelProfile(ctx context.Context, scope *string, input store.ModelProfile) (store.ModelProfile, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return store.ModelProfile{}, err
	}
	if err := validateModelProfile(input); err != nil {
		return store.ModelProfile{}, err
	}
	if _, err := s.GetProvider(ctx, input.ProviderID); err != nil {
		return store.ModelProfile{}, err
	}
	value, err := s.store.UpdateModelProfile(ctx, scope, input)
	return value, translateStoreError(err, "model_profile")
}

func (s *Service) ListRuntimes(ctx context.Context, scope *string) ([]store.Runtime, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return nil, err
	}
	return s.store.ListRuntimes(ctx, scope)
}
func (s *Service) GetRuntime(ctx context.Context, scope *string, id string) (store.Runtime, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return store.Runtime{}, err
	}
	value, err := s.store.GetRuntime(ctx, scope, id)
	return value, translateStoreError(err, "runtime")
}
func (s *Service) CreateRuntime(ctx context.Context, input store.Runtime) (store.Runtime, error) {
	if err := s.ensureScope(ctx, input.ProjectID); err != nil {
		return store.Runtime{}, err
	}
	if err := validateRuntime(input); err != nil {
		return store.Runtime{}, err
	}
	value, err := s.store.CreateRuntime(ctx, input)
	return value, translateStoreError(err, "runtime")
}
func (s *Service) UpdateRuntime(ctx context.Context, scope *string, input store.Runtime) (store.Runtime, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return store.Runtime{}, err
	}
	if err := validateRuntime(input); err != nil {
		return store.Runtime{}, err
	}
	value, err := s.store.UpdateRuntime(ctx, scope, input)
	return value, translateStoreError(err, "runtime")
}

func (s *Service) ListExecutorProfiles(ctx context.Context, scope *string) ([]store.ExecutorProfile, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return nil, err
	}
	return s.store.ListExecutorProfiles(ctx, scope)
}
func (s *Service) GetExecutorProfile(ctx context.Context, scope *string, id string) (store.ExecutorProfile, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return store.ExecutorProfile{}, err
	}
	value, err := s.store.GetExecutorProfile(ctx, scope, id)
	return value, translateStoreError(err, "executor_profile")
}
func (s *Service) CreateExecutorProfile(ctx context.Context, input store.ExecutorProfile) (store.ExecutorProfile, error) {
	if err := s.ensureScope(ctx, input.ProjectID); err != nil {
		return store.ExecutorProfile{}, err
	}
	if err := validateExecutorProfile(input); err != nil {
		return store.ExecutorProfile{}, err
	}
	if _, err := s.GetModelProfile(ctx, input.ProjectID, input.ModelProfileID); err != nil {
		return store.ExecutorProfile{}, err
	}
	if _, err := s.GetRuntime(ctx, input.ProjectID, input.RuntimeID); err != nil {
		return store.ExecutorProfile{}, err
	}
	value, err := s.store.CreateExecutorProfile(ctx, input)
	return value, translateStoreError(err, "executor_profile")
}
func (s *Service) UpdateExecutorProfile(ctx context.Context, scope *string, input store.ExecutorProfile) (store.ExecutorProfile, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return store.ExecutorProfile{}, err
	}
	if err := validateExecutorProfile(input); err != nil {
		return store.ExecutorProfile{}, err
	}
	if _, err := s.GetModelProfile(ctx, scope, input.ModelProfileID); err != nil {
		return store.ExecutorProfile{}, err
	}
	if _, err := s.GetRuntime(ctx, scope, input.RuntimeID); err != nil {
		return store.ExecutorProfile{}, err
	}
	value, err := s.store.UpdateExecutorProfile(ctx, scope, input)
	return value, translateStoreError(err, "executor_profile")
}

func (s *Service) ListAgents(ctx context.Context, scope *string) ([]store.Agent, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return nil, err
	}
	return s.store.ListAgents(ctx, scope)
}
func (s *Service) GetAgent(ctx context.Context, scope *string, id string) (store.Agent, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return store.Agent{}, err
	}
	value, err := s.store.GetAgentInScope(ctx, scope, id)
	return value, translateStoreError(err, "agent")
}
func (s *Service) CreateAgent(ctx context.Context, input store.Agent) (store.Agent, error) {
	if err := s.ensureScope(ctx, input.ProjectID); err != nil {
		return store.Agent{}, err
	}
	if err := validateAgent(input); err != nil {
		return store.Agent{}, err
	}
	if _, err := s.GetExecutorProfile(ctx, input.ProjectID, input.ExecutorProfileID); err != nil {
		return store.Agent{}, err
	}
	value, err := s.store.CreateAgent(ctx, input)
	return value, translateStoreError(err, "agent")
}
func (s *Service) UpdateAgent(ctx context.Context, scope *string, input store.Agent) (store.Agent, error) {
	if err := s.ensureScope(ctx, scope); err != nil {
		return store.Agent{}, err
	}
	if err := validateAgent(input); err != nil {
		return store.Agent{}, err
	}
	if _, err := s.GetExecutorProfile(ctx, scope, input.ExecutorProfileID); err != nil {
		return store.Agent{}, err
	}
	value, err := s.store.UpdateAgent(ctx, scope, input)
	return value, translateStoreError(err, "agent")
}

func (s *Service) ListIssues(ctx context.Context, projectID string) ([]store.Issue, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.store.ListIssues(ctx, projectID)
}
func (s *Service) GetIssue(ctx context.Context, projectID, issueID string) (store.Issue, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return store.Issue{}, err
	}
	value, err := s.store.GetIssue(ctx, projectID, issueID)
	return value, translateStoreError(err, "issue")
}
func (s *Service) CreateIssue(ctx context.Context, input store.Issue) (store.Issue, error) {
	if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	if err := validateIssue(input); err != nil {
		return store.Issue{}, err
	}
	value, err := s.store.CreateIssue(ctx, input)
	return value, translateStoreError(err, "issue")
}
func (s *Service) UpdateIssue(ctx context.Context, input store.Issue) (store.Issue, error) {
	if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	if err := validateIssue(input); err != nil {
		return store.Issue{}, err
	}
	value, err := s.store.UpdateIssue(ctx, input)
	return value, translateStoreError(err, "issue")
}
func (s *Service) ListRuns(ctx context.Context, projectID string) ([]store.Run, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.store.ListRuns(ctx, projectID)
}
func (s *Service) GetRun(ctx context.Context, projectID, runID string) (store.Run, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return store.Run{}, err
	}
	value, err := s.store.GetRun(ctx, projectID, runID)
	return value, translateStoreError(err, "run")
}

func translateStoreError(err error, resource string) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return NewError(resource+"_not_found", strings.ReplaceAll(resource, "_", " ")+" not found", err)
	case errors.Is(err, store.ErrConflict):
		return NewError("conflict", "resource conflicts with existing state", err)
	case errors.Is(err, store.ErrInvalidArgument):
		return NewError("invalid_argument", "invalid request", err)
	default:
		return err
	}
}

func invalid(message string) error {
	return NewError("invalid_argument", message, store.ErrInvalidArgument)
}
func validObject(value json.RawMessage) bool {
	if len(value) == 0 {
		return true
	}
	var object map[string]any
	return json.Unmarshal(value, &object) == nil && object != nil
}
func validateProject(v store.Project) error {
	if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.RepositoryPath) == "" {
		return invalid("project name and repositoryPath are required")
	}
	if v.DefaultBranch != "" && strings.TrimSpace(v.DefaultBranch) == "" {
		return invalid("defaultBranch must not be blank")
	}
	if !validObject(v.WorkflowSettings) {
		return invalid("workflowSettings must be a JSON object")
	}
	return nil
}
func validateProvider(v store.Provider) error {
	if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Kind) == "" {
		return invalid("provider name and kind are required")
	}
	if !validObject(v.SafeMetadata) {
		return invalid("safeMetadata must be a JSON object")
	}
	return nil
}
func validateModelProfile(v store.ModelProfile) error {
	if strings.TrimSpace(v.ProviderID) == "" || strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Model) == "" {
		return invalid("providerId, name and model are required")
	}
	if v.Temperature != nil && (*v.Temperature < 0 || *v.Temperature > 2) {
		return invalid("temperature must be between 0 and 2")
	}
	if v.MaxTokens != nil && *v.MaxTokens < 1 {
		return invalid("maxTokens must be positive")
	}
	if v.MaxConcurrent != nil && *v.MaxConcurrent < 1 {
		return invalid("maxConcurrent must be positive")
	}
	if !validObject(v.GenerationSettings) {
		return invalid("generationSettings must be a JSON object")
	}
	return nil
}
func validateRuntime(v store.Runtime) error {
	if strings.TrimSpace(v.Name) == "" || v.Kind != "docker" || strings.TrimSpace(v.Image) == "" {
		return invalid("runtime requires name, docker kind and image")
	}
	if v.CPULimitMillis != nil && *v.CPULimitMillis < 1 {
		return invalid("cpuLimitMillis must be positive")
	}
	if v.MemoryLimitBytes != nil && *v.MemoryLimitBytes < 1 {
		return invalid("memoryLimitBytes must be positive")
	}
	if v.PIDLimit != nil && *v.PIDLimit < 1 {
		return invalid("pidLimit must be positive")
	}
	if v.TimeoutSeconds != nil && *v.TimeoutSeconds < 1 {
		return invalid("timeoutSeconds must be positive")
	}
	if v.NetworkPolicy != "none" && v.NetworkPolicy != "restricted" && v.NetworkPolicy != "outbound" {
		return invalid("invalid networkPolicy")
	}
	if v.WorkspacePolicy != "" && v.WorkspacePolicy != "issue" {
		return invalid("invalid workspacePolicy")
	}
	if !validObject(v.Capabilities) {
		return invalid("capabilities must be a JSON object")
	}
	return nil
}
func validateExecutorProfile(v store.ExecutorProfile) error {
	if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Engine) == "" || strings.TrimSpace(v.ModelProfileID) == "" || strings.TrimSpace(v.RuntimeID) == "" {
		return invalid("executor profile fields are required")
	}
	if !validObject(v.EngineSettings) {
		return invalid("engineSettings must be a JSON object")
	}
	return nil
}
func validateAgent(v store.Agent) error {
	if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.ExecutorProfileID) == "" {
		return invalid("agent name and executorProfileId are required")
	}
	if v.ConcurrencyLimit < 1 {
		return invalid("concurrencyLimit must be positive")
	}
	switch v.State {
	case "DRAFT", "ENABLED", "DISABLED", "ARCHIVED":
	default:
		return invalid("invalid agent state")
	}
	return nil
}
func validateIssue(v store.Issue) error {
	if strings.TrimSpace(v.Title) == "" {
		return invalid("issue title is required")
	}
	switch v.Status {
	case "BACKLOG", "TODO", "IN_PROGRESS", "BLOCKED", "REVIEW", "DONE":
	default:
		return invalid("invalid issue status")
	}
	return nil
}
