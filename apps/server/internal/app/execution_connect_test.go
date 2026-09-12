package app

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type sessionDialExecutionClient struct {
	*fakeExecutionClient
	conn      net.Conn
	err       error
	sessionID string
	network   string
	address   string
}

func (c *sessionDialExecutionClient) DialSession(_ context.Context, sessionID, network, address string) (net.Conn, error) {
	c.sessionID = sessionID
	c.network = network
	c.address = address
	return c.conn, c.err
}

func TestExecutionSessionServiceDialsBoundRunnerSession(t *testing.T) {
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = local.Close(); _ = remote.Close() })

	storeFake := &executionSessionStoreFake{session: store.ExecutionSession{
		ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1", Status: "RUNNING",
	}}
	client := &sessionDialExecutionClient{
		fakeExecutionClient: &fakeExecutionClient{done: make(chan struct{})},
		conn:                local,
	}
	manager := &fakeExecutionManager{client: client}
	service, err := NewExecutionSessionService(storeFake, manager, manager)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := service.DialSession(context.Background(), "project-1", "session-1", "tcp", "127.0.0.1:4096")
	if err != nil {
		t.Fatalf("dial runner session: %v", err)
	}
	if conn != local || client.sessionID != "session-1" || client.network != "tcp" || client.address != "127.0.0.1:4096" {
		t.Fatalf("conn=%v session=%q network=%q address=%q", conn, client.sessionID, client.network, client.address)
	}
}

func TestExecutionSessionServiceRejectsInvalidSessionDialBindings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		session store.ExecutionSession
		client  runner.Client
	}{
		{
			name:    "missing runner",
			session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", Status: "RUNNING"},
			client:  &fakeExecutionClient{done: make(chan struct{})},
		},
		{
			name:    "not running",
			session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunnerID: "runner-1", Status: "COMPLETED"},
			client:  &fakeExecutionClient{done: make(chan struct{})},
		},
		{
			name:    "runner capability missing",
			session: store.ExecutionSession{ID: "session-1", ProjectID: "project-1", RunnerID: "runner-1", Status: "RUNNING"},
			client:  &fakeExecutionClient{done: make(chan struct{})},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storeFake := &executionSessionStoreFake{session: tc.session}
			service, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: tc.client}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.DialSession(context.Background(), "project-1", "session-1", "tcp", "127.0.0.1:4096")
			if err == nil {
				t.Fatal("expected dial rejection")
			}
			if tc.name == "runner capability missing" && !errors.Is(err, runner.ErrSessionConnectUnsupported) {
				t.Fatalf("err=%v want ErrSessionConnectUnsupported", err)
			}
		})
	}
}

func TestAuthorizedExecutionSessionServiceForwardsSessionDial(t *testing.T) {
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = local.Close(); _ = remote.Close() })
	storeFake := &executionSessionStoreFake{session: store.ExecutionSession{
		ID: "session-1", ProjectID: "project-1", RunID: "run-1", RunnerID: "runner-1", Status: "RUNNING",
	}}
	client := &sessionDialExecutionClient{
		fakeExecutionClient: &fakeExecutionClient{done: make(chan struct{})},
		conn:                local,
	}
	service, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: client}, nil)
	if err != nil {
		t.Fatal(err)
	}
	authorized := &AuthorizedExecutionSessionService{sessions: service}

	conn, err := authorized.DialSession(context.Background(), "project-1", "session-1", "tcp", "127.0.0.1:4096")
	if err != nil {
		t.Fatalf("authorized dial: %v", err)
	}
	if conn != local || client.sessionID != "session-1" || client.network != "tcp" || client.address != "127.0.0.1:4096" {
		t.Fatalf("conn=%v session=%q network=%q address=%q", conn, client.sessionID, client.network, client.address)
	}
}

func TestAuthorizedExecutionSessionServiceRejectsUnavailableService(t *testing.T) {
	var nilService *AuthorizedExecutionSessionService
	if _, err := nilService.DialSession(context.Background(), "project-1", "session-1", "tcp", "127.0.0.1:4096"); err == nil {
		t.Fatal("nil authorized service unexpectedly dialed")
	}
	if _, err := (&AuthorizedExecutionSessionService{}).DialSession(context.Background(), "project-1", "session-1", "tcp", "127.0.0.1:4096"); err == nil {
		t.Fatal("authorized service without sessions unexpectedly dialed")
	}
}

func TestExecutionSessionServiceRejectsMissingDialIdentity(t *testing.T) {
	var service *ExecutionSessionService
	if _, err := service.DialSession(context.Background(), "project-1", "session-1", "tcp", "127.0.0.1:4096"); err == nil {
		t.Fatal("nil execution session service unexpectedly dialed")
	}
	storeFake := &executionSessionStoreFake{}
	valid, err := NewExecutionSessionService(storeFake, &fakeExecutionManager{client: &fakeExecutionClient{done: make(chan struct{})}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][2]string{{"", "session-1"}, {"project-1", ""}} {
		if _, err := valid.DialSession(context.Background(), args[0], args[1], "tcp", "127.0.0.1:4096"); err == nil {
			t.Fatalf("missing identity %+v unexpectedly accepted", args)
		}
	}
}
