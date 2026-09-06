package redact

import (
	"bytes"
	"io"
	"testing"
)

func TestStreamRedactsAcrossChunkBoundaries(t *testing.T) {
	stream := New([]string{"SECRET", "TOKEN-LONG"})
	var output bytes.Buffer
	output.Write(stream.Push([]byte("before SEC")))
	output.Write(stream.Push([]byte("RET middle TOKEN")))
	output.Write(stream.Push([]byte("-LONG after")))
	output.Write(stream.Flush())
	if got, want := output.String(), "before *** middle *** after"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStringAndReaderRedact(t *testing.T) {
	if got := String("x-secret-y", []string{"secret"}); got != "x-***-y" {
		t.Fatalf("String() = %q", got)
	}
	got, err := io.ReadAll(NewReader(bytes.NewBufferString("x-secret-y"), []string{"secret"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "x-***-y" {
		t.Fatalf("reader = %q", got)
	}
}

func TestCompiledMatcherCanBeReused(t *testing.T) {
	matcher := Compile([]string{"secret"})
	if matcher.Empty() {
		t.Fatal("compiled matcher unexpectedly empty")
	}
	for _, input := range []string{"secret", "x-secret-y", "secret twice secret"} {
		if got := matcher.String(input); bytes.Contains([]byte(got), []byte("secret")) {
			t.Fatalf("matcher leaked secret for %q: %q", input, got)
		}
	}
	got, err := io.ReadAll(matcher.Reader(bytes.NewBufferString("reader-secret")))
	if err != nil || bytes.Contains(got, []byte("secret")) {
		t.Fatalf("matcher reader=%q err=%v", got, err)
	}
}

func TestReplacementBoundariesCannotComposeAnotherSecret(t *testing.T) {
	for _, test := range []struct {
		name   string
		input  string
		values []string
	}{
		{name: "replacement then literal", input: "triggera", values: []string{"trigger", "*a"}},
		{name: "literal then replacement", input: "atrigger", values: []string{"trigger", "a*"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := Bytes([]byte(test.input), test.values)
			for _, secret := range test.values {
				if bytes.Contains(output, []byte(secret)) {
					t.Fatalf("output %q contains secret %q", output, secret)
				}
			}
		})
	}
}

func TestLargeSafeRunPassesThroughUnchanged(t *testing.T) {
	input := bytes.Repeat([]byte("safe-output-"), 8192)
	if got := Bytes(input, []string{"configured-secret-value"}); !bytes.Equal(got, input) {
		t.Fatalf("large safe output changed: got=%d want=%d", len(got), len(input))
	}
}

func TestReplacementCannotExposeConfiguredMarker(t *testing.T) {
	for _, secret := range []string{"***", "*", "[REDACTED]", "<redacted>"} {
		output := Bytes([]byte(secret), []string{secret})
		if bytes.Contains(output, []byte(secret)) {
			t.Fatalf("output %q contains secret %q", output, secret)
		}
	}
}
