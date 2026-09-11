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

func TestEnrollRunnerUsesExplicitContractAndNormalizesURL(t *testing.T) {
	var received struct {
		RegistrationToken string `json:"registrationToken"`
		Hostname          string `json:"hostname"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/runner/register" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"runnerId":"runner-1","runnerToken":"runner-credential"}`))
	}))
	defer server.Close()

	result, err := enrollRunner(context.Background(), server.Client(), server.URL+"/", " registration-token ", " host-1 ")
	if err != nil {
		t.Fatal(err)
	}
	if received.RegistrationToken != "registration-token" || received.Hostname != "host-1" {
		t.Fatalf("unexpected enrollment payload %#v", received)
	}
	if result.ServerURL != server.URL || result.RunnerID != "runner-1" || result.RunnerToken != "runner-credential" {
		t.Fatalf("unexpected result %#v", result)
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
		{"userinfo", "https://user@agent-board.local", "token", "host"},
		{"query", "https://agent-board.local?credential=bad", "token", "host"},
		{"fragment", "https://agent-board.local/#fragment", "token", "host"},
		{"path", "https://agent-board.local/base", "token", "host"},
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
		{"missing runner id", http.StatusCreated, `{"runnerToken":"credential"}`},
		{"missing credential", http.StatusCreated, `{"runnerId":"runner-1"}`},
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

func TestSaveRunnerEnvironmentPersistsOnlyPermanentConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-board", "agent-runner.env")
	result := enrollmentResult{ServerURL: "https://agent-board.local", RunnerID: "runner-1", RunnerToken: "runner-credential"}
	if err := saveRunnerEnvironment(path, result, "/work spaces"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("environment mode=%o", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, expected := range []string{
		`AGENT_BOARD_URL="https://agent-board.local"`,
		`AGENT_RUNNER_ID="runner-1"`,
		`AGENT_RUNNER_TOKEN="runner-credential"`,
		`AGENT_RUNNER_WORKSPACE_ROOT="/work spaces"`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("missing %q from %q", expected, content)
		}
	}
	if strings.Contains(content, "registration") {
		t.Fatalf("registration secret field persisted: %q", content)
	}
	if err := saveRunnerEnvironment(filepath.Join(t.TempDir(), "invalid.env"), enrollmentResult{}, ""); err == nil {
		t.Fatal("saved incomplete enrollment result")
	}
	if _, err := environmentLine("AGENT_BOARD_URL", "bad\nvalue"); err == nil {
		t.Fatal("environment newline accepted")
	}
}

func TestSaveRunnerEnvironmentFilesystemFailures(t *testing.T) {
	result := enrollmentResult{ServerURL: "https://agent-board.local", RunnerID: "runner-1", RunnerToken: "credential"}

	t.Run("parent is a file", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "state")
		if err := os.WriteFile(parent, []byte("not a directory"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := saveRunnerEnvironment(filepath.Join(parent, "agent-runner.env"), result, ""); err == nil {
			t.Fatal("expected parent creation failure")
		}
	})

	t.Run("temporary path is a directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "agent-runner.env")
		if err := os.Mkdir(path+".tmp", 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveRunnerEnvironment(path, result, ""); err == nil {
			t.Fatal("expected temporary write failure")
		}
	})

	t.Run("destination is a directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "agent-runner.env")
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveRunnerEnvironment(path, result, ""); err == nil {
			t.Fatal("expected rename failure")
		}
		if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary credential file was not removed: %v", err)
		}
	})
}

func TestRegisterInteractiveUsesSystemHostnameAndPersistsEnvironment(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			RegistrationToken string `json:"registrationToken"`
			Hostname          string `json:"hostname"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if request.RegistrationToken != "one-time-token" || request.Hostname != hostname {
			t.Errorf("request=%#v hostname=%q", request, hostname)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"runnerId":"runner-2","runnerToken":"credential-2"}`))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "agent-runner.env")
	var output strings.Builder
	input := strings.NewReader(server.URL + "\none-time-token\n")
	if err := registerInteractive(context.Background(), server.Client(), input, &output, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `AGENT_RUNNER_ID="runner-2"`) || !strings.Contains(content, `AGENT_RUNNER_TOKEN="credential-2"`) || strings.Contains(content, "one-time-token") {
		t.Fatalf("unexpected persisted environment %q", content)
	}
	if !strings.Contains(output.String(), hostname) || strings.Contains(output.String(), "one-time-token") || strings.Contains(output.String(), "credential-2") {
		t.Fatalf("unsafe registration output: %q", output.String())
	}
}

func TestRegisterInteractiveReturnsEnrollmentFailureWithoutConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-runner.env")
	var output strings.Builder
	err := registerInteractive(context.Background(), http.DefaultClient, strings.NewReader("not-a-url\ntoken\n"), &output, path)
	if err == nil {
		t.Fatal("expected enrollment failure")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("configuration unexpectedly written: %v", statErr)
	}
}

func TestRegisterInteractivePropagatesInputAndConfigurationFailures(t *testing.T) {
	var output strings.Builder
	if err := registerInteractive(context.Background(), http.DefaultClient, failingReader{}, &output, filepath.Join(t.TempDir(), "agent-runner.env")); err == nil {
		t.Fatal("expected URL input failure")
	}

	input := &secondReadFailReader{first: "https://agent-board.local\n"}
	if err := registerInteractive(context.Background(), http.DefaultClient, input, &output, filepath.Join(t.TempDir(), "agent-runner.env")); err == nil {
		t.Fatal("expected token input failure")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"runnerId":"runner-3","runnerToken":"credential-3"}`))
	}))
	defer server.Close()
	parent := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(parent, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := registerInteractive(context.Background(), server.Client(), strings.NewReader(server.URL+"\ntoken\n"), &output, filepath.Join(parent, "agent-runner.env")); err == nil || !strings.Contains(err.Error(), "save runner environment") {
		t.Fatalf("configuration persistence error=%v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

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
