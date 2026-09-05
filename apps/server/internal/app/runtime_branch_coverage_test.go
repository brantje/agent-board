package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	runtimepkg "github.com/brantje/agent-board/apps/server/internal/runtime"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

type passthroughMaterializer struct{}

func (passthroughMaterializer) Ensure(_ context.Context, _ store.Project, _ store.Issue, workspace store.Workspace) (store.Workspace, error) {
	return workspace, nil
}

type closableRuntime struct {
	*fakeRuntimeImplementation
	closeErr   error
	closeCalls int
}

func (r *closableRuntime) Close() error {
	r.closeCalls++
	return r.closeErr
}

type inspectErrorRuntime struct {
	*fakeRuntimeImplementation
	err error
}

func (r *inspectErrorRuntime) Inspect(ctx context.Context, handle runtimepkg.Handle) (runtimepkg.Inspection, error) {
	if r.err != nil {
		return runtimepkg.Inspection{}, r.err
	}
	return r.fakeRuntimeImplementation.Inspect(ctx, handle)
}

type updateBehaviorStore struct {
	*runtimeServiceStore
	failStatus    string
	err           error
	mutateBinding bool
}

func (s *updateBehaviorStore) UpdateRuntimeInstanceState(ctx context.Context, projectID, instanceID, status string, externalID *string, runnerStatus string, metadata json.RawMessage) (store.RuntimeInstance, error) {
	if s.err != nil && (s.failStatus == "" || s.failStatus == status) {
		return store.RuntimeInstance{}, s.err
	}
	updated, err := s.runtimeServiceStore.UpdateRuntimeInstanceState(ctx, projectID, instanceID, status, externalID, runnerStatus, metadata)
	if err != nil {
		return store.RuntimeInstance{}, err
	}
	if s.mutateBinding {
		updated.WorkspaceID = "other-workspace"
	}
	return updated, nil
}

type listProjectsErrorStore struct {
	*reconcileStore
	err error
}

func (s *listProjectsErrorStore) ListProjects(context.Context) ([]store.Project, error) {
	return nil, s.err
}

type listInstancesErrorStore struct {
	*reconcileStore
	err error
}

func (s *listInstancesErrorStore) ListRuntimeInstances(context.Context, string, []string) ([]store.RuntimeInstance, error) {
	return nil, s.err
}

type mismatchedWorkspaceStore struct{ *reconcileStore }

func (s *mismatchedWorkspaceStore) GetWorkspace(context.Context, string, string) (store.Workspace, error) {
	workspace := s.workspace
	workspace.ProjectID = "other-project"
	return workspace, nil
}

func TestRuntimeInstanceServiceConstructorAndServiceBundleBranches(t *testing.T) {
	materializer := passthroughMaterializer{}
	if _, err := NewServices(nil, materializer); err == nil {
		t.Fatal("NewServices() unexpectedly accepted nil store")
	}
	if _, err := NewServices(&fakeStore{}, nil); err == nil {
		t.Fatal("NewServices() unexpectedly accepted nil materializer")
	}
	services, err := NewServices(&fakeStore{}, materializer)
	if err != nil {
		t.Fatalf("NewServices() error=%v", err)
	}
	if services.ControlPlane == nil || services.Workspaces == nil || services.RuntimeInstances != nil {
		t.Fatalf("NewServices()=%+v", services)
	}
	if err := services.Close(); err != nil {
		t.Fatalf("Services.Close() without runtimes error=%v", err)
	}
	var nilServices *Services
	if err := nilServices.Close(); err != nil {
		t.Fatalf("nil Services.Close() error=%v", err)
	}

	implementation := &closableRuntime{fakeRuntimeImplementation: &fakeRuntimeImplementation{}}
	services, err = NewServicesWithRuntimes(&fakeStore{}, materializer, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatalf("NewServicesWithRuntimes() error=%v", err)
	}
	if services.RuntimeInstances == nil {
		t.Fatal("RuntimeInstances was not wired")
	}
	if err := services.Close(); err != nil || implementation.closeCalls != 1 {
		t.Fatalf("Services.Close() calls=%d err=%v", implementation.closeCalls, err)
	}

	closeErr := errors.New("close failed")
	implementation = &closableRuntime{fakeRuntimeImplementation: &fakeRuntimeImplementation{}, closeErr: closeErr}
	services, err = NewServicesWithRuntimes(&fakeStore{}, materializer, map[string]runtimepkg.Implementation{"docker": implementation})
	if err != nil {
		t.Fatal(err)
	}
	if err := services.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Services.Close() error=%v", err)
	}

	workspace := &runtimeWorkspaceEnsurer{}
	baseStore := &runtimeServiceStore{}
	impl := &fakeRuntimeImplementation{}
	for name, call := range map[string]func() error{
		"nil store": func() error {
			_, err := NewRuntimeInstanceService(nil, workspace, map[string]runtimepkg.Implementation{"docker": impl})
			return err
		},
		"nil workspace": func() error {
			_, err := NewRuntimeInstanceService(baseStore, nil, map[string]runtimepkg.Implementation{"docker": impl})
			return err
		},
		"empty implementations": func() error {
			_, err := NewRuntimeInstanceService(baseStore, workspace, nil)
			return err
		},
		"blank kind": func() error {
			_, err := NewRuntimeInstanceService(baseStore, workspace, map[string]runtimepkg.Implementation{" ": impl})
			return err
		},
		"nil implementation": func() error {
			_, err := NewRuntimeInstanceService(baseStore, workspace, map[string]runtimepkg.Implementation{"docker": nil})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("expected constructor error")
			}
		})
	}

	if _, err := NewServicesWithRuntimes(&fakeStore{}, materializer, nil); err == nil {
		t.Fatal("NewServicesWithRuntimes() unexpectedly accepted empty implementations")
	}
}

func TestRuntimeInstanceServiceLifecycleFailureBranches(t *testing.T) {
	t.Run("running start is idempotent", func(t *testing.T) {
		service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
		instance, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
		if err != nil {
			t.Fatal(err)
		}
		runtimeStore.instance.Status = string(runtimepkg.StateRunning)
		instance, err = service.Start(context.Background(), workspace.ProjectID, instance.ID)
		if err != nil || instance.Status != string(runtimepkg.StateRunning) || implementation.startCalls != 0 {
			t.Fatalf("Start() instance=%+v calls=%d err=%v", instance, implementation.startCalls, err)
		}
	})

	t.Run("destroyed start is rejected", func(t *testing.T) {
		service, runtimeStore, _, workspace := runtimeServiceFixture(t)
		instance, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
		if err != nil {
			t.Fatal(err)
		}
		runtimeStore.instance.Status = string(runtimepkg.StateDestroyed)
		if _, err := service.Start(context.Background(), workspace.ProjectID, instance.ID); !errors.Is(err, runtimepkg.ErrInvalidTransition) {
			t.Fatalf("Start() error=%v", err)
		}
	})

	t.Run("start failure is persisted", func(t *testing.T) {
		service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
		instance, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
		if err != nil {
			t.Fatal(err)
		}
		implementation.startErr = errors.New("start failed")
		instance, err = service.Start(context.Background(), workspace.ProjectID, instance.ID)
		if err == nil || instance.Status != string(runtimepkg.StateFailed) || runtimeStore.instance.Status != string(runtimepkg.StateFailed) {
			t.Fatalf("Start() instance=%+v persisted=%+v err=%v", instance, runtimeStore.instance, err)
		}
	})

	t.Run("non-running start result is persisted as failed", func(t *testing.T) {
		service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
		instance, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
		if err != nil {
			t.Fatal(err)
		}
		implementation.inspectState = runtimepkg.StateStopped
		instance, err = service.Start(context.Background(), workspace.ProjectID, instance.ID)
		if err == nil || instance.Status != string(runtimepkg.StateFailed) {
			t.Fatalf("Start() instance=%+v err=%v", instance, err)
		}
	})

	t.Run("provisioning stop is rejected", func(t *testing.T) {
		service, runtimeStore, _, workspace := runtimeServiceFixture(t)
		instance, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Stop(context.Background(), workspace.ProjectID, instance.ID, runtimepkg.StopReasonRequested); !errors.Is(err, runtimepkg.ErrInvalidTransition) {
			t.Fatalf("Stop() error=%v", err)
		}
	})

	for _, state := range []runtimepkg.State{runtimepkg.StateStopped, runtimepkg.StateDestroyed} {
		t.Run("stop is idempotent from "+string(state), func(t *testing.T) {
			service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
			instance, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
			if err != nil {
				t.Fatal(err)
			}
			runtimeStore.instance.Status = string(state)
			instance, err = service.Stop(context.Background(), workspace.ProjectID, instance.ID, runtimepkg.StopReasonRequested)
			if err != nil || instance.Status != string(state) || implementation.stopCalls != 0 {
				t.Fatalf("Stop() instance=%+v calls=%d err=%v", instance, implementation.stopCalls, err)
			}
		})
	}

	t.Run("stop failure is persisted", func(t *testing.T) {
		service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
		instance, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
		if err != nil {
			t.Fatal(err)
		}
		instance, err = service.Start(context.Background(), workspace.ProjectID, instance.ID)
		if err != nil {
			t.Fatal(err)
		}
		implementation.stopErr = errors.New("stop failed")
		instance, err = service.Stop(context.Background(), workspace.ProjectID, instance.ID, runtimepkg.StopReasonRequested)
		if err == nil || instance.Status != string(runtimepkg.StateFailed) {
			t.Fatalf("Stop() instance=%+v err=%v", instance, err)
		}
	})

	t.Run("destroy failure is persisted", func(t *testing.T) {
		service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
		instance, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
		if err != nil {
			t.Fatal(err)
		}
		implementation.destroyErr = errors.New("destroy failed")
		instance, err = service.Destroy(context.Background(), workspace.ProjectID, instance.ID)
		if err == nil || instance.Status != string(runtimepkg.StateFailed) {
			t.Fatalf("Destroy() instance=%+v err=%v", instance, err)
		}
	})

	t.Run("destroy is idempotent", func(t *testing.T) {
		service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
		instance, err := service.Create(context.Background(), workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
		if err != nil {
			t.Fatal(err)
		}
		runtimeStore.instance.Status = string(runtimepkg.StateDestroyed)
		instance, err = service.Destroy(context.Background(), workspace.ProjectID, instance.ID)
		if err != nil || instance.Status != string(runtimepkg.StateDestroyed) || implementation.destroyCalls != 0 {
			t.Fatalf("Destroy() instance=%+v calls=%d err=%v", instance, implementation.destroyCalls, err)
		}
	})
}

func TestRuntimeInstanceServicePersistenceAndValidationGuards(t *testing.T) {
	service, runtimeStore, _, workspace := runtimeServiceFixture(t)
	ctx := context.Background()
	if _, err := service.getInstance(ctx, "", "instance"); err == nil {
		t.Fatal("getInstance() unexpectedly accepted blank project")
	}
	if _, err := service.getInstance(ctx, workspace.ProjectID, ""); err == nil {
		t.Fatal("getInstance() unexpectedly accepted blank instance")
	}

	runtimeStore.runtime.Kind = "unsupported"
	if _, err := service.Create(ctx, workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID); !errors.Is(err, runtimepkg.ErrUnsupportedPolicy) {
		t.Fatalf("Create() unsupported kind error=%v", err)
	}
	runtimeStore.runtime.Kind = "docker"
	if _, err := service.Create(ctx, workspace.ProjectID, workspace.IssueID, "missing-runtime"); err == nil {
		t.Fatal("Create() unexpectedly found missing Runtime")
	}

	service.workspaces = &runtimeWorkspaceEnsurer{err: errors.New("workspace failed")}
	if _, err := service.Create(ctx, workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID); err == nil || err.Error() != "workspace failed" {
		t.Fatalf("Create() workspace error=%v", err)
	}
	service.workspaces = &runtimeWorkspaceEnsurer{workspace: workspace}

	bindingStore := &updateBehaviorStore{runtimeServiceStore: runtimeStore, mutateBinding: true}
	service.store = bindingStore
	if _, err := service.Create(ctx, workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID); err == nil {
		t.Fatal("Create() unexpectedly accepted mutated immutable binding")
	}

	service, runtimeStore, _, workspace = runtimeServiceFixture(t)
	instance, err := service.Create(ctx, workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
	if err != nil {
		t.Fatal(err)
	}
	updateErr := errors.New("update failed")
	service.store = &updateBehaviorStore{runtimeServiceStore: runtimeStore, err: updateErr}
	if _, err := service.Start(ctx, workspace.ProjectID, instance.ID); !errors.Is(err, updateErr) {
		t.Fatalf("Start() update error=%v", err)
	}

	service, runtimeStore, implementation, workspace := runtimeServiceFixture(t)
	instance, err = service.Create(ctx, workspace.ProjectID, workspace.IssueID, runtimeStore.runtime.ID)
	if err != nil {
		t.Fatal(err)
	}
	implementation.startErr = errors.New("runtime start failed")
	service.store = &updateBehaviorStore{runtimeServiceStore: runtimeStore, failStatus: string(runtimepkg.StateFailed), err: updateErr}
	if _, err := service.Start(ctx, workspace.ProjectID, instance.ID); !errors.Is(err, implementation.startErr) || !errors.Is(err, updateErr) {
		t.Fatalf("Start() joined failure error=%v", err)
	}
}

func TestRuntimeInstanceReconcileFailureAndStateBranches(t *testing.T) {
	service, base, implementation, workspace := runtimeServiceFixture(t)
	if err := service.ReconcileAll(context.Background()); err == nil {
		t.Fatal("ReconcileAll() unexpectedly accepted non-reconciliation store")
	}
	if _, err := service.Reconcile(context.Background(), workspace.ProjectID, "instance"); err == nil {
		t.Fatal("Reconcile() unexpectedly accepted non-reconciliation store")
	}

	externalID := "container-1"
	base.instance = store.RuntimeInstance{ID: "instance-1", ProjectID: workspace.ProjectID, WorkspaceID: workspace.ID, RuntimeID: base.runtime.ID, Status: string(runtimepkg.StateDestroyed)}
	rs := &reconcileStore{runtimeServiceStore: base, workspace: workspace}
	service.store = rs
	instance, err := service.Reconcile(context.Background(), workspace.ProjectID, base.instance.ID)
	if err != nil || instance.Status != string(runtimepkg.StateDestroyed) {
		t.Fatalf("destroyed Reconcile() instance=%+v err=%v", instance, err)
	}

	base.instance = store.RuntimeInstance{ID: "instance-1", ProjectID: workspace.ProjectID, WorkspaceID: workspace.ID, RuntimeID: base.runtime.ID, Status: string(runtimepkg.StateProvisioning)}
	if _, err := service.Reconcile(context.Background(), workspace.ProjectID, base.instance.ID); !errors.Is(err, runtimepkg.ErrUnsupportedPolicy) {
		t.Fatalf("missing identity Reconcile() error=%v", err)
	}

	base.instance = store.RuntimeInstance{ID: "instance-1", ProjectID: workspace.ProjectID, WorkspaceID: workspace.ID, RuntimeID: base.runtime.ID, Status: string(runtimepkg.StateRunning), ExternalID: &externalID, SafeHandleMetadata: json.RawMessage(`{"safe":true}`)}
	inspectRuntime := &inspectErrorRuntime{fakeRuntimeImplementation: implementation, err: runtimepkg.ErrNotFound}
	service.implementations["docker"] = inspectRuntime
	instance, err = service.Reconcile(context.Background(), workspace.ProjectID, base.instance.ID)
	if err != nil || instance.Status != string(runtimepkg.StateFailed) || base.instance.RunnerStatus != "UNAVAILABLE" {
		t.Fatalf("not-found Reconcile() instance=%+v persisted=%+v err=%v", instance, base.instance, err)
	}

	base.instance = store.RuntimeInstance{ID: "instance-1", ProjectID: workspace.ProjectID, WorkspaceID: workspace.ID, RuntimeID: base.runtime.ID, Status: string(runtimepkg.StateRunning), ExternalID: &externalID, SafeHandleMetadata: json.RawMessage(`{"safe":true}`)}
	implementation = &fakeRuntimeImplementation{inspectState: runtimepkg.StateDestroyed}
	service.implementations["docker"] = implementation
	if _, err := service.Reconcile(context.Background(), workspace.ProjectID, base.instance.ID); err == nil {
		t.Fatal("Reconcile() unexpectedly accepted unsupported observed state")
	}

	implementation.inspectState = runtimepkg.StateRunning
	service.store = &mismatchedWorkspaceStore{reconcileStore: rs}
	if _, err := service.Reconcile(context.Background(), workspace.ProjectID, base.instance.ID); err == nil {
		t.Fatal("Reconcile() unexpectedly accepted workspace binding mismatch")
	}

	service.store = &listProjectsErrorStore{reconcileStore: rs, err: store.ErrNotFound}
	if err := service.ReconcileAll(context.Background()); err == nil {
		t.Fatal("ReconcileAll() unexpectedly ignored project-list error")
	}
	service.store = &listInstancesErrorStore{reconcileStore: rs, err: errors.New("list failed")}
	if err := service.ReconcileAll(context.Background()); err == nil {
		t.Fatal("ReconcileAll() unexpectedly ignored instance-list error")
	}

	service.store = rs
	service.implementations["docker"] = implementation
	for _, tc := range []struct {
		state        runtimepkg.State
		runnerStatus string
	}{
		{runtimepkg.StateStarting, "CONNECTING"},
		{runtimepkg.StateStopping, "DRAINING"},
		{runtimepkg.StateFailed, "UNAVAILABLE"},
		{runtimepkg.StateStopped, "UNAVAILABLE"},
	} {
		base.instance = store.RuntimeInstance{ID: "instance-1", ProjectID: workspace.ProjectID, WorkspaceID: workspace.ID, RuntimeID: base.runtime.ID, Status: string(runtimepkg.StateRunning)}
		updated, err := service.reconcileState(context.Background(), base.instance, tc.state, &externalID, json.RawMessage(`{"safe":true}`))
		if err != nil || updated.RunnerStatus != tc.runnerStatus {
			t.Fatalf("reconcileState(%s)=%+v err=%v", tc.state, updated, err)
		}
	}

	base.runtime.Kind = "missing"
	if _, _, err := service.resolveForReconciliation(context.Background(), workspace.ProjectID, base.runtime.ID); !errors.Is(err, runtimepkg.ErrUnsupportedPolicy) {
		t.Fatalf("resolveForReconciliation() error=%v", err)
	}
}
