package server

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type blockingStdinInput struct {
	started chan struct{}
	release chan struct{}
	writes  chan []byte
	once    sync.Once
}

func newBlockingStdinInput() *blockingStdinInput {
	return &blockingStdinInput{
		started: make(chan struct{}),
		release: make(chan struct{}),
		writes:  make(chan []byte, stdinQueueCapacity+4),
	}
}

func (i *blockingStdinInput) Write(data []byte) (int, error) {
	i.once.Do(func() {
		close(i.started)
		<-i.release
	})
	i.writes <- append([]byte(nil), data...)
	return len(data), nil
}

func (i *blockingStdinInput) Close() error { return nil }

func TestStdinPumpQueueOverflowIsTerminal(t *testing.T) {
	input := newBlockingStdinInput()
	pump := newStdinPump(input)

	if err := pump.Enqueue([]byte("first")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-input.started:
	case <-time.After(time.Second):
		t.Fatal("stdin writer did not block")
	}

	for index := 0; index < stdinQueueCapacity; index++ {
		if err := pump.Enqueue([]byte("queued")); err != nil {
			t.Fatalf("fill queue %d: %v", index, err)
		}
	}
	if err := pump.Enqueue([]byte("dropped")); !errors.Is(err, errStdinQueueFull) {
		t.Fatalf("expected queue-full error, got %v", err)
	}

	close(input.release)
	for index := 0; index < 2; index++ {
		select {
		case <-input.writes:
		case <-time.After(time.Second):
			t.Fatal("stdin queue did not resume draining")
		}
	}

	if err := pump.Enqueue([]byte("late")); !errors.Is(err, errStdinQueueFull) {
		t.Fatalf("stdin accepted data after a dropped chunk: %v", err)
	}
}
