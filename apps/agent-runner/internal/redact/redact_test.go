package redact

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestStreamRedactsAcrossChunkBoundaries(t *testing.T) {
	stream := New([]string{"SECRET", "TOKEN-LONG", "SECRET"})
	var output bytes.Buffer
	output.Write(stream.Push([]byte("before SEC")))
	output.Write(stream.Push([]byte("RET middle TOKEN")))
	output.Write(stream.Push([]byte("-LONG after")))
	output.Write(stream.Flush())
	if got, want := output.String(), "before *** middle *** after"; got != want {
		t.Fatalf("redaction mismatch: got %q want %q", got, want)
	}
}

func TestStreamReplacementDoesNotExposeConfiguredSecret(t *testing.T) {
	for _, secret := range []string{"***", "*", "[REDACTED]", "<redacted>"} {
		t.Run(secret, func(t *testing.T) {
			stream := New([]string{secret})
			var output bytes.Buffer
			output.Write(stream.Push([]byte(secret)))
			output.Write(stream.Flush())
			if bytes.Contains(output.Bytes(), []byte(secret)) {
				t.Fatalf("redacted output %q still contains configured secret %q", output.String(), secret)
			}
		})
	}
}

func TestStreamFallsBackToDroppingSecretWhenEveryMarkerCollides(t *testing.T) {
	secrets := []string{"*", "[REDACTED]", "<redacted>", "[masked]"}
	stream := New(secrets)
	var output bytes.Buffer
	output.Write(stream.Push([]byte("before *** after")))
	output.Write(stream.Flush())
	if got, want := output.String(), "before  after"; got != want {
		t.Fatalf("fallback redaction mismatch: got %q want %q", got, want)
	}
}

func TestStreamIgnoresEmptySecretsAndPassesThroughWithoutPatterns(t *testing.T) {
	stream := New([]string{"", ""})
	if got := string(stream.Push([]byte("unchanged"))); got != "unchanged" {
		t.Fatalf("unexpected output %q", got)
	}
	if got := stream.Flush(); len(got) != 0 {
		t.Fatalf("unexpected flush %q", got)
	}
}

func TestReaderRedactsWithSmallConsumerBuffers(t *testing.T) {
	source := bytes.NewBufferString("alpha-sensitive-value-omega")
	reader := NewReader(source, []string{"sensitive-value"})
	data, err := io.ReadAll(&oneByteReader{reader: reader})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "alpha-***-omega"; got != want {
		t.Fatalf("reader mismatch: got %q want %q", got, want)
	}
}

func TestReaderEmitsShortSafeOutputWithoutWaitingForNextSourceRead(t *testing.T) {
	unblock := make(chan struct{})
	defer close(unblock)
	source := &blockAfterFirstReader{first: []byte("ready\n"), unblock: unblock}
	reader := NewReader(source, []string{"a-much-longer-secret"})

	type result struct {
		data string
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		buffer := make([]byte, 32)
		n, err := reader.Read(buffer)
		resultCh <- result{data: string(buffer[:n]), err: err}
	}()

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("unexpected read error: %v", got.err)
		}
		if got.data != "ready\n" {
			t.Fatalf("unexpected output %q", got.data)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("safe output was withheld while source waited for more input")
	}
}

func TestReaderWithoutSecretsReturnsOriginalReader(t *testing.T) {
	source := bytes.NewBufferString("plain")
	reader := NewReader(source, nil)
	if reader != source {
		t.Fatal("expected source reader to be reused when no redaction is needed")
	}
}

type oneByteReader struct{ reader io.Reader }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.reader.Read(p)
}

type blockAfterFirstReader struct {
	first   []byte
	unblock <-chan struct{}
}

func (r *blockAfterFirstReader) Read(p []byte) (int, error) {
	if r.first != nil {
		data := r.first
		r.first = nil
		return copy(p, data), nil
	}
	<-r.unblock
	return 0, io.EOF
}
