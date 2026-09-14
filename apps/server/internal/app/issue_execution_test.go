package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type executionCommandStore struct {
	*projectWorkflowAuthorizationStore
	filters []store.IssueExecutionFilter
	calls   int
	err     error
}

func (s *executionCommandStore) StartIssueRun(context.Context, string, string) (store.Run, store.Event, error) {
	s.calls++
	return store.Run{ID: "run", Status: "QUEUED"}, store.Event{ID: "event", Type: "run.created"}, s.err
}
func (s *executionCommandStore) ReconcileIssueExecution(_ context.Context, f store.IssueExecutionFilter) ([]store.Event, error) {
	s.filters = append(s.filters, f)
	return []store.Event{{ID: "recovered", Type: "run.created"}}, s.err
}
func TestStartIssueRunAuthorizationAndPublication(t *testing.T) {
	f := &executionCommandStore{projectWorkflowAuthorizationStore: &projectWorkflowAuthorizationStore{project: store.Project{ID: "project"}, roles: map[string]string{"member": "member", "viewer": "viewer"}}}
	svc := New(f)
	pub := &assigneePublisher{}
	svc.SetEventRecorder(pub)
	access, err := NewProjectAccessService(svc, f)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"outside", "viewer"} {
		if _, err = access.StartIssueRun(t.Context(), activeProjectActor(name, "member"), "project", "issue"); err == nil {
			t.Fatal("unauthorized")
		}
	}
	if f.calls != 0 {
		t.Fatal("unauthorized store call")
	}
	run, err := access.StartIssueRun(t.Context(), activeProjectActor("member", "member"), "project", "issue")
	if err != nil || run.Status != "QUEUED" || len(pub.published) != 1 {
		t.Fatalf("%+v %v", run, err)
	}
	f.err = store.ErrConflict
	if _, err = svc.StartIssueRun(t.Context(), "project", "issue"); !errors.Is(err, store.ErrConflict) {
		t.Fatal(err)
	}
	if len(pub.published) != 1 {
		t.Fatal("published rejection")
	}
	if err = svc.ReconcileIssueExecution(t.Context(), store.IssueExecutionFilter{}); !errors.Is(err, store.ErrConflict) {
		t.Fatal(err)
	}
	if len(pub.published) != 2 {
		t.Fatal("must publish committed partial scan results")
	}
	if _, err = New(&fakeStore{}).StartIssueRun(t.Context(), "project", "issue"); err == nil {
		t.Fatal("unsupported")
	}
}

type recoveryHealthStore struct {
	providerHealthStore
	filters []store.IssueExecutionFilter
	failure error
}

func (s *recoveryHealthStore) StartIssueRun(context.Context, string, string) (store.Run, store.Event, error) {
	return store.Run{}, store.Event{}, nil
}
func (s *recoveryHealthStore) ReconcileIssueExecution(_ context.Context, f store.IssueExecutionFilter) ([]store.Event, error) {
	s.filters = append(s.filters, f)
	return nil, s.failure
}
func (s *recoveryHealthStore) RunnableIssueExecutionScopes(_ context.Context, f store.IssueExecutionFilter) ([]store.IssueExecutionScope, error) {
	if f.ProviderID != testProviderID || !s.provider.Enabled || s.provider.HealthStatus == "UNHEALTHY" {
		return nil, nil
	}
	return []store.IssueExecutionScope{{ProjectID: "project", AgentID: "agent"}}, nil
}
func TestProviderHealthRecoveryTriggersOnlyOnTransition(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"data":[{"id":"model"}]}`)) }))
	defer upstream.Close()
	f := &recoveryHealthStore{providerHealthStore: providerHealthStore{provider: store.Provider{ID: testProviderID, Kind: "openai-compatible", BaseURL: &upstream.URL, Enabled: true, HealthStatus: "UNHEALTHY"}}}
	svc := New(f)
	for range 2 {
		if _, err := svc.ListProviderModels(t.Context(), nil, testProviderID, nil, upstream.Client()); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.filters) != 1 || f.filters[0].ProjectID != "project" || f.filters[0].AgentID != "agent" {
		t.Fatalf("scans=%+v", f.filters)
	}
	f.provider.HealthStatus = "UNHEALTHY"
	f.failure = errors.New("recovery failed")
	if _, err := svc.ListProviderModels(t.Context(), nil, testProviderID, nil, upstream.Client()); err != nil {
		t.Fatal("health persistence must not be undone", err)
	}
	if len(f.filters) != 2 {
		t.Fatal("missed recovery")
	}
}
