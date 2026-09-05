package redact

import (
	"bytes"
	"io"
	"testing"
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
