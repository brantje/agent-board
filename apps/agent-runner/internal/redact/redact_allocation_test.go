package redact

import "testing"

func TestAppendSanitizedReusesCallerBuffer(t *testing.T) {
	stream := New([]string{"secret"})
	output := make([]byte, 1, 128)
	output[0] = '!'
	base := &output[0]

	output = stream.appendSanitized(output, []byte("safe-output"))
	output = stream.flushSanitizedInto(output)

	if &output[0] != base {
		t.Fatal("sanitizer replaced caller-provided output buffer")
	}
	if got := string(output); got != "!safe-output" {
		t.Fatalf("unexpected sanitized output %q", got)
	}
}
