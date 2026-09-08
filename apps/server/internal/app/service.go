package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type Service struct {
	store               store.ControlPlaneStore
	projectRepositories repository.ProjectRepositoryProvisioner
	events              issueEventRecorder
}

type issueEventRecorder interface {
	Record(context.Context, store.Event) (store.Event, error)
}

func New(controlPlaneStore store.ControlPlaneStore) *Service {
	return &Service{store: controlPlaneStore}
}

func (s *Service) SetProjectRepositoryProvisioner(provisioner repository.ProjectRepositoryProvisioner) {
	if s != nil {
		s.projectRepositories = provisioner
	}
}

func (s *Service) SetEventRecorder(recorder issueEventRecorder) {
	if s == nil {
		return
	}
	s.events = recorder
}

func (s *Service) ListProjects(ctx context.Context) ([]store.Project, error) {
	return s.store.ListProjects(ctx)
}

func (s *Service) CreateProject(ctx context.Context, input store.Project) (store.Project, error) {
	if err := validateProject(input); err != nil {
		return store.Project{}, err
	}
	input, err := s.ensureProjectRepository(ctx, input)
	if err != nil {
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
	input, err := s.ensureProjectRepository(ctx, input)
	if err != nil {
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
	if _, err := s.GetModelProfile(ctx, input.ProjectID, input.ModelProfileID); err != nil {
		return store.Agent{}, err
	}
	if _, err := s.GetRuntime(ctx, input.ProjectID, input.RuntimeID); err != nil {
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
	if _, err := s.GetModelProfile(ctx, scope, input.ModelProfileID); err != nil {
		return store.Agent{}, err
	}
	if _, err := s.GetRuntime(ctx, scope, input.RuntimeID); err != nil {
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

func (s *Service) ResolveIssueUUID(ctx context.Context, projectID, key string) (string, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return "", err
	}
	if !store.ValidIssueKey(key) {
		return "", invalid("issue id must be an issue key")
	}
	uuid, err := s.store.GetIssueUUIDByKey(ctx, projectID, key)
	return uuid, translateStoreError(err, "issue")
}
func (s *Service) CreateIssue(ctx context.Context, input store.Issue) (store.Issue, error) {
	if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	if err := validateIssue(input); err != nil {
		return store.Issue{}, err
	}
	value, err := s.store.CreateIssue(ctx, input)
	if err != nil {
		return store.Issue{}, translateStoreError(err, "issue")
	}
	event, err := s.recordIssueEvent(ctx, "issue.created", value, issueMutationPayload(value))
	if err != nil {
		return store.Issue{}, err
	}
	return attachIssueEvent(value, event), nil
}
func (s *Service) UpdateIssue(ctx context.Context, input store.Issue) (store.Issue, error) {
	if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
		return store.Issue{}, err
	}
	if err := validateIssue(input); err != nil {
		return store.Issue{}, err
	}
	current, err := s.GetIssue(ctx, input.ProjectID, input.ID)
	if err != nil {
		return store.Issue{}, err
	}
	value, err := s.store.UpdateIssue(ctx, input)
	if err != nil {
		return store.Issue{}, translateStoreError(err, "issue")
	}
	eventType := "issue.updated"
	payload := issueMutationPayload(value)
	previousStatus := value.PreviousStatus
	if previousStatus == "" {
		previousStatus = current.Status
	}
	if previousStatus != value.Status {
		eventType = "issue.status_changed"
		payload["previousStatus"] = previousStatus
	}
	event, err := s.recordIssueEvent(ctx, eventType, value, payload)
	if err != nil {
		return store.Issue{}, err
	}
	return attachIssueEvent(value, event), nil
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

var projectEventPageSize = 500
var projectEventMaxPages = 20

func (s *Service) ListProjectEventsAfter(ctx context.Context, projectID, afterID string) ([]store.Event, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	if afterID == "" {
		return nil, nil
	}
	events := make([]store.Event, 0)
	cursor := afterID
	for page := 0; page < projectEventMaxPages; page++ {
		batch, err := s.store.ListProjectEventsAfter(ctx, projectID, cursor, projectEventPageSize)
		if err != nil {
			return nil, translateStoreError(err, "event")
		}
		if len(batch) == 0 {
			return events, nil
		}
		events = append(events, batch...)
		cursor = batch[len(batch)-1].ID
		if len(batch) < projectEventPageSize {
			return events, nil
		}
	}
	return events, nil
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

func (s *Service) ensureProjectRepository(ctx context.Context, input store.Project) (store.Project, error) {
	if s == nil || s.projectRepositories == nil {
		return input, nil
	}
	branch := strings.TrimSpace(input.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	canonical, err := s.projectRepositories.EnsureProjectRepository(ctx, input.RepositoryPath, branch)
	if err != nil {
		return store.Project{}, translateRepositoryProvisionerError(err)
	}
	input.RepositoryPath = canonical
	if strings.TrimSpace(input.DefaultBranch) == "" {
		input.DefaultBranch = branch
	}
	return input, nil
}

func translateRepositoryProvisionerError(err error) error {
	switch {
	case errors.Is(err, repository.ErrPathNotAbsolute),
		errors.Is(err, repository.ErrPathNotAuthorized),
		errors.Is(err, repository.ErrPathNotDirectory),
		errors.Is(err, repository.ErrNoAuthorizedRoots):
		return NewError("repository_path_invalid", "repository path is outside deployment-authorized roots", err)
	case errors.Is(err, repository.ErrPathUnavailable):
		return NewError("repository_path_unavailable", "repository path is unavailable", err)
	default:
		return NewError("repository_provision_failed", "repository could not be prepared", err)
	}
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
	prefix := store.NormalizeIssuePrefix(v.IssuePrefix)
	if !store.ValidIssuePrefix(prefix) {
		return invalid("issuePrefix is required and must be 2-10 alphanumeric characters starting with a letter")
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
func validateAgent(v store.Agent) error {
	if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Engine) == "" || strings.TrimSpace(v.ModelProfileID) == "" || strings.TrimSpace(v.RuntimeID) == "" {
		return invalid("agent name, engine, modelProfileId and runtimeId are required")
	}
	if !validObject(v.EngineSettings) {
		return invalid("engineSettings must be a JSON object")
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
