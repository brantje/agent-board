package redact

import "testing"

func TestStreamWrapperPreservesSafeOutput(t *testing.T) {
	stream := New([]string{"secret"})
	output := stream.Push([]byte("safe-output"))
	output = append(output, stream.Flush()...)
	if got := string(output); got != "safe-output" {
		t.Fatalf("unexpected sanitized output %q", got)
	}
}
