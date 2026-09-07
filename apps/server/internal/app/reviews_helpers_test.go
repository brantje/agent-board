package app

import (
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type reviewCapabilityControlPlane struct {
	store.ControlPlaneStore
	store.ReviewStore
	enabled bool
}

func (s *reviewCapabilityControlPlane) SupportsReviewStore() bool { return s.enabled }

func TestNewReviewServiceRequiresEveryDependency(t *testing.T) {
	validStore := &reviewServiceStore{}
	validEvidence := &RunEvidenceService{}
	validApplier := &reviewCandidateApplierFake{}
	withoutCandidates := &reviewCapabilityControlPlane{enabled: true}
	cases := []struct {
		name     string
		reviews  store.ReviewStore
		projects reviewProjectStore
		evidence *RunEvidenceService
		applier  reviewCandidateApplier
	}{
		{name: "review store", projects: validStore, evidence: validEvidence, applier: validApplier},
		{name: "project store", reviews: validStore, evidence: validEvidence, applier: validApplier},
		{name: "evidence", reviews: validStore, projects: validStore, applier: validApplier},
		{name: "applier", reviews: validStore, projects: validStore, evidence: validEvidence},
		{name: "candidate reader", reviews: withoutCandidates, projects: validStore, evidence: validEvidence, applier: validApplier},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewReviewService(tc.reviews, tc.projects, tc.evidence, tc.applier); err == nil {
				t.Fatal("missing dependency should fail")
			}
		})
	}
}

func TestReviewServiceFromServicesHonorsOptionalCapability(t *testing.T) {
	if ReviewServiceFromServices(nil) != nil {
		t.Fatal("nil Services should not expose Reviews")
	}
	if ReviewServiceFromServices(&Services{}) != nil {
		t.Fatal("incomplete Services should not expose Reviews")
	}

	disabled := &reviewCapabilityControlPlane{enabled: false}
	services := &Services{
		ExecutionStore:   disabled,
		RunEvidence:      &RunEvidenceService{},
		ReviewCandidates: &reviewServiceStore{},
		Workspaces:       &WorkspaceService{},
	}
	if ReviewServiceFromServices(services) != nil {
		t.Fatal("disabled ReviewStore capability should not expose Reviews")
	}

	enabled := &reviewCapabilityControlPlane{enabled: true}
	services.ExecutionStore = enabled
	if ReviewServiceFromServices(services) == nil {
		t.Fatal("complete Review-capable Services should expose Reviews")
	}

	services.ReviewCandidates = nil
	if ReviewServiceFromServices(services) != nil {
		t.Fatal("missing private Review candidate reader should not expose Reviews")
	}
}
