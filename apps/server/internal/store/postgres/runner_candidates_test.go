package postgres

import "testing"

func TestLiveRunnerCandidatesUsesConfiguredSupplier(t *testing.T) {
	s := &Store{}
	if got := s.LiveRunnerCandidates("opencode"); got != nil {
		t.Fatalf("nil supplier returned %v", got)
	}
	s.SetRunnerCandidates(func(engine string) []string {
		if engine != "opencode" {
			t.Fatalf("engine=%q", engine)
		}
		return []string{"runner-1"}
	})
	got := s.LiveRunnerCandidates("opencode")
	if len(got) != 1 || got[0] != "runner-1" {
		t.Fatalf("candidates=%v", got)
	}
}
