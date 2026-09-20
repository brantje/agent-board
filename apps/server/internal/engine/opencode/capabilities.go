package opencode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/brantje/agent-board/apps/server/internal/engine"
	"github.com/brantje/agent-board/apps/server/internal/engine/opencode/client"
	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
)

type attachedProcessResetter interface {
	ResetAttachedProcess()
}

func reconcileAttachedOpenCodeCapabilities(ctx context.Context, launcher engine.ProcessLauncher, process engine.Process, host, port string, env map[string]string, issueStatusEnabled, issueCommentEnabled, delegationEnabled bool) (engine.Process, bool, error) {
	return reconcileAttachedOpenCodeCapabilitiesWithDiscussion(ctx, launcher, process, host, port, env, issueStatusEnabled, issueCommentEnabled, false, delegationEnabled)
}

func reconcileAttachedOpenCodeCapabilitiesWithDiscussion(ctx context.Context, launcher engine.ProcessLauncher, process engine.Process, host, port string, env map[string]string, issueStatusEnabled, issueCommentEnabled, issueDiscussionEnabled, delegationEnabled bool) (engine.Process, bool, error) {
	connector, ok := process.(engine.SessionConnector)
	if !ok {
		return nil, false, fmt.Errorf("opencode engine: attached process does not support session-local connections")
	}
	address := net.JoinHostPort(host, port)
	native, err := client.NewSession(connector, address)
	if err != nil {
		return nil, false, err
	}
	native.SetDirectory(nativeWorkingDirectory(process))
	if err := waitHealthy(ctx, native); err != nil {
		native.CloseIdleConnections()
		return nil, false, err
	}
	ids, err := native.ToolIDs(ctx)
	native.CloseIdleConnections()
	if err != nil {
		var httpErr *client.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			// Older/reduced OpenCode servers may not expose tool discovery. They are
			// safe to reuse only when this execution needs neither Agent Board tool;
			// otherwise restart so the effective capability files are known-good.
			if !issueStatusEnabled && !issueCommentEnabled && !issueDiscussionEnabled && !delegationEnabled {
				return process, true, nil
			}
			ids = nil
		} else {
			return nil, false, fmt.Errorf("opencode engine: inspect attached tool capabilities: %w", err)
		}
	}
	if openCodeToolCapabilitiesMatchWithDiscussion(ids, issueStatusEnabled, issueCommentEnabled, issueDiscussionEnabled, delegationEnabled) {
		return process, true, nil
	}

	drained := discardProcessStreams(process)
	if err := stopService(ctx, process); err != nil {
		waitDrained(drained, serviceStopTimeout)
		return nil, false, fmt.Errorf("opencode engine: restart server for capability change: %w", err)
	}
	waitDrained(drained, serviceStopTimeout)
	if resetter, ok := launcher.(attachedProcessResetter); ok {
		resetter.ResetAttachedProcess()
	}
	fresh, err := startOpenCodeProcessWithDiscussion(ctx, launcher, host, port, env, issueStatusEnabled, issueCommentEnabled, issueDiscussionEnabled, delegationEnabled)
	if err != nil {
		return nil, false, err
	}
	return fresh, false, nil
}

func openCodeToolCapabilitiesMatch(ids []string, issueStatusEnabled, issueCommentEnabled, delegationEnabled bool) bool {
	available := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		available[id] = struct{}{}
	}
	_, hasStatus := available[issueStatusToolName]
	_, hasComment := available[issueCommentToolName]
	_, hasDelegation := available[delegationToolName]
	return hasStatus == issueStatusEnabled && hasComment == issueCommentEnabled && hasDelegation == delegationEnabled
}

func openCodeToolCapabilitiesMatchWithDiscussion(ids []string, issueStatusEnabled, issueCommentEnabled, issueDiscussionEnabled, delegationEnabled bool) bool {
	available := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		available[id] = struct{}{}
	}
	_, hasStatus := available[issueStatusToolName]
	_, hasComment := available[issueCommentToolName]
	_, hasDiscussion := available[issueDiscussionToolName]
	_, hasDelegation := available[delegationToolName]
	return hasStatus == issueStatusEnabled &&
		hasComment == issueCommentEnabled &&
		hasDiscussion == issueDiscussionEnabled &&
		hasDelegation == delegationEnabled
}

func discardProcessStreams(process engine.Process) <-chan struct{} {
	done := make(chan struct{})
	var wg sync.WaitGroup
	for _, source := range []io.Reader{process.Stdout(), process.Stderr()} {
		wg.Add(1)
		go func(source io.Reader) {
			defer wg.Done()
			_, _ = io.Copy(io.Discard, source)
		}(source)
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}

func startOpenCodeProcess(ctx context.Context, launcher engine.ProcessLauncher, host, port string, env map[string]string, issueStatusEnabled, issueCommentEnabled, delegationEnabled bool) (engine.Process, error) {
	process, err := launcher.Start(ctx, engine.ProcessRequest{
		Command:               openCodeServeCommand(host, port, issueStatusEnabled, issueCommentEnabled, delegationEnabled),
		CWD:                   runtimepkg.WorkspaceTarget,
		Env:                   env,
		ProviderCredentialEnv: providerCredentialEnv,
		Kind:                  "tool",
		Name:                  "opencode-server",
	})
	if err != nil {
		return nil, fmt.Errorf("opencode engine: start server: %w", err)
	}
	return process, nil
}

func startOpenCodeProcessWithDiscussion(ctx context.Context, launcher engine.ProcessLauncher, host, port string, env map[string]string, issueStatusEnabled, issueCommentEnabled, issueDiscussionEnabled, delegationEnabled bool) (engine.Process, error) {
	if !issueDiscussionEnabled {
		return startOpenCodeProcess(ctx, launcher, host, port, env, issueStatusEnabled, issueCommentEnabled, delegationEnabled)
	}
	process, err := launcher.Start(ctx, engine.ProcessRequest{
		Command:               openCodeServeCommandWithDiscussion(host, port, issueStatusEnabled, issueCommentEnabled, true, delegationEnabled),
		CWD:                   runtimepkg.WorkspaceTarget,
		Env:                   env,
		ProviderCredentialEnv: providerCredentialEnv,
		Kind:                  "tool",
		Name:                  "opencode-server",
	})
	if err != nil {
		return nil, fmt.Errorf("opencode engine: start server: %w", err)
	}
	return process, nil
}
