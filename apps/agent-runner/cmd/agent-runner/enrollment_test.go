package main

import (
	"context"
	"encoding/json"
	"errors"
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
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
			return
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

func TestEnrollRunnerRejectsInvalidInputsAndResponses(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, serverURL, token, hostname string
	}{
		{"empty URL", "", "token", "host"},
		{"relative URL", "agent-board.local", "token", "host"},
		{"unsupported scheme", "ftp://agent-board.local", "token", "host"},
		{"empty token", "https://agent-board.local", "", "host"},
		{"empty hostname", "https://agent-board.local", "token", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := enrollRunner(ctx, http.DefaultClient, tc.serverURL, tc.token, tc.hostname); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"server rejection", http.StatusBadRequest, `{"error":{"code":"invalid_argument"}}`},
		{"server rejection without body", http.StatusUnauthorized, ""},
		{"malformed response", http.StatusCreated, `{`},
		{"missing runner id", http.StatusCreated, `{"runner":{},"token":"credential"}`},
		{"missing credential", http.StatusCreated, `{"runner":{"id":"runner-1"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			if _, err := enrollRunner(ctx, server.Client(), server.URL, "token", "host"); err == nil {
				t.Fatal("expected response error")
			}
		})
	}

	if _, err := enrollRunner(ctx, failingHTTPClient{}, "https://agent-board.local", "token", "host"); err == nil {
		t.Fatal("expected transport error")
	}
}

type failingHTTPClient struct{}

func (failingHTTPClient) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("network unavailable")
}

func TestRunnerStateRejectsIncompleteOrMalformedFiles(t *testing.T) {
	if err := saveRunnerState(filepath.Join(t.TempDir(), "runner.json"), runnerState{ServerURL: "https://agent-board.local"}); err == nil {
		t.Fatal("saved incomplete runner state")
	}

	for _, tc := range []struct {
		name, content string
	}{
		{"malformed", `{`},
		{"incomplete", `{"serverUrl":"https://agent-board.local"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runner.json")
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRunnerState(path); err == nil {
				t.Fatal("loaded invalid runner state")
			}
		})
	}
	if _, err := loadRunnerState(filepath.Join(t.TempDir(), "missing.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing state error=%v", err)
	}
}

func TestSaveRunnerStateFilesystemFailures(t *testing.T) {
	state := runnerState{ServerURL: "https://agent-board.local", RunnerID: "runner-1", Token: "credential"}

	t.Run("parent is a file", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "state")
		if err := os.WriteFile(parent, []byte("not a directory"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := saveRunnerState(filepath.Join(parent, "runner.json"), state); err == nil {
			t.Fatal("expected parent creation failure")
		}
	})

	t.Run("temporary path is a directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "runner.json")
		if err := os.Mkdir(path+".tmp", 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveRunnerState(path, state); err == nil {
			t.Fatal("expected temporary write failure")
		}
	})

	t.Run("destination is a directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "runner.json")
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveRunnerState(path, state); err == nil {
			t.Fatal("expected rename failure")
		}
		if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary credential file was not removed: %v", err)
		}
	})
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
			t.Errorf("decode request: %v", err)
			return
		}
		if request.Token != "one-time-token" || request.Name != hostname {
			t.Errorf("request=%#v hostname=%q", request, hostname)
			return
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

func TestRegisterInteractiveReturnsEnrollmentFailureWithoutState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runner.json")
	var output strings.Builder
	err := registerInteractive(context.Background(), http.DefaultClient, strings.NewReader("not-a-url\ntoken\n"), &output, path)
	if err == nil {
		t.Fatal("expected enrollment failure")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state unexpectedly written: %v", statErr)
	}
}

func TestRegisterInteractivePropagatesInputAndStateFailures(t *testing.T) {
	var output strings.Builder
	if err := registerInteractive(context.Background(), http.DefaultClient, failingReader{}, &output, filepath.Join(t.TempDir(), "runner.json")); err == nil {
		t.Fatal("expected URL input failure")
	}

	input := &secondReadFailReader{first: "https://agent-board.local\n"}
	if err := registerInteractive(context.Background(), http.DefaultClient, input, &output, filepath.Join(t.TempDir(), "runner.json")); err == nil {
		t.Fatal("expected token input failure")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"runner":{"id":"runner-3"},"token":"credential-3"}`))
	}))
	defer server.Close()
	parent := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(parent, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := registerInteractive(context.Background(), server.Client(), strings.NewReader(server.URL+"\ntoken\n"), &output, filepath.Join(parent, "runner.json")); err == nil || !strings.Contains(err.Error(), "save runner credentials") {
		t.Fatalf("state persistence error=%v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

type secondReadFailReader struct {
	first string
	done  bool
}

func (r *secondReadFailReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, errors.New("read failed")
	}
	r.done = true
	return copy(p, r.first), nil
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
	if _, err := resolveConfig(appConfig{}, filepath.Join(t.TempDir(), "missing")); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("missing state error=%v", err)
	}
}
