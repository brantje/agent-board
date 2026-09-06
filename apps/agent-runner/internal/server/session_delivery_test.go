package server

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAttachDeliveriesDoesNotStealLiveWriter(t *testing.T) {
	runner := New(Config{WorkspaceRoot: t.TempDir(), MaxActiveSessions: 1})
	first := &connectionWriter{conn: &recordingWebSocketWriteConn{}}
	second := &connectionWriter{conn: &recordingWebSocketWriteConn{}}
	delivery := runner.registerDelivery("owned", first)

	runner.attachDeliveries(second)
	if got := currentDeliveryWriter(delivery); got != first {
		t.Fatalf("live delivery writer was replaced: got %p want %p", got, first)
	}

	runner.detachDeliveries(first)
	runner.attachDeliveries(second)
	if got := currentDeliveryWriter(delivery); got != second {
		t.Fatalf("idle delivery did not attach to reconnect: got %p want %p", got, second)
	}
}

func TestDetachedDeliveryExpiresWithoutReconnect(t *testing.T) {
	delivery := newSessionDelivery(context.Background(), nil, 20*time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := delivery.waitWriter()
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, errDeliveryExpired) {
			t.Fatalf("expected delivery expiry, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("detached delivery did not expire")
	}
}

func TestDetachedDeliveryCanReconnectBeforeExpiry(t *testing.T) {
	delivery := newSessionDelivery(context.Background(), nil, time.Second)

	writer := &connectionWriter{conn: &recordingWebSocketWriteConn{}}
	done := make(chan error, 1)
	go func() {
		got, err := delivery.waitWriter()
		if err == nil && got != writer {
			err = errors.New("reconnected writer was not returned")
		}
		done <- err
	}()

	delivery.attach(writer)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("delivery did not resume after reconnect")
	}
}

func TestDetachRestartsReconnectWindow(t *testing.T) {
	first := &connectionWriter{conn: &recordingWebSocketWriteConn{}}
	delivery := newSessionDelivery(context.Background(), first, 40*time.Millisecond)
	delivery.detach(first)

	time.Sleep(20 * time.Millisecond)
	second := &connectionWriter{conn: &recordingWebSocketWriteConn{}}
	if !delivery.attachIfIdle(second) {
		t.Fatal("delivery did not reconnect within retention window")
	}
	delivery.detach(second)

	time.Sleep(25 * time.Millisecond)
	third := &connectionWriter{conn: &recordingWebSocketWriteConn{}}
	if !delivery.attachIfIdle(third) {
		t.Fatal("reconnect window was not restarted after a later detach")
	}
}

func currentDeliveryWriter(delivery *sessionDelivery) *connectionWriter {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	return delivery.writer
}
