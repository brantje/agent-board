package runner

import (
	"testing"
	"time"
)

func TestDisconnectedSinceStartsAtFirstObservedDisconnect(t *testing.T) {
	registry := NewRegistry(nil, nil)
	before := time.Now()
	first, disconnected := registry.DisconnectedSince("runner-1")
	if !disconnected {
		t.Fatal("missing runner should be reported disconnected")
	}
	if first.Before(before) || first.After(time.Now()) {
		t.Fatalf("disconnect timestamp %v was not recorded at observation time", first)
	}

	time.Sleep(time.Millisecond)
	second, disconnected := registry.DisconnectedSince("runner-1")
	if !disconnected {
		t.Fatal("runner should remain disconnected")
	}
	if !second.Equal(first) {
		t.Fatalf("disconnect grace restarted: first=%v second=%v", first, second)
	}
}

func TestDisconnectDoesNotRestartExistingDisconnectGrace(t *testing.T) {
	registry := NewRegistry(nil, nil)
	first, _ := registry.DisconnectedSince("runner-1")
	time.Sleep(time.Millisecond)
	registry.Disconnect("runner-1")
	second, disconnected := registry.DisconnectedSince("runner-1")
	if !disconnected || !second.Equal(first) {
		t.Fatalf("disconnect reset grace: first=%v second=%v disconnected=%v", first, second, disconnected)
	}
}
