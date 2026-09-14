package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type assigneeCommandStore struct {
	*projectWorkflowAuthorizationStore
	actor   json.RawMessage
	targets []*store.Assignee
	calls   int
	fail    error
}

func (s *assigneeCommandStore) SetIssueAssignee(_ context.Context, _, _ string, target *store.Assignee, actor json.RawMessage) (store.IssueMutationResult, error) {
	s.calls++
	s.actor = actor
	if target == nil {
		s.targets = append(s.targets, nil)
	} else {
		copyTarget := *target
		s.targets = append(s.targets, &copyTarget)
	}
	if s.fail != nil {
		return store.IssueMutationResult{}, s.fail
	}
	events := []store.Event{{ID: "assignment", Type: "issue.assigned"}}
	if target != nil && target.Type == "AGENT" {
		events = append(events, store.Event{ID: "run", Type: "run.created"})
	}
	return store.IssueMutationResult{Issue: store.Issue{ID: "issue"}, Events: events}, nil
}
func (s *assigneeCommandStore) ListIssueAssignees(context.Context, string) ([]store.Assignee, error) {
	s.calls++
	return []store.Assignee{}, s.fail
}

type assigneePublisher struct{ published []store.Event }

func (*assigneePublisher) Record(context.Context, store.Event) (store.Event, error) {
	panic("already persisted")
}
func (p *assigneePublisher) PublishPersisted(_ context.Context, event store.Event) {
	p.published = append(p.published, event)
}

func TestAssigneeSharedAuthorizationActorAndPublication(t *testing.T) {
	const (
		pid     = "00000000-0000-4000-8000-000000000001"
		agentID = "00000000-0000-4000-8000-000000000002"
		userID  = "00000000-0000-4000-8000-000000000003"
	)
	fake := &assigneeCommandStore{projectWorkflowAuthorizationStore: &projectWorkflowAuthorizationStore{project: store.Project{ID: pid}, roles: map[string]string{"member": "member", "viewer": "viewer"}}}
	svc := New(fake)
	pub := &assigneePublisher{}
	svc.SetEventRecorder(pub)
	access, err := NewProjectAccessService(svc, fake)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"outside", "viewer"} {
		if _, err = access.SetIssueAssignee(t.Context(), activeProjectActor(name, "member"), pid, "issue", nil); err == nil {
			t.Fatalf("%s assigned", name)
		}
	}
	if fake.calls != 0 {
		t.Fatal("unauthorized mutation reached store")
	}

	actor := activeProjectActor("member", "member")
	operations := []*store.Assignee{
		{Type: "AGENT", ID: agentID},
		{Type: "USER", ID: userID},
		nil,
	}
	for _, target := range operations {
		if _, err = access.SetIssueAssignee(t.Context(), actor, pid, "issue", target); err != nil {
			t.Fatal(err)
		}
		if string(fake.actor) != `{"id":"member","type":"HUMAN"}` {
			t.Fatalf("actor=%s", fake.actor)
		}
	}
	if len(fake.targets) != 3 || fake.targets[0] == nil || fake.targets[0].Type != "AGENT" || fake.targets[1] == nil || fake.targets[1].Type != "USER" || fake.targets[2] != nil {
		t.Fatalf("generic targets=%#v", fake.targets)
	}
	if len(pub.published) != 4 {
		t.Fatalf("published=%#v", pub.published)
	}
	if pub.published[0].Type != "issue.assigned" || pub.published[1].Type != "run.created" || pub.published[2].Type != "issue.assigned" || pub.published[3].Type != "issue.assigned" {
		t.Fatalf("published types=%#v", pub.published)
	}

	if _, err = access.ListIssueAssignees(t.Context(), activeProjectActor("viewer", "member"), pid); err != nil {
		t.Fatal(err)
	}
	if _, err = access.ListIssueAssignees(t.Context(), activeProjectActor("outside", "member"), pid); err == nil {
		t.Fatal("directory leaked")
	}
	for _, target := range []*store.Assignee{{Type: "GROUP", ID: pid}, {Type: "USER", ID: "bad"}} {
		if _, err = svc.SetIssueAssignee(t.Context(), pid, "issue", target, nil); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	fake.fail = store.ErrNotFound
	before := len(pub.published)
	if _, err = svc.SetIssueAssignee(t.Context(), pid, "issue", &store.Assignee{Type: "USER", ID: pid}, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("mutation error=%v", err)
	}
	if len(pub.published) != before {
		t.Fatal("published failed mutation")
	}
	if _, err = svc.ListIssueAssignees(t.Context(), pid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("directory error=%v", err)
	}
	unsupported := New(&missingProjectStore{})
	if _, err = unsupported.SetIssueAssignee(t.Context(), pid, "issue", nil, nil); err == nil {
		t.Fatal("missing store accepted")
	}
	if _, err = unsupported.ListIssueAssignees(t.Context(), pid); err == nil {
		t.Fatal("missing directory accepted")
	}
}

func TestProductionServicesPreserveAssignmentCapability(t *testing.T) {
	fake := &assigneeCommandStore{projectWorkflowAuthorizationStore: &projectWorkflowAuthorizationStore{}}
	services, err := NewServicesWithRuntimes(fake, workspaceMaterializerFunc(func(_ context.Context, _ store.Project, _ store.Issue, w store.Workspace) (store.Workspace, error) {
		return w, nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = services.Close() })
	if _, err = services.ControlPlane.SetIssueAssignee(t.Context(), "project", "issue", nil, store.EmptyObject); err != nil {
		t.Fatal(err)
	}
	if _, err = services.ControlPlane.ListIssueAssignees(t.Context(), "project"); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 2 {
		t.Fatalf("calls=%d", fake.calls)
	}
}
