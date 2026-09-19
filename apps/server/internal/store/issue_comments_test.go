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
