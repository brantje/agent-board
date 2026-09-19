package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

type capabilityAttachLauncher struct {
	attached engine.Process
	fresh    engine.Process
	request  engine.ProcessRequest
	starts   int
}

func (l *capabilityAttachLauncher) Attach(context.Context) (engine.Process, error) {
	return l.attached, nil
}

func (l *capabilityAttachLauncher) Start(_ context.Context, request engine.ProcessRequest) (engine.Process, error) {
	l.starts++
	l.request = request
	return l.fresh, nil
}

func TestRecoveredOpenCodeProcessIsReusedWhenCapabilitiesMatch(t *testing.T) {
	server, host := capabilityServer(t, []string{issueStatusToolName, issueCommentToolName, delegationToolName})
	defer server.Close()
	attached := newFakeOpenCodeProcess(host)
	fresh := newFakeOpenCodeProcess(host)
	launcher := &capabilityAttachLauncher{attached: attached, fresh: fresh}
	process, recovered, err := launchOpenCodeProcessWithCapabilities(t.Context(), launcher, "127.0.0.1", serverPort(t, server.URL), map[string]string{}, true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered || process != attached || launcher.starts != 0 {
		t.Fatalf("process=%T recovered=%v starts=%d", process, recovered, launcher.starts)
	}
	_ = attached.Terminate(t.Context())
}

func TestRecoveredOpenCodeProcessRestartsWhenCapabilitiesDiffer(t *testing.T) {
	server, host := capabilityServer(t, []string{delegationToolName})
	defer server.Close()
	attached := newFakeOpenCodeProcess(host)
	fresh := newFakeOpenCodeProcess(host)
	launcher := &capabilityAttachLauncher{attached: attached, fresh: fresh}
	process, recovered, err := launchOpenCodeProcessWithCapabilities(t.Context(), launcher, "127.0.0.1", serverPort(t, server.URL), map[string]string{}, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if recovered || process != fresh || launcher.starts != 1 {
		t.Fatalf("process=%T recovered=%v starts=%d", process, recovered, launcher.starts)
	}
	if len(launcher.request.Command) != 9 || launcher.request.Command[6] == "" || launcher.request.Command[7] != "" || launcher.request.Command[8] != "" {
		t.Fatalf("restart command=%v", launcher.request.Command)
	}
	select {
	case <-attached.done:
	default:
		t.Fatal("stale attached process was not stopped")
	}
	_ = fresh.Terminate(t.Context())
}

func TestRecoveredOpenCodeProcessRestartsWhenRequiredCapabilitiesCannotBeInspected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"healthy": true, "version": "test"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	attached := newFakeOpenCodeProcess(parsed.Host)
	fresh := newFakeOpenCodeProcess(parsed.Host)
	launcher := &capabilityAttachLauncher{attached: attached, fresh: fresh}
	process, recovered, err := launchOpenCodeProcessWithCapabilities(t.Context(), launcher, "127.0.0.1", parsed.Port(), map[string]string{}, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if recovered || process != fresh || launcher.starts != 1 {
		t.Fatalf("process=%T recovered=%v starts=%d", process, recovered, launcher.starts)
	}
	_ = fresh.Terminate(t.Context())
}

func TestRecoveredOpenCodeProcessReusesUnknownCapabilitiesWhenNoToolsRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"healthy": true, "version": "test"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	attached := newFakeOpenCodeProcess(parsed.Host)
	fresh := newFakeOpenCodeProcess(parsed.Host)
	launcher := &capabilityAttachLauncher{attached: attached, fresh: fresh}
	process, recovered, err := launchOpenCodeProcessWithCapabilities(t.Context(), launcher, "127.0.0.1", parsed.Port(), map[string]string{}, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered || process != attached || launcher.starts != 0 {
		t.Fatalf("process=%T recovered=%v starts=%d", process, recovered, launcher.starts)
	}
	_ = attached.Terminate(t.Context())
}

func TestServerEnvironmentRequiresAgentBoardDelegationPermissionApproval(t *testing.T) {
	env, err := serverEnvironment(executioncontext.SafeContext{
		Run:      executioncontext.RunContext{ID: "run-1"},
		Model:    executioncontext.ModelContext{Model: "model-1"},
		Provider: executioncontext.ProviderContext{Kind: "openrouter"},
	}, "openrouter")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Permission map[string]string `json:"permission"`
	}
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &config); err != nil {
		t.Fatal(err)
	}
	if config.Permission["*"] != "allow" || config.Permission[delegationPermissionName] != "ask" {
		t.Fatalf("permission config=%+v", config.Permission)
	}
}

func capabilityServer(t *testing.T, toolIDs []string) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"healthy": true, "version": "test"})
		case "/experimental/tool/ids":
			_ = json.NewEncoder(w).Encode(toolIDs)
		default:
			http.NotFound(w, r)
		}
	}))
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return server, parsed.Host
}

func serverPort(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Port()
}

var _ engine.ProcessLauncher = (*capabilityAttachLauncher)(nil)
var _ engine.ProcessAttacher = (*capabilityAttachLauncher)(nil)
