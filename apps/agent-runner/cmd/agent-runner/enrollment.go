package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const defaultStatePath = "/var/lib/agent-runner/runner.json"

type runnerState struct {
	ServerURL string `json:"serverUrl"`
	RunnerID  string `json:"runnerId"`
	Token     string `json:"token"`
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type enrollmentResponse struct {
	Runner struct {
		ID string `json:"id"`
	} `json:"runner"`
	Token string `json:"token"`
}

func enrollRunner(ctx context.Context, client httpDoer, serverURL, registrationToken, name string) (runnerState, error) {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	registrationToken = strings.TrimSpace(registrationToken)
	name = strings.TrimSpace(name)
	parsed, err := url.Parse(serverURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return runnerState{}, errors.New("Agent Board URL must be an absolute http or https URL")
	}
	if registrationToken == "" {
		return runnerState{}, errors.New("runner registration token is required")
	}
	if name == "" {
		return runnerState{}, errors.New("runner hostname is required")
	}

	payload, err := json.Marshal(map[string]string{"token": registrationToken, "name": name})
	if err != nil {
		return runnerState{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/runner/register", bytes.NewReader(payload))
	if err != nil {
		return runnerState{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return runnerState{}, fmt.Errorf("register runner: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = response.Status
		}
		return runnerState{}, fmt.Errorf("register runner: %s", message)
	}
	var enrolled enrollmentResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&enrolled); err != nil {
		return runnerState{}, fmt.Errorf("decode registration response: %w", err)
	}
	if enrolled.Runner.ID == "" || enrolled.Token == "" {
		return runnerState{}, errors.New("registration response did not include runner credentials")
	}
	return runnerState{ServerURL: serverURL, RunnerID: enrolled.Runner.ID, Token: enrolled.Token}, nil
}

func saveRunnerState(path string, state runnerState) error {
	if state.ServerURL == "" || state.RunnerID == "" || state.Token == "" {
		return errors.New("runner state is incomplete")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0600); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func loadRunnerState(path string) (runnerState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return runnerState{}, err
	}
	var state runnerState
	if err := json.Unmarshal(data, &state); err != nil {
		return runnerState{}, err
	}
	if state.ServerURL == "" || state.RunnerID == "" || state.Token == "" {
		return runnerState{}, errors.New("runner state is incomplete")
	}
	return state, nil
}

func registerInteractive(ctx context.Context, client httpDoer, input io.Reader, output io.Writer, statePath string) error {
	reader := bufio.NewReader(input)
	fmt.Fprint(output, "Agent Board URL: ")
	serverURL, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	fmt.Fprint(output, "One-time registration token: ")
	registrationToken, tokenErr := reader.ReadString('\n')
	if tokenErr != nil && !errors.Is(tokenErr, io.EOF) {
		return tokenErr
	}
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read hostname: %w", err)
	}
	state, err := enrollRunner(ctx, client, serverURL, registrationToken, hostname)
	if err != nil {
		return err
	}
	if err := saveRunnerState(statePath, state); err != nil {
		return fmt.Errorf("save runner credentials: %w", err)
	}
	fmt.Fprintf(output, "Registered runner %s as %s.\n", state.RunnerID, hostname)
	return nil
}
