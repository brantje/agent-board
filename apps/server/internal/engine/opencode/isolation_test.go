package opencode

import (
	"path"
	"strings"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestOpenCodeProcessIsolationEnvironmentIsStableAndRunScoped(t *testing.T) {
	first := openCodeProcessIsolationEnvironment(executioncontext.SafeContext{Run: executioncontext.RunContext{ID: "run-a"}})
	again := openCodeProcessIsolationEnvironment(executioncontext.SafeContext{Run: executioncontext.RunContext{ID: "run-a"}})
	second := openCodeProcessIsolationEnvironment(executioncontext.SafeContext{Run: executioncontext.RunContext{ID: "run-b"}})

	if first["OPENCODE_DB"] != ":memory:" {
		t.Fatalf("OPENCODE_DB=%q want :memory:", first["OPENCODE_DB"])
	}
	for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		if first[key] == "" {
			t.Fatalf("%s is empty", key)
		}
		if first[key] != again[key] {
			t.Fatalf("%s changed for the same Run: %q != %q", key, first[key], again[key])
		}
		if first[key] == second[key] {
			t.Fatalf("%s is shared by different Runs: %q", key, first[key])
		}
		if !strings.HasPrefix(first[key], openCodeIsolationRoot+"/") {
			t.Fatalf("%s=%q is outside %s", key, first[key], openCodeIsolationRoot)
		}
	}

	root := path.Dir(first["XDG_DATA_HOME"])
	if path.Dir(first["XDG_CONFIG_HOME"]) != root || path.Dir(first["XDG_CACHE_HOME"]) != root || path.Dir(first["XDG_STATE_HOME"]) != root {
		t.Fatalf("XDG roots do not share one Run namespace: %+v", first)
	}
}

func TestOpenCodeProcessIsolationEnvironmentDoesNotExposeRunIDAsPath(t *testing.T) {
	env := openCodeProcessIsolationEnvironment(executioncontext.SafeContext{Run: executioncontext.RunContext{ID: "../unsafe/run"}})
	for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		if strings.Contains(env[key], "unsafe") || strings.Contains(env[key], "..") {
			t.Fatalf("%s exposes unsanitized Run ID: %q", key, env[key])
		}
	}
}

func TestServerEnvironmentIncludesRunScopedOpenCodeIsolation(t *testing.T) {
	safe := executioncontext.SafeContext{
		Run:      executioncontext.RunContext{ID: "run-isolated"},
		Model:    executioncontext.ModelContext{Model: "deepseek/deepseek-v4-flash"},
		Provider: executioncontext.ProviderContext{Kind: "openrouter"},
	}
	env, err := serverEnvironment(safe, "openrouter")
	if err != nil {
		t.Fatal(err)
	}
	if env["OPENCODE_CONFIG_CONTENT"] == "" {
		t.Fatal("missing OPENCODE_CONFIG_CONTENT")
	}
	if env["OPENCODE_DB"] != ":memory:" || env["XDG_STATE_HOME"] == "" {
		t.Fatalf("server environment did not include OpenCode isolation: %+v", env)
	}
}
