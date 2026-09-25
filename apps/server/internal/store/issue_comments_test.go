package store

import "testing"

func TestValidIssueCommentReaction(t *testing.T) {
	valid := []string{
		IssueCommentReactionThumbsUp,
		IssueCommentReactionThumbsDown,
		IssueCommentReactionLaugh,
		IssueCommentReactionHooray,
		IssueCommentReactionConfused,
		IssueCommentReactionHeart,
		IssueCommentReactionRocket,
		IssueCommentReactionEyes,
	}
	for _, reaction := range valid {
		if !ValidIssueCommentReaction(reaction) {
			t.Fatalf("expected %q to be valid", reaction)
		}
	}
	if ValidIssueCommentReaction("PARTY") || ValidIssueCommentReaction("") {
		t.Fatal("unsupported reaction was accepted")
	}
}

func TestIssueCommentTargetValid(t *testing.T) {
	if !(IssueCommentTarget{Type: IssueCommentTargetTypeAgent, ID: "agent"}).Valid() ||
		!(IssueCommentTarget{Type: IssueCommentTargetTypeSquad, ID: "squad"}).Valid() {
		t.Fatal("valid typed comment targets were rejected")
	}
	for _, target := range []IssueCommentTarget{{}, {Type: "USER", ID: "user"}, {Type: IssueCommentTargetTypeAgent}} {
		if target.Valid() {
			t.Fatalf("invalid typed comment target accepted: %+v", target)
		}
	}
}
