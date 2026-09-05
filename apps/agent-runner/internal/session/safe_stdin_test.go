package session

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type bufferWriteCloser struct {
	bytes.Buffer
	closes int
}

func (b *bufferWriteCloser) Close() error {
	b.closes++
	return nil
}

func TestSafeWriteCloserIsIdempotent(t *testing.T) {
	underlying := &bufferWriteCloser{}
	writer := newSafeWriteCloser(underlying)
	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if underlying.closes != 1 || underlying.String() != "hello" {
		t.Fatalf("unexpected underlying state closes=%d data=%q", underlying.closes, underlying.String())
	}
	if _, err := writer.Write([]byte("later")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("expected closed-pipe error, got %v", err)
	}
}
