package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnrollRunnerAndPersistState(t *testing.T) {
	var received struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/runner/register" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"runner":{"id":"runner-1"},"token":"runner-credential"}`))
	}))
	defer server.Close()

	state, err := enrollRunner(context.Background(), server.Client(), server.URL+"/", " registration-token ", " host-1 ")
	if err != nil {
		t.Fatal(err)
	}
	if received.Token != "registration-token" || received.Name != "host-1" {
		t.Fatalf("unexpected enrollment payload %#v", received)
	}
	if state.ServerURL != server.URL || state.RunnerID != "runner-1" || state.Token != "runner-credential" {
		t.Fatalf("unexpected state %#v", state)
	}

	path := filepath.Join(t.TempDir(), "state", "runner.json")
	if err := saveRunnerState(path, state); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("state mode=%o", info.Mode().Perm())
	}
	loaded, err := loadRunnerState(path)
	if err != nil || loaded != state {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}

func TestRegisterInteractiveUsesSystemHostname(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Token string `json:"token"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Token != "one-time-token" || request.Name != hostname {
			t.Fatalf("request=%#v hostname=%q", request, hostname)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"runner":{"id":"runner-2"},"token":"credential-2"}`))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "runner.json")
	var output strings.Builder
	input := strings.NewReader(server.URL + "\none-time-token\n")
	if err := registerInteractive(context.Background(), server.Client(), input, &output, path); err != nil {
		t.Fatal(err)
	}
	state, err := loadRunnerState(path)
	if err != nil || state.RunnerID != "runner-2" || state.Token != "credential-2" {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	if !strings.Contains(output.String(), hostname) {
		t.Fatalf("output does not mention hostname: %q", output.String())
	}
}

func TestResolveConfigPrefersExplicitCredentialsThenState(t *testing.T) {
	explicit := appConfig{ServerURL: "https://env.example", RunnerID: "env-id", Token: "env-token", WorkspaceRoot: "/work"}
	resolved, err := resolveConfig(explicit, filepath.Join(t.TempDir(), "missing"))
	if err != nil || resolved != explicit {
		t.Fatalf("explicit=%#v err=%v", resolved, err)
	}

	path := filepath.Join(t.TempDir(), "runner.json")
	state := runnerState{ServerURL: "https://state.example", RunnerID: "state-id", Token: "state-token"}
	if err := saveRunnerState(path, state); err != nil {
		t.Fatal(err)
	}
	resolved, err = resolveConfig(appConfig{WorkspaceRoot: "/workspace"}, path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ServerURL != state.ServerURL || resolved.RunnerID != state.RunnerID || resolved.Token != state.Token || resolved.WorkspaceRoot != "/workspace" {
		t.Fatalf("resolved=%#v", resolved)
	}
}
