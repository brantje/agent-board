package httpapi

import (
	"net/http"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/go-chi/chi/v5"
)

func (a *api) registerConfigurationRoutes(r chi.Router) {
	r.Get("/projects", a.listProjects)
	r.Post("/projects", a.createProject)
	r.Get("/projects/{projectID}", a.getProject)
	r.Patch("/projects/{projectID}", a.updateProject)
	r.Route("/projects/{projectID}", func(r chi.Router) {
		a.registerScopedConfig(r)
	})

	r.Get("/providers", a.listProviders)
	r.Post("/providers", a.createProvider)
	r.Get("/providers/{resourceID}", a.getProvider)
	r.Put("/providers/{resourceID}", a.updateProvider)

	a.registerGlobalConfig(r)
}

func (a *api) registerGlobalConfig(r chi.Router) {
	r.Get("/model-profiles", a.listGlobalModelProfiles)
	r.Post("/model-profiles", a.createGlobalModelProfile)
	r.Get("/model-profiles/{resourceID}", a.getGlobalModelProfile)
	r.Put("/model-profiles/{resourceID}", a.updateGlobalModelProfile)
	r.Get("/runtimes", a.listGlobalRuntimes)
	r.Post("/runtimes", a.createGlobalRuntime)
	r.Get("/runtimes/{resourceID}", a.getGlobalRuntime)
	r.Put("/runtimes/{resourceID}", a.updateGlobalRuntime)
	r.Get("/executor-profiles", a.listGlobalExecutorProfiles)
	r.Post("/executor-profiles", a.createGlobalExecutorProfile)
	r.Get("/executor-profiles/{resourceID}", a.getGlobalExecutorProfile)
	r.Put("/executor-profiles/{resourceID}", a.updateGlobalExecutorProfile)
	r.Get("/agents", a.listGlobalAgents)
	r.Post("/agents", a.createGlobalAgent)
	r.Get("/agents/{resourceID}", a.getGlobalAgent)
	r.Put("/agents/{resourceID}", a.updateGlobalAgent)
}

func (a *api) registerScopedConfig(r chi.Router) {
	r.Get("/model-profiles", a.listProjectModelProfiles)
	r.Post("/model-profiles", a.createProjectModelProfile)
	r.Get("/model-profiles/{resourceID}", a.getProjectModelProfile)
	r.Put("/model-profiles/{resourceID}", a.updateProjectModelProfile)
	r.Get("/runtimes", a.listProjectRuntimes)
	r.Post("/runtimes", a.createProjectRuntime)
	r.Get("/runtimes/{resourceID}", a.getProjectRuntime)
	r.Put("/runtimes/{resourceID}", a.updateProjectRuntime)
	r.Get("/executor-profiles", a.listProjectExecutorProfiles)
	r.Post("/executor-profiles", a.createProjectExecutorProfile)
	r.Get("/executor-profiles/{resourceID}", a.getProjectExecutorProfile)
	r.Put("/executor-profiles/{resourceID}", a.updateProjectExecutorProfile)
	r.Get("/agents", a.listProjectAgents)
	r.Post("/agents", a.createProjectAgent)
	r.Get("/agents/{resourceID}", a.getProjectAgent)
	r.Put("/agents/{resourceID}", a.updateProjectAgent)
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
func scopeFromProject(w http.ResponseWriter, r *http.Request) (*string, bool) {
	id, ok := pathUUID(w, r, "projectID")
	if !ok {
		return nil, false
	}
	return &id, true
}
func resourceID(w http.ResponseWriter, r *http.Request) (string, bool) {
	return pathUUID(w, r, "resourceID")
}

func (a *api) listProjects(w http.ResponseWriter, r *http.Request) {
	values, err := a.service.ListProjects(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]ProjectDTO, 0, len(values))
	for _, v := range values {
		out = append(out, projectDTO(v))
	}
	writeJSON(w, 200, out)
}
func (a *api) createProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	branch := req.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	v, err := a.service.CreateProject(r.Context(), store.Project{Name: req.Name, RepositoryPath: req.RepositoryPath, DefaultBranch: branch, WorkflowSettings: req.WorkflowSettings})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 201, projectDTO(v))
}
func (a *api) getProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	v, err := a.service.GetProject(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, projectDTO(v))
}
func (a *api) updateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	current, err := a.service.GetProject(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req UpdateProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name != nil {
		current.Name = *req.Name
	}
	if req.RepositoryPath != nil {
		current.RepositoryPath = *req.RepositoryPath
	}
	if req.DefaultBranch != nil {
		current.DefaultBranch = *req.DefaultBranch
	}
	if req.WorkflowSettings != nil {
		current.WorkflowSettings = *req.WorkflowSettings
	}
	v, err := a.service.UpdateProject(r.Context(), current)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, projectDTO(v))
}

func (a *api) listProviders(w http.ResponseWriter, r *http.Request) {
	values, err := a.service.ListProviders(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]ProviderDTO, 0, len(values))
	for _, v := range values {
		out = append(out, providerDTO(v))
	}
	writeJSON(w, 200, out)
}
func (a *api) createProvider(w http.ResponseWriter, r *http.Request) {
	var req CreateProviderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	v, err := a.service.CreateProvider(r.Context(), store.Provider{Name: req.Name, Kind: req.Kind, BaseURL: req.BaseURL, CredentialRef: req.CredentialRef, Enabled: boolDefault(req.Enabled, true), SafeMetadata: req.SafeMetadata})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 201, providerDTO(v))
}
func (a *api) getProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	v, err := a.service.GetProvider(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, providerDTO(v))
}
func (a *api) updateProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	var req CreateProviderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	current, err := a.service.GetProvider(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	current.Name = req.Name
	current.Kind = req.Kind
	current.BaseURL = req.BaseURL
	current.CredentialRef = req.CredentialRef
	current.Enabled = boolDefault(req.Enabled, current.Enabled)
	current.SafeMetadata = req.SafeMetadata
	v, err := a.service.UpdateProvider(r.Context(), current)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, providerDTO(v))
}

func (a *api) listModelProfiles(w http.ResponseWriter, r *http.Request, scope *string) {
	values, err := a.service.ListModelProfiles(r.Context(), scope)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]ModelProfileDTO, 0, len(values))
	for _, v := range values {
		out = append(out, modelProfileDTO(v))
	}
	writeJSON(w, 200, out)
}
func (a *api) createModelProfile(w http.ResponseWriter, r *http.Request, scope *string) {
	var req CreateModelProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	v, err := a.service.CreateModelProfile(r.Context(), store.ModelProfile{ProjectID: scope, ProviderID: req.ProviderID, Name: req.Name, Model: req.Model, Temperature: req.Temperature, MaxTokens: req.MaxTokens, MaxConcurrent: req.MaxConcurrent, GenerationSettings: req.GenerationSettings, Enabled: boolDefault(req.Enabled, true)})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 201, modelProfileDTO(v))
}
func (a *api) getModelProfile(w http.ResponseWriter, r *http.Request, scope *string) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	v, err := a.service.GetModelProfile(r.Context(), scope, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, modelProfileDTO(v))
}
func (a *api) updateModelProfile(w http.ResponseWriter, r *http.Request, scope *string) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	var req CreateModelProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	current, err := a.service.GetModelProfile(r.Context(), scope, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	current.ProviderID = req.ProviderID
	current.Name = req.Name
	current.Model = req.Model
	current.Temperature = req.Temperature
	current.MaxTokens = req.MaxTokens
	current.MaxConcurrent = req.MaxConcurrent
	current.GenerationSettings = req.GenerationSettings
	current.Enabled = boolDefault(req.Enabled, current.Enabled)
	v, err := a.service.UpdateModelProfile(r.Context(), scope, current)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, modelProfileDTO(v))
}
func (a *api) listGlobalModelProfiles(w http.ResponseWriter, r *http.Request) {
	a.listModelProfiles(w, r, nil)
}
func (a *api) createGlobalModelProfile(w http.ResponseWriter, r *http.Request) {
	a.createModelProfile(w, r, nil)
}
func (a *api) getGlobalModelProfile(w http.ResponseWriter, r *http.Request) {
	a.getModelProfile(w, r, nil)
}
func (a *api) updateGlobalModelProfile(w http.ResponseWriter, r *http.Request) {
	a.updateModelProfile(w, r, nil)
}
func (a *api) listProjectModelProfiles(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.listModelProfiles(w, r, s)
	}
}
func (a *api) createProjectModelProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.createModelProfile(w, r, s)
	}
}
func (a *api) getProjectModelProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.getModelProfile(w, r, s)
	}
}
func (a *api) updateProjectModelProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.updateModelProfile(w, r, s)
	}
}

func (a *api) listRuntimes(w http.ResponseWriter, r *http.Request, scope *string) {
	values, err := a.service.ListRuntimes(r.Context(), scope)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]RuntimeDTO, 0, len(values))
	for _, v := range values {
		out = append(out, runtimeDTO(v))
	}
	writeJSON(w, 200, out)
}
func (a *api) createRuntime(w http.ResponseWriter, r *http.Request, scope *string) {
	var req CreateRuntimeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	v, err := a.service.CreateRuntime(r.Context(), store.Runtime{ProjectID: scope, Name: req.Name, Kind: req.Kind, Image: req.Image, CPULimitMillis: req.CPULimitMillis, MemoryLimitBytes: req.MemoryLimitBytes, PIDLimit: req.PIDLimit, TimeoutSeconds: req.TimeoutSeconds, NetworkPolicy: req.NetworkPolicy, WorkspacePolicy: "issue", AllowedSecretRefs: req.AllowedSecretRefs, Capabilities: req.Capabilities, Enabled: boolDefault(req.Enabled, true)})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 201, runtimeDTO(v))
}
func (a *api) getRuntime(w http.ResponseWriter, r *http.Request, scope *string) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	v, err := a.service.GetRuntime(r.Context(), scope, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, runtimeDTO(v))
}
func (a *api) updateRuntime(w http.ResponseWriter, r *http.Request, scope *string) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	var req CreateRuntimeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	current, err := a.service.GetRuntime(r.Context(), scope, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	current.Name = req.Name
	current.Kind = req.Kind
	current.Image = req.Image
	current.CPULimitMillis = req.CPULimitMillis
	current.MemoryLimitBytes = req.MemoryLimitBytes
	current.PIDLimit = req.PIDLimit
	current.TimeoutSeconds = req.TimeoutSeconds
	current.NetworkPolicy = req.NetworkPolicy
	current.AllowedSecretRefs = req.AllowedSecretRefs
	current.Capabilities = req.Capabilities
	current.Enabled = boolDefault(req.Enabled, current.Enabled)
	v, err := a.service.UpdateRuntime(r.Context(), scope, current)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, runtimeDTO(v))
}
func (a *api) listGlobalRuntimes(w http.ResponseWriter, r *http.Request)   { a.listRuntimes(w, r, nil) }
func (a *api) createGlobalRuntime(w http.ResponseWriter, r *http.Request) { a.createRuntime(w, r, nil) }
func (a *api) getGlobalRuntime(w http.ResponseWriter, r *http.Request)    { a.getRuntime(w, r, nil) }
func (a *api) updateGlobalRuntime(w http.ResponseWriter, r *http.Request) { a.updateRuntime(w, r, nil) }
func (a *api) listProjectRuntimes(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.listRuntimes(w, r, s)
	}
}
func (a *api) createProjectRuntime(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.createRuntime(w, r, s)
	}
}
func (a *api) getProjectRuntime(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.getRuntime(w, r, s)
	}
}
func (a *api) updateProjectRuntime(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.updateRuntime(w, r, s)
	}
}

func (a *api) listExecutorProfiles(w http.ResponseWriter, r *http.Request, scope *string) {
	values, err := a.service.ListExecutorProfiles(r.Context(), scope)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]ExecutorProfileDTO, 0, len(values))
	for _, v := range values {
		out = append(out, executorProfileDTO(v))
	}
	writeJSON(w, 200, out)
}
func (a *api) createExecutorProfile(w http.ResponseWriter, r *http.Request, scope *string) {
	var req CreateExecutorProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	v, err := a.service.CreateExecutorProfile(r.Context(), store.ExecutorProfile{ProjectID: scope, Name: req.Name, Engine: req.Engine, ModelProfileID: req.ModelProfileID, RuntimeID: req.RuntimeID, EngineSettings: req.EngineSettings, Enabled: boolDefault(req.Enabled, true)})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 201, executorProfileDTO(v))
}
func (a *api) getExecutorProfile(w http.ResponseWriter, r *http.Request, scope *string) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	v, err := a.service.GetExecutorProfile(r.Context(), scope, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, executorProfileDTO(v))
}
func (a *api) updateExecutorProfile(w http.ResponseWriter, r *http.Request, scope *string) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	var req CreateExecutorProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	current, err := a.service.GetExecutorProfile(r.Context(), scope, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	current.Name = req.Name
	current.Engine = req.Engine
	current.ModelProfileID = req.ModelProfileID
	current.RuntimeID = req.RuntimeID
	current.EngineSettings = req.EngineSettings
	current.Enabled = boolDefault(req.Enabled, current.Enabled)
	v, err := a.service.UpdateExecutorProfile(r.Context(), scope, current)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, executorProfileDTO(v))
}
func (a *api) listGlobalExecutorProfiles(w http.ResponseWriter, r *http.Request) {
	a.listExecutorProfiles(w, r, nil)
}
func (a *api) createGlobalExecutorProfile(w http.ResponseWriter, r *http.Request) {
	a.createExecutorProfile(w, r, nil)
}
func (a *api) getGlobalExecutorProfile(w http.ResponseWriter, r *http.Request) {
	a.getExecutorProfile(w, r, nil)
}
func (a *api) updateGlobalExecutorProfile(w http.ResponseWriter, r *http.Request) {
	a.updateExecutorProfile(w, r, nil)
}
func (a *api) listProjectExecutorProfiles(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.listExecutorProfiles(w, r, s)
	}
}
func (a *api) createProjectExecutorProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.createExecutorProfile(w, r, s)
	}
}
func (a *api) getProjectExecutorProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.getExecutorProfile(w, r, s)
	}
}
func (a *api) updateProjectExecutorProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.updateExecutorProfile(w, r, s)
	}
}

func (a *api) listAgents(w http.ResponseWriter, r *http.Request, scope *string) {
	values, err := a.service.ListAgents(r.Context(), scope)
	if err != nil {
		writeAppError(w, err)
		return
	}
	out := make([]AgentDTO, 0, len(values))
	for _, v := range values {
		out = append(out, agentDTO(v))
	}
	writeJSON(w, 200, out)
}
func (a *api) createAgent(w http.ResponseWriter, r *http.Request, scope *string) {
	var req CreateAgentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	limit := req.ConcurrencyLimit
	if limit == 0 {
		limit = 1
	}
	state := req.State
	if state == "" {
		state = "ENABLED"
	}
	v, err := a.service.CreateAgent(r.Context(), store.Agent{ProjectID: scope, Name: req.Name, RoleInstructions: req.RoleInstructions, ExecutorProfileID: req.ExecutorProfileID, ConcurrencyLimit: limit, State: state})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 201, agentDTO(v))
}
func (a *api) getAgent(w http.ResponseWriter, r *http.Request, scope *string) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	v, err := a.service.GetAgent(r.Context(), scope, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, agentDTO(v))
}
func (a *api) updateAgent(w http.ResponseWriter, r *http.Request, scope *string) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	var req CreateAgentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	current, err := a.service.GetAgent(r.Context(), scope, id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	current.Name = req.Name
	current.RoleInstructions = req.RoleInstructions
	current.ExecutorProfileID = req.ExecutorProfileID
	if req.ConcurrencyLimit != 0 {
		current.ConcurrencyLimit = req.ConcurrencyLimit
	}
	if req.State != "" {
		current.State = req.State
	}
	v, err := a.service.UpdateAgent(r.Context(), scope, current)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, 200, agentDTO(v))
}
func (a *api) listGlobalAgents(w http.ResponseWriter, r *http.Request)   { a.listAgents(w, r, nil) }
func (a *api) createGlobalAgent(w http.ResponseWriter, r *http.Request) { a.createAgent(w, r, nil) }
func (a *api) getGlobalAgent(w http.ResponseWriter, r *http.Request)    { a.getAgent(w, r, nil) }
func (a *api) updateGlobalAgent(w http.ResponseWriter, r *http.Request) { a.updateAgent(w, r, nil) }
func (a *api) listProjectAgents(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.listAgents(w, r, s)
	}
}
func (a *api) createProjectAgent(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.createAgent(w, r, s)
	}
}
func (a *api) getProjectAgent(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.getAgent(w, r, s)
	}
}
func (a *api) updateProjectAgent(w http.ResponseWriter, r *http.Request) {
	s, ok := scopeFromProject(w, r)
	if ok {
		a.updateAgent(w, r, s)
	}
}
