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
	actor json.RawMessage
	calls int
	fail  error
}

func (s *assigneeCommandStore) SetIssueAssignee(_ context.Context, _, _ string, target *store.Assignee, actor json.RawMessage) (store.Issue, store.Event, error) {
	s.calls++
	s.actor = actor
	return store.Issue{}, store.Event{ID: "event"}, s.fail
}
func (s *assigneeCommandStore) ListIssueAssignees(context.Context, string) ([]store.Assignee, error) {
	s.calls++
	return []store.Assignee{}, s.fail
}

type assigneePublisher struct{ published int }

func (*assigneePublisher) Record(context.Context, store.Event) (store.Event, error) {
	panic("already persisted")
}
func (p *assigneePublisher) PublishPersisted(context.Context, store.Event) { p.published++ }

func TestAssigneeSharedAuthorizationActorAndPublication(t *testing.T) {
	const pid = "00000000-0000-4000-8000-000000000001"
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
	if _, err = access.SetIssueAssignee(t.Context(), actor, pid, "issue", nil); err != nil {
		t.Fatal(err)
	}
	if string(fake.actor) != `{"id":"member","type":"HUMAN"}` || pub.published != 1 {
		t.Fatalf("actor=%s published=%d", fake.actor, pub.published)
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
	if _, err = svc.SetIssueAssignee(t.Context(), pid, "issue", &store.Assignee{Type: "USER", ID: pid}, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("mutation error=%v", err)
	}
	if pub.published != 1 {
		t.Fatal("published failed mutation")
	}
	if _, err = svc.ListIssueAssignees(t.Context(), pid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("directory error=%v", err)
	}
	unsupported := New(&fakeStore{})
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
