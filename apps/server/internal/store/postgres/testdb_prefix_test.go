package postgres

import "testing"

func TestPrefixForTestNamePreservesSuffixUniqueness(t *testing.T) {
	first := prefixForTestName("project-run-evidence-getters")
	second := prefixForTestName("project-run-evidence-getters-other")
	if first == second {
		t.Fatalf("prefix collision: both %q", first)
	}
	if len(first) < 2 || len(first) > 10 {
		t.Fatalf("prefix length out of range: %q", first)
	}
	if first[0] < 'A' || first[0] > 'Z' {
		t.Fatalf("prefix must start with a letter: %q", first)
	}
}
