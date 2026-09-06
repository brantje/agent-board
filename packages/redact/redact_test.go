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

func TestReplacementCannotExposeConfiguredMarker(t *testing.T) {
	for _, secret := range []string{"***", "*", "[REDACTED]", "<redacted>"} {
		output := Bytes([]byte(secret), []string{secret})
		if bytes.Contains(output, []byte(secret)) {
			t.Fatalf("output %q contains secret %q", output, secret)
		}
	}
}
