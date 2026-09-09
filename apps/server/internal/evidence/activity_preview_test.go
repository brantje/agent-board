package evidence

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundActivityPreviewTrimsAndBoundsUTF8(t *testing.T) {
	if got := BoundActivityPreview("  short preview  "); got != "short preview" {
		t.Fatalf("short preview=%q", got)
	}
	long := strings.Repeat("界", ActivityPreviewLimit)
	got := BoundActivityPreview(long)
	if len(got) > ActivityPreviewLimit {
		t.Fatalf("preview length=%d limit=%d", len(got), ActivityPreviewLimit)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("preview is not valid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("preview missing truncation suffix: %q", got[len(got)-8:])
	}
}
