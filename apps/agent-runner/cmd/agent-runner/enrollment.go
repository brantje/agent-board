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
	"os"
	"path/filepath"
	"strings"

	runnerserver "github.com/brantje/agent-board/apps/agent-runner/internal/server"
)

const defaultConfigPath = "/etc/agent-board/agent-runner.env"

type enrollmentResult struct {
	ServerURL   string
	RunnerID    string
	RunnerToken string
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type enrollmentResponse struct {
	RunnerID    string `json:"runnerId"`
	RunnerToken string `json:"runnerToken"`
}

func enrollRunner(ctx context.Context, client httpDoer, serverURL, registrationToken, hostname string) (enrollmentResult, error) {
	normalizedURL, endpoint, err := runnerserver.AgentBoardEndpoint(serverURL, "/api/runner/register", false)
	if err != nil {
		return enrollmentResult{}, err
	}
	registrationToken = strings.TrimSpace(registrationToken)
	hostname = strings.TrimSpace(hostname)
	if registrationToken == "" {
		return enrollmentResult{}, errors.New("runner registration token is required")
	}
	if hostname == "" {
		return enrollmentResult{}, errors.New("runner hostname is required")
	}

	payload, err := json.Marshal(struct {
		RegistrationToken string `json:"registrationToken"`
		Hostname          string `json:"hostname"`
	}{RegistrationToken: registrationToken, Hostname: hostname})
	if err != nil {
		return enrollmentResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return enrollmentResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return enrollmentResult{}, fmt.Errorf("register runner: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = response.Status
		}
		return enrollmentResult{}, fmt.Errorf("register runner: %s", message)
	}
	var enrolled enrollmentResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&enrolled); err != nil {
		return enrollmentResult{}, fmt.Errorf("decode registration response: %w", err)
	}
	if enrolled.RunnerID == "" || enrolled.RunnerToken == "" {
		return enrollmentResult{}, errors.New("registration response did not include runner credentials")
	}
	return enrollmentResult{ServerURL: normalizedURL, RunnerID: enrolled.RunnerID, RunnerToken: enrolled.RunnerToken}, nil
}

func saveRunnerEnvironment(path string, enrolled enrollmentResult, workspaceRoot string) error {
	if enrolled.ServerURL == "" || enrolled.RunnerID == "" || enrolled.RunnerToken == "" {
		return errors.New("runner enrollment result is incomplete")
	}
	if workspaceRoot == "" {
		workspaceRoot = defaultWorkspaceRoot
	}
	values := []struct {
		name  string
		value string
	}{
		{"AGENT_BOARD_URL", enrolled.ServerURL},
		{"AGENT_RUNNER_ID", enrolled.RunnerID},
		{"AGENT_RUNNER_TOKEN", enrolled.RunnerToken},
		{"AGENT_RUNNER_WORKSPACE_ROOT", workspaceRoot},
	}
	var content strings.Builder
	for _, value := range values {
		line, err := environmentLine(value.name, value.value)
		if err != nil {
			return err
		}
		content.WriteString(line)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(content.String()), 0600); err != nil {
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

func environmentLine(name, value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("%s has an invalid value", name)
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return fmt.Sprintf("%s=\"%s\"\n", name, escaped), nil
}

func registerInteractive(ctx context.Context, client httpDoer, input io.Reader, output io.Writer, configPath string) error {
	reader := bufio.NewReader(input)
	fmt.Fprint(output, "Agent Board URL: ")
	serverURL, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	fmt.Fprint(output, "One-time registration token: ")
	registrationToken, err := readRegistrationToken(reader, input, output)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read hostname: %w", err)
	}
	enrolled, err := enrollRunner(ctx, client, serverURL, registrationToken, hostname)
	if err != nil {
		return err
	}
	if err := saveRunnerEnvironment(configPath, enrolled, os.Getenv("AGENT_RUNNER_WORKSPACE_ROOT")); err != nil {
		return fmt.Errorf("save runner environment: %w", err)
	}
	fmt.Fprintf(output, "Registered runner %s as %s.\n", enrolled.RunnerID, hostname)
	return nil
}
