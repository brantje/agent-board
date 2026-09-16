package app

import (
	"context"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type squadStoreHidingDecorator struct {
	store.ControlPlaneStore
}

func TestSquadStoreCanBeBoundBehindControlPlaneDecorator(t *testing.T) {
	base := newSquadTestStore()
	decorated := &squadStoreHidingDecorator{ControlPlaneStore: base}
	svc := New(decorated)

	if _, err := svc.ListSquads(context.Background(), squadProjectID); !isAppCode(err, "squad_management_unavailable") {
		t.Fatalf("decorated store unexpectedly exposed Squad capability: %v", err)
	}

	svc.bindOptionalControlPlaneStores(base)
	created, err := svc.CreateSquad(context.Background(), store.Squad{
		ProjectID:     squadProjectID,
		Name:          "Backend",
		LeaderAgentID: squadLeaderID,
	})
	if err != nil {
		t.Fatalf("CreateSquad after base-store binding: %v", err)
	}
	if created.ID != squadID {
		t.Fatalf("created id=%q want %q", created.ID, squadID)
	}
}
