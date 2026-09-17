package runexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/app"
	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode"
	"github.com/brantje/agent-board/apps/server/internal/evidence"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
	"github.com/brantje/agent-board/apps/server/internal/httpapi"
	"github.com/brantje/agent-board/apps/server/internal/repository"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/scheduler"
	"github.com/brantje/agent-board/apps/server/internal/secrets"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/brantje/agent-board/apps/server/internal/store/postgres"
	"github.com/brantje/agent-board/apps/server/internal/workspace"
	"github.com/jackc/pgx/v5"
)

const (
	delegationExecutionSessionID       = "ses_delegation_e2e"
	delegationExecutionCallID          = "execution-e2e-request-1"
	delegationExecutionTask            = "inspect scheduler ownership"
	delegationParentBoundaryFile       = "parent-before-delegation.txt"
	delegationChildBoundaryFile        = "delegate-change.txt"
	delegationExecutionRoleParent      = "parent"
	delegationExecutionRoleDelegate    = "delegate"
	delegationExecutionRoleParentResume = "parent-resume"
	delegationExecutionRoleDenied      = "denied"
)

type delegationExecutionRunnerClient struct {
	*launcherClient
	targetAgentID string

	mu            sync.Mutex
	payloads      map[string][]byte
	servers       map[string]*httptest.Server
	processes     map[string]*launcherDialTransport
	roles         map[string]string
	allowedStarts int
	deniedStarts  int
	onceReplies   int
	rejectReplies int
}

func newDelegationExecutionRunnerClient(targetAgentID string) *delegationExecutionRunnerClient {
	return &delegationExecutionRunnerClient{
		launcherClient: newLauncherClient("", "", 0, nil),
		targetAgentID:  targetAgentID,
		payloads:       make(map[string][]byte),
		servers:        make(map[string]*httptest.Server),
		processes:      make(map[string]*launcherDialTransport),
		roles:          make(map[string]string),
	}
}

func (c *delegationExecutionRunnerClient) Start(_ context.Context, sessionID string, request runner.Request) (runner.ProcessSession, error) {
	if len(request.Command) < 8 || request.Command[0] != "sh" || request.Command[1] != "-c" || !strings.Contains(request.Command[2], "opencode serve") {
		return nil, fmt.Errorf("delegation E2E runner received unexpected command: %v", request.Command)
	}
	delegationEnabled := strings.TrimSpace(request.Command[7]) != ""

	c.mu.Lock()
	role := delegationExecutionRoleDenied
	emitDelegation := false
	if delegationEnabled {
		if c.allowedStarts == 0 {
			role = delegationExecutionRoleParent
			emitDelegation = true
		} else {
			role = delegationExecutionRoleParentResume
		}
		c.allowedStarts++
	} else {
		if c.deniedStarts == 0 {
			role = delegationExecutionRoleDelegate
		}
		c.deniedStarts++
	}
	payload := append([]byte(nil), c.payloads[sessionID]...)
	c.roles[sessionID] = role
	c.mu.Unlock()

	if role == delegationExecutionRoleDelegate {
		hasParentState, err := delegationExecutionBundleHasFile(payload, delegationParentBoundaryFile)
		if err != nil {
			return nil, fmt.Errorf("inspect delegated inbound Workspace: %w", err)
		}
		if !hasParentState {
			return nil, fmt.Errorf("delegated execution did not inherit parent Workspace state")
		}
	}
	if role == delegationExecutionRoleParentResume {
		hasDelegateState, err := delegationExecutionBundleHasFile(payload, delegationChildBoundaryFile)
		if err != nil {
			return nil, fmt.Errorf("inspect resumed parent Workspace: %w", err)
		}
		if !hasDelegateState {
			return nil, fmt.Errorf("resumed parent did not inherit delegated Workspace state")
		}
	}

	native := &delegationExecutionNativeOpenCode{
		client:            c,
		targetAgentID:     c.targetAgentID,
		delegationEnabled: delegationEnabled,
		emitDelegation:    emitDelegation,
		events:            make(chan string, 8),
	}
	server := httptest.NewServer(native.handler())
	process := &launcherDialTransport{id: sessionID, done: make(chan struct{})}

	c.mu.Lock()
	if previous := c.servers[sessionID]; previous != nil {
		c.mu.Unlock()
		server.Close()
		return nil, fmt.Errorf("delegation E2E runner session %s already started", sessionID)
	}
	c.servers[sessionID] = server
	c.processes[sessionID] = process
	c.mu.Unlock()
	return process, nil
}

func (c *delegationExecutionRunnerClient) Attach(sessionID string) (runner.ProcessSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	process := c.processes[sessionID]
	if process == nil {
		return nil, nil
	}
	return process, nil
}

func (c *delegationExecutionRunnerClient) DialSession(ctx context.Context, sessionID, network, _ string) (net.Conn, error) {
	c.mu.Lock()
	server := c.servers[sessionID]
	c.mu.Unlock()
	if server == nil {
		return nil, fmt.Errorf("delegation E2E runner has no native OpenCode server for session %s", sessionID)
	}
	parsed, err := url.Parse(server.URL)
	if err != nil {
		return nil, err
	}
	return (&net.Dialer{}).DialContext(ctx, network, parsed.Host)
}

func (c *delegationExecutionRunnerClient) SendTransfer(_ context.Context, sessionID, _, direction string, payload []byte, progress runner.TransferProgressFunc) error {
	if direction == "to_runner" {
		c.mu.Lock()
		c.payloads[sessionID] = append([]byte(nil), payload...)
		c.mu.Unlock()
	}
	if progress != nil && len(payload) > 0 {
		progress(runner.TransferProgress{BytesTransferred: int64(len(payload)), TotalBytes: int64(len(payload))})
	}
	return nil
}

func (c *delegationExecutionRunnerClient) ReceiveTransfer(_ context.Context, sessionID string, progress runner.TransferProgressFunc) (string, []byte, error) {
	c.mu.Lock()
	payload := append([]byte(nil), c.payloads[sessionID]...)
	role := c.roles[sessionID]
	c.mu.Unlock()
	if len(payload) == 0 {
		return "", nil, fmt.Errorf("delegation E2E runner has no workspace payload for session %s", sessionID)
	}
	var err error
	switch role {
	case delegationExecutionRoleParent:
		payload, err = delegationExecutionMutateBundle(payload, delegationParentBoundaryFile, "parent state before delegation\n")
	case delegationExecutionRoleDelegate:
		payload, err = delegationExecutionMutateBundle(payload, delegationChildBoundaryFile, "delegated state returned to parent\n")
	}
	if err != nil {
		return "", nil, err
	}
	if progress != nil {
		progress(runner.TransferProgress{BytesTransferred: int64(len(payload)), TotalBytes: int64(len(payload))})
	}
	return "delegation-e2e-return-" + sessionID, payload, nil
}

func (*delegationExecutionRunnerClient) ConfirmTransferApplied(context.Context, string, string) error {
	return nil
}

func (c *delegationExecutionRunnerClient) Close() error {
	c.mu.Lock()
	servers := make([]*httptest.Server, 0, len(c.servers))
	for _, server := range c.servers {
		servers = append(servers, server)
	}
	processes := make([]*launcherDialTransport, 0, len(c.processes))
	for _, process := range c.processes {
		processes = append(processes, process)
	}
	c.mu.Unlock()
	for _, process := range processes {
		process.finish()
	}
	for _, server := range servers {
		server.Close()
	}
	return c.launcherClient.Close()
}

func (c *delegationExecutionRunnerClient) recordPermissionReply(reply string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch reply {
	case "once":
		c.onceReplies++
	case "reject":
		c.rejectReplies++
	}
}

func (c *delegationExecutionRunnerClient) stats() (int, int, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.allowedStarts, c.deniedStarts, c.onceReplies, c.rejectReplies
}

type delegationExecutionNativeOpenCode struct {
	client            *delegationExecutionRunnerClient
	targetAgentID     string
	delegationEnabled bool
	emitDelegation    bool
	events            chan string

	mu             sync.Mutex
	prompted       bool
	settled        bool
	completionSent bool
	activeUntil    time.Time
}

func (h *delegationExecutionNativeOpenCode) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		delegationExecutionNativeJSON(w, map[string]any{"healthy": true, "version": "delegation-e2e"})
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, _ *http.Request) {
		delegationExecutionNativeJSON(w, map[string]any{"data": map[string]any{"id": delegationExecutionSessionID, "directory": "/workspace"}})
	})
	mux.HandleFunc("GET /event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"connected\",\"type\":\"server.connected\",\"properties\":{}}\n\n")
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-h.events:
				_, _ = io.WriteString(w, "data: "+event+"\n\n")
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("POST /session/"+delegationExecutionSessionID+"/prompt_async", func(w http.ResponseWriter, _ *http.Request) {
		h.mu.Lock()
		if h.prompted {
			h.mu.Unlock()
			http.Error(w, "prompt already sent", http.StatusConflict)
			return
		}
		h.prompted = true
		h.activeUntil = time.Now().Add(700 * time.Millisecond)
		h.mu.Unlock()
		go func() {
			time.Sleep(350 * time.Millisecond)
			if h.delegationEnabled && !h.emitDelegation {
				h.mu.Lock()
				h.settled = true
				h.mu.Unlock()
				return
			}
			event := h.permissionEvent()
			h.events <- event
			if h.emitDelegation {
				// Replay the same externally visible tool call. The production OpenCode
				// tracker and canonical request key must keep it to one delegation/Run/job.
				h.events <- event
			}
		}()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		h.mu.Lock()
		active := h.prompted && (time.Now().Before(h.activeUntil) || !h.settled)
		h.mu.Unlock()
		if active {
			delegationExecutionNativeJSON(w, map[string]any{delegationExecutionSessionID: map[string]any{"type": "busy"}})
			return
		}
		delegationExecutionNativeJSON(w, map[string]any{})
	})
	mux.HandleFunc("GET /question", func(w http.ResponseWriter, _ *http.Request) {
		delegationExecutionNativeJSON(w, []any{})
	})
	mux.HandleFunc("GET /permission", func(w http.ResponseWriter, _ *http.Request) {
		delegationExecutionNativeJSON(w, []any{})
	})
	mux.HandleFunc("POST /permission/per_delegation_e2e/reply", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Reply string `json:"reply"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid permission reply", http.StatusBadRequest)
			return
		}
		h.client.recordPermissionReply(payload.Reply)
		emitCompletion := false
		h.mu.Lock()
		if payload.Reply == "once" && h.emitDelegation && !h.completionSent {
			h.completionSent = true
			emitCompletion = true
		}
		h.settled = true
		h.mu.Unlock()
		if emitCompletion {
			h.events <- h.completedDelegationEvent()
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /provider", func(w http.ResponseWriter, _ *http.Request) {
		delegationExecutionNativeJSON(w, map[string]any{"all": []any{}})
	})
	mux.HandleFunc("GET /session/"+delegationExecutionSessionID+"/message", func(w http.ResponseWriter, _ *http.Request) {
		delegationExecutionNativeJSON(w, []any{})
	})
	return mux
}

func (h *delegationExecutionNativeOpenCode) permissionEvent() string {
	permission := map[string]any{
		"id":         "per_delegation_e2e",
		"sessionID":  delegationExecutionSessionID,
		"permission": "agent_board_delegate",
		"patterns":   []string{delegationExecutionCallID},
		"metadata": map[string]any{
			"tool":          "delegate_task",
			"targetAgentId": h.targetAgentID,
			"task":          delegationExecutionTask,
			"callID":        delegationExecutionCallID,
		},
		"tool": map[string]any{"messageID": "msg_delegation_e2e", "callID": delegationExecutionCallID},
	}
	event, _ := json.Marshal(map[string]any{
		"id":         "evt_delegation_e2e",
		"type":       "permission.asked",
		"properties": permission,
	})
	return string(event)
}

func (h *delegationExecutionNativeOpenCode) completedDelegationEvent() string {
	event, _ := json.Marshal(map[string]any{
		"id":   "evt_delegation_e2e_completed",
		"type": "message.part.updated",
		"properties": map[string]any{
			"sessionID": delegationExecutionSessionID,
			"part": map[string]any{
				"id":        "part_delegation_e2e",
				"callID":    delegationExecutionCallID,
				"sessionID": delegationExecutionSessionID,
				"type":      "tool",
				"tool":      "delegate_task",
				"state": map[string]any{
					"status": "completed",
					"input": map[string]any{
						"targetAgentId": h.targetAgentID,
						"task":          delegationExecutionTask,
					},
				},
			},
		},
	})
	return string(event)
}

func delegationExecutionNativeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

type delegationExecutionRunnerConnector struct {
	client runnerClient
}

func (c delegationExecutionRunnerConnector) Connect(context.Context, string, string) (runnerClient, error) {
	return c.client, nil
}

func TestDelegationTrustedExecutionEndToEnd(t *testing.T) {
	baseDatabaseURL := strings.TrimSpace(os.Getenv("AGENT_BOARD_TEST_DATABASE_URL"))
	if baseDatabaseURL == "" {
		t.Skip("AGENT_BOARD_TEST_DATABASE_URL is required for delegation E2E")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	databaseURL := isolatedDelegationExecutionDatabase(t, ctx, baseDatabaseURL)
	database, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetEngineRegistered(func(name string) bool { return name == opencode.Name })

	cipher, err := secrets.NewAESGCM(1, map[int][]byte{1: bytes.Repeat([]byte{0x5a}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	secretService, err := secrets.NewService(database, cipher)
	if err != nil {
		t.Fatal(err)
	}

	repositoryPath := createScriptedFixtureRepository(t, ctx)
	repositoryPolicy, err := repository.NewPolicy([]string{filepath.Dir(repositoryPath)})
	if err != nil {
		t.Fatal(err)
	}
	git, err := workspace.NewGitCLI("")
	if err != nil {
		t.Fatal(err)
	}
	materializer, err := workspace.NewMaterializer(database, repositoryPolicy, git, filepath.Join(t.TempDir(), "workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	services, err := app.NewServicesWithRuntimes(database, materializer, nil, secretService)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := services.Close(); err != nil {
			t.Errorf("close delegation E2E services: %v", err)
		}
	}()

	router := httpapi.NewRouterWithSecrets(services.ControlPlane, secretService)
	prefix := fmt.Sprintf("D%08X", uint32(time.Now().UnixNano()))
	var project httpapi.ProjectDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects", fmt.Sprintf(`{"name":"Delegation execution E2E","issuePrefix":"%s","repositoryPath":%q,"defaultBranch":"main","workflowSettings":{}}`, prefix, repositoryPath), http.StatusCreated, &project)

	var provider httpapi.ProviderDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/providers", `{"name":"Provider","kind":"openrouter","credential":"delegation-e2e-provider-key","enabled":true,"safeMetadata":{}}`, http.StatusCreated, &provider)
	var model httpapi.ModelProfileDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/model-profiles", fmt.Sprintf(`{"providerId":"%s","name":"Model","model":"test-model","generationSettings":{},"enabled":true}`, provider.ID), http.StatusCreated, &model)

	var parent httpapi.AgentDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/agents", fmt.Sprintf(`{"name":"Parent","roleInstructions":"delegate bounded work","engine":%q,"modelProfileId":"%s","engineSettings":{},"concurrencyLimit":1,"allowDelegation":true,"state":"ENABLED"}`, opencode.Name, model.ID), http.StatusCreated, &parent)
	if !parent.AllowDelegation {
		t.Fatal("public Agent create did not persist allowDelegation")
	}
	var target httpapi.AgentDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/agents", fmt.Sprintf(`{"name":"Target","roleInstructions":"perform bounded work","engine":%q,"modelProfileId":"%s","engineSettings":{},"concurrencyLimit":1,"allowDelegation":false,"state":"ENABLED"}`, opencode.Name, model.ID), http.StatusCreated, &target)

	runnerRecord, err := database.CreateRunner(ctx, store.Runner{Name: "Delegation E2E Runner", TokenHash: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ObserveRunner(ctx, runnerRecord.ID, json.RawMessage(`{"max_active_sessions":4}`)); err != nil {
		t.Fatal(err)
	}
	database.SetRunnerCandidates(func(name string) []string {
		if name != opencode.Name {
			return nil
		}
		return []string{runnerRecord.ID}
	})

	var issue httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues", `{"title":"Delegate through trusted execution","description":"bounded delegation","status":"TODO"}`, http.StatusCreated, &issue)
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues/"+issue.ID+"/assignment", fmt.Sprintf(`{"assignedTo":{"type":"AGENT","id":"%s"}}`, parent.ID), http.StatusOK, nil)
	var execution httpapi.IssueExecutionStateDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID+"/execution", "", http.StatusOK, &execution)
	if execution.ActiveRun == nil || execution.ActiveRun.AgentID == nil || *execution.ActiveRun.AgentID != parent.ID {
		t.Fatalf("API assignment did not create authoritative parent Run: %+v", execution)
	}
	parentRun := *execution.ActiveRun
	var before httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID, "", http.StatusOK, &before)

	baseBlobs, err := evidence.NewFileBlobStore(filepath.Join(t.TempDir(), "evidence"), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := evidence.NewRedactingBlobStore(baseBlobs, services.Redaction)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := evidence.NewRecorder(services.ExecutionStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := evidence.NewOutputRecorder(services.ExecutionStore, blobs, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	engines, err := engine.NewRegistry(opencode.New())
	if err != nil {
		t.Fatal(err)
	}
	runnerClient := newDelegationExecutionRunnerClient(target.ID)
	defer func() {
		if err := runnerClient.Close(); err != nil {
			t.Errorf("close delegation E2E runner: %v", err)
		}
	}()
	transportSessions, err := newLauncherExecutionSessionService(services.ExecutionStore, runnerClient)
	if err != nil {
		t.Fatal(err)
	}
	preparer, err := executioncontext.NewPreparer(services.ExecutionContext, secretService, services.ExecutionStore, services.Redaction)
	if err != nil {
		t.Fatal(err)
	}
	authorizedSessions, err := app.NewAuthorizedExecutionSessionService(transportSessions, preparer)
	if err != nil {
		t.Fatal(err)
	}
	processor, err := NewProcessor(
		services.ExecutionStore,
		services.ExecutionContext,
		services.RuntimeInstances,
		authorizedSessions,
		engines,
		recorder,
		output,
		git,
		delegationExecutionRunnerConnector{client: runnerClient},
	)
	if err != nil {
		t.Fatal(err)
	}
	processor.SetWorkspaceEnsurer(services.Workspaces)
	config := scheduler.DefaultConfig("delegation-api-e2e")
	config.PollInterval = 10 * time.Millisecond
	config.LeaseDuration = 3 * time.Second
	config.HeartbeatInterval = 500 * time.Millisecond
	config.CapacityBackoff = 10 * time.Millisecond
	config.MaxInFlight = 1
	config.PersistedEvents = processor
	coordinator, err := scheduler.New(services.ExecutionStore, processor, processor, config)
	if err != nil {
		t.Fatal(err)
	}
	schedulerCtx, stopScheduler := context.WithCancel(ctx)
	schedulerDone := make(chan error, 1)
	go func() {
		schedulerDone <- coordinator.Run(schedulerCtx)
		close(schedulerDone)
	}()
	defer func() {
		stopScheduler()
		select {
		case err := <-schedulerDone:
			if err != nil {
				t.Errorf("delegation E2E scheduler stopped with error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("delegation E2E scheduler did not stop")
		}
	}()

	parentTerminal := waitForDelegationExecutionRun(t, ctx, router, project.ID, parentRun.ID)
	if parentTerminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("parent Run status=%s failure=%v want READY_FOR_REVIEW", parentTerminal.Status, parentTerminal.FailureReason)
	}

	var listed []httpapi.DelegationDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs/"+parentRun.ID+"/delegations", "", http.StatusOK, &listed)
	if len(listed) != 1 || listed[0].RequestKey != delegationExecutionCallID || listed[0].TargetAgentID != target.ID || listed[0].WorkspaceAccess != store.DelegationWorkspaceAccessWrite {
		t.Fatalf("parent delegation list=%+v", listed)
	}
	created := listed[0]
	var lineage httpapi.DelegationDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs/"+created.DelegatedRunID+"/delegation", "", http.StatusOK, &lineage)
	if lineage.ID != created.ID || lineage.ParentRunID != parentRun.ID || lineage.ParentAgentID != parent.ID || lineage.TargetAgentID != target.ID || lineage.IssueID != issue.ID || lineage.WorkspaceAccess != store.DelegationWorkspaceAccessWrite {
		t.Fatalf("child lineage=%+v", lineage)
	}

	childTerminal := waitForDelegationExecutionRun(t, ctx, router, project.ID, created.DelegatedRunID)
	if childTerminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("delegated Run status=%s failure=%v want READY_FOR_REVIEW", childTerminal.Status, childTerminal.FailureReason)
	}
	if parentTerminal.WorkspaceID != childTerminal.WorkspaceID || parentTerminal.CurrentBranch == nil || childTerminal.CurrentBranch == nil || *parentTerminal.CurrentBranch != *childTerminal.CurrentBranch {
		t.Fatalf("delegated Workspace continuity parent=%+v child=%+v", parentTerminal, childTerminal)
	}
	allowedStarts, deniedStarts, onceReplies, rejectReplies := runnerClient.stats()
	if allowedStarts != 2 || deniedStarts != 1 || onceReplies < 2 || rejectReplies != 1 {
		t.Fatalf("unexpected OpenCode tool-path stats allowed=%d denied=%d onceReplies=%d rejectReplies=%d", allowedStarts, deniedStarts, onceReplies, rejectReplies)
	}

	var runs []httpapi.RunDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs", "", http.StatusOK, &runs)
	children := 0
	for _, run := range runs {
		if run.IssueID != issue.ID || run.AgentID == nil || *run.AgentID != target.ID {
			continue
		}
		children++
		if run.ID != created.DelegatedRunID || run.WorkspaceID != parentRun.WorkspaceID || run.Status != "READY_FOR_REVIEW" {
			t.Fatalf("ordinary delegated Run=%+v", run)
		}
	}
	if children != 1 {
		t.Fatalf("delegated Run count=%d want 1 after replayed delegate_task permission", children)
	}

	jobCount, jobState := delegationExecutionStartJob(t, ctx, databaseURL, project.ID, created.DelegatedRunID)
	if jobCount != 1 || jobState != "DONE" {
		t.Fatalf("delegated START scheduler jobs count=%d state=%q want one DONE job", jobCount, jobState)
	}
	resumeCount, resumeState := delegationExecutionResumeJob(t, ctx, databaseURL, project.ID, parentRun.ID)
	if resumeCount != 1 || resumeState != "DONE" {
		t.Fatalf("parent RESUME scheduler jobs count=%d state=%q want one DONE job", resumeCount, resumeState)
	}

	workspaceRecord, err := database.GetWorkspace(ctx, project.ID, parentRun.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{delegationParentBoundaryFile, delegationChildBoundaryFile} {
		if _, err := os.Stat(filepath.Join(workspaceRecord.Path, name)); err != nil {
			t.Fatalf("final authoritative Workspace missing %s: %v", name, err)
		}
	}

	var afterSchedule httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+issue.ID, "", http.StatusOK, &afterSchedule)
	if afterSchedule.Status != before.Status || afterSchedule.AssignedTo == nil || before.AssignedTo == nil || *afterSchedule.AssignedTo != *before.AssignedTo {
		t.Fatalf("delegation execution changed Issue authority: before=%+v after=%+v", before, afterSchedule)
	}

	parent.AllowDelegation = false
	delegationExecutionJSON(t, router, http.MethodPut, "/api/projects/"+project.ID+"/agents/"+parent.ID, fmt.Sprintf(`{"name":%q,"roleInstructions":%q,"engine":%q,"modelProfileId":"%s","engineSettings":{},"concurrencyLimit":%d,"allowDelegation":false,"state":%q}`, parent.Name, parent.RoleInstructions, parent.Engine, parent.ModelProfileID, parent.ConcurrencyLimit, parent.State), http.StatusOK, &parent)
	if parent.AllowDelegation {
		t.Fatal("public Agent update did not disable delegation")
	}
	var deniedIssue httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues", `{"title":"Disabled delegation","description":"must reject","status":"TODO"}`, http.StatusCreated, &deniedIssue)
	delegationExecutionJSON(t, router, http.MethodPost, "/api/projects/"+project.ID+"/issues/"+deniedIssue.ID+"/assignment", fmt.Sprintf(`{"assignedTo":{"type":"AGENT","id":"%s"}}`, parent.ID), http.StatusOK, nil)
	var deniedExecution httpapi.IssueExecutionStateDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+deniedIssue.ID+"/execution", "", http.StatusOK, &deniedExecution)
	if deniedExecution.ActiveRun == nil || deniedExecution.ActiveRun.AgentID == nil || *deniedExecution.ActiveRun.AgentID != parent.ID {
		t.Fatalf("disabled-policy fixture has no authoritative parent Run: %+v", deniedExecution)
	}
	deniedTerminal := waitForDelegationExecutionRun(t, ctx, router, project.ID, deniedExecution.ActiveRun.ID)
	if deniedTerminal.Status != "READY_FOR_REVIEW" {
		t.Fatalf("disabled-policy parent status=%s failure=%v want READY_FOR_REVIEW", deniedTerminal.Status, deniedTerminal.FailureReason)
	}
	allowedStarts, deniedStarts, onceReplies, rejectReplies = runnerClient.stats()
	if allowedStarts != 2 || deniedStarts != 2 || onceReplies < 2 || rejectReplies != 2 {
		t.Fatalf("disabled execution did not reject forged delegate_task path: allowed=%d denied=%d onceReplies=%d rejectReplies=%d", allowedStarts, deniedStarts, onceReplies, rejectReplies)
	}
	var denied []httpapi.DelegationDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs/"+deniedExecution.ActiveRun.ID+"/delegations", "", http.StatusOK, &denied)
	if len(denied) != 0 {
		t.Fatalf("disabled parent created lineage: %+v", denied)
	}
	var afterDisabledRuns []httpapi.RunDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/runs", "", http.StatusOK, &afterDisabledRuns)
	for _, run := range afterDisabledRuns {
		if run.IssueID == deniedIssue.ID && run.AgentID != nil && *run.AgentID == target.ID {
			t.Fatalf("disabled delegation created target Run: %+v", run)
		}
	}
	var afterDisabledIssue httpapi.IssueDTO
	delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+project.ID+"/issues/"+deniedIssue.ID, "", http.StatusOK, &afterDisabledIssue)
	if afterDisabledIssue.Status != deniedIssue.Status || afterDisabledIssue.AssignedTo == nil || afterDisabledIssue.AssignedTo.Type != "AGENT" || afterDisabledIssue.AssignedTo.ID != parent.ID {
		t.Fatalf("disabled delegation changed Issue authority: created=%+v after=%+v", deniedIssue, afterDisabledIssue)
	}
}

func waitForDelegationExecutionRun(t *testing.T, ctx context.Context, router http.Handler, projectID, runID string) httpapi.RunDTO {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var last httpapi.RunDTO
	for {
		var runs []httpapi.RunDTO
		delegationExecutionJSON(t, router, http.MethodGet, "/api/projects/"+projectID+"/runs", "", http.StatusOK, &runs)
		for _, run := range runs {
			if run.ID != runID {
				continue
			}
			last = run
			switch run.Status {
			case "READY_FOR_REVIEW", "FAILED", "CANCELLED":
				return run
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for Run %s: %v last=%+v", runID, ctx.Err(), last)
		case <-ticker.C:
		}
	}
}

func delegationExecutionStartJob(t *testing.T, ctx context.Context, databaseURL, projectID, runID string) (int, string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var count int
	var state string
	if err := conn.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(state), '')
		FROM scheduler_jobs
		WHERE project_id=$1 AND run_id=$2 AND kind='START'
	`, projectID, runID).Scan(&count, &state); err != nil {
		t.Fatal(err)
	}
	return count, state
}

func delegationExecutionResumeJob(t *testing.T, ctx context.Context, databaseURL, projectID, runID string) (int, string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var count int
	var state string
	if err := conn.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(state), '')
		FROM scheduler_jobs
		WHERE project_id=$1 AND run_id=$2 AND kind='RESUME'
	`, projectID, runID).Scan(&count, &state); err != nil {
		t.Fatal(err)
	}
	return count, state
}

func delegationExecutionMutateBundle(payload []byte, name, body string) ([]byte, error) {
	repositoryPath, branch, cleanup, err := delegationExecutionCheckoutBundle(payload)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := os.WriteFile(filepath.Join(repositoryPath, name), []byte(body), 0o644); err != nil {
		return nil, err
	}
	commands := [][]string{
		{"add", name},
		{"-c", "user.name=Delegation E2E", "-c", "user.email=delegation-e2e@example.invalid", "commit", "-q", "-m", "delegation E2E " + name},
	}
	for _, args := range commands {
		command := exec.Command("git", append([]string{"-C", repositoryPath}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git %v: %w: %s", args, err, output)
		}
	}
	bundlePath := filepath.Join(filepath.Dir(repositoryPath), "returned.bundle")
	command := exec.Command("git", "-C", repositoryPath, "bundle", "create", bundlePath, "refs/heads/"+branch)
	if output, err := command.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("create returned bundle: %w: %s", err, output)
	}
	return os.ReadFile(bundlePath)
}

func delegationExecutionBundleHasFile(payload []byte, name string) (bool, error) {
	repositoryPath, _, cleanup, err := delegationExecutionCheckoutBundle(payload)
	if err != nil {
		return false, err
	}
	defer cleanup()
	_, err = os.Stat(filepath.Join(repositoryPath, name))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func delegationExecutionCheckoutBundle(payload []byte) (string, string, func(), error) {
	root, err := os.MkdirTemp("", "agent-board-delegation-e2e-*")
	if err != nil {
		return "", "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	bundlePath := filepath.Join(root, "workspace.bundle")
	if err := os.WriteFile(bundlePath, payload, 0o600); err != nil {
		cleanup()
		return "", "", nil, err
	}
	output, err := exec.Command("git", "bundle", "list-heads", bundlePath).CombinedOutput()
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("list bundle heads: %w: %s", err, output)
	}
	fields := strings.Fields(strings.TrimSpace(string(output)))
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "refs/heads/") {
		cleanup()
		return "", "", nil, fmt.Errorf("unexpected bundle head: %q", output)
	}
	branch := strings.TrimPrefix(fields[1], "refs/heads/")
	repositoryPath := filepath.Join(root, "repo")
	if output, err := exec.Command("git", "init", "-q", repositoryPath).CombinedOutput(); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("init bundle checkout: %w: %s", err, output)
	}
	fetch := exec.Command("git", "-C", repositoryPath, "fetch", "-q", bundlePath, fields[1]+":"+fields[1])
	if output, err := fetch.CombinedOutput(); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("fetch bundle checkout: %w: %s", err, output)
	}
	checkout := exec.Command("git", "-C", repositoryPath, "checkout", "-q", branch)
	if output, err := checkout.CombinedOutput(); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("checkout bundle branch: %w: %s", err, output)
	}
	return repositoryPath, branch, cleanup, nil
}

func isolatedDelegationExecutionDatabase(t *testing.T, ctx context.Context, baseDatabaseURL string) string {
	t.Helper()
	parsed, err := url.Parse(baseDatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("agent_board_delegation_execution_%x", uint64(time.Now().UnixNano()))
	admin, err := pgx.Connect(ctx, baseDatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+databaseName); err != nil {
		_ = admin.Close(ctx)
		t.Fatal(err)
	}
	if err := admin.Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanup, connectErr := pgx.Connect(cleanupCtx, baseDatabaseURL)
		if connectErr != nil {
			t.Errorf("connect to drop delegation execution database: %v", connectErr)
			return
		}
		defer cleanup.Close(cleanupCtx)
		if _, dropErr := cleanup.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+databaseName+" WITH (FORCE)"); dropErr != nil {
			t.Errorf("drop delegation execution database: %v", dropErr)
		}
	})

	parsed.Path = "/" + databaseName
	isolatedURL := parsed.String()
	schemaConfig, err := pgx.ParseConfig(isolatedURL)
	if err != nil {
		t.Fatal(err)
	}
	schemaConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	schemaConn, err := pgx.ConnectConfig(ctx, schemaConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer schemaConn.Close(ctx)
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "database", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schemaConn.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply isolated delegation execution schema: %v", err)
	}
	return isolatedURL
}

func delegationExecutionJSON(t *testing.T, router http.Handler, method, path, body string, wantStatus int, target any) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.Code, wantStatus, response.Body.String())
	}
	if target != nil && response.Body.Len() != 0 {
		if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
			t.Fatalf("decode %s %s response: %v body=%s", method, path, err, response.Body.String())
		}
	}
}
