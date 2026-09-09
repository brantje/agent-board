package store

import "testing"

func TestWorkingBranchForIssue(t *testing.T) {
	if got := WorkingBranchForIssue("AB-12"); got != "agent-board/AB-12" {
		t.Fatalf("WorkingBranchForIssue = %q, want agent-board/AB-12", got)
	}
}

func TestFormatAndParseIssueKey(t *testing.T) {
	key := FormatIssueKey("AB", 12)
	if key != "AB-12" {
		t.Fatalf("FormatIssueKey = %q, want AB-12", key)
	}
	prefix, number, err := ParseIssueKey(key)
	if err != nil || prefix != "AB" || number != 12 {
		t.Fatalf("ParseIssueKey(%q) = %q %d %v", key, prefix, number, err)
	}
}

func TestNormalizeIssuePrefix(t *testing.T) {
	if got := NormalizeIssuePrefix(" ab "); got != "AB" {
		t.Fatalf("NormalizeIssuePrefix = %q, want AB", got)
	}
}

func TestValidIssuePrefixRejectsInvalidValues(t *testing.T) {
	cases := []string{"", "A", "1B", "ab", "AB-", "TOOLONGPREFIX1", "A_B"}
	for _, value := range cases {
		if ValidIssuePrefix(value) {
			t.Fatalf("expected invalid prefix %q", value)
		}
	}
	if !ValidIssuePrefix("AB") || !ValidIssuePrefix("AGENT1") {
		t.Fatal("expected valid prefixes")
	}
}

func TestValidIssueKeyRejectsMalformedValues(t *testing.T) {
	cases := []string{"", "AB", "AB-0", "AB-12-extra", "12-3", "uuid-550e8400-e29b-41d4-a716-446655440000"}
	for _, value := range cases {
		if ValidIssueKey(value) {
			t.Fatalf("expected invalid issue key %q", value)
		}
	}
	if !ValidIssueKey("AB-12") {
		t.Fatal("expected AB-12 to be valid")
	}
}
