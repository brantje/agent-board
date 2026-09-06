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
	const retention = 2 * time.Second

	first := &connectionWriter{conn: &recordingWebSocketWriteConn{}}
	delivery := newSessionDelivery(context.Background(), first, retention)
	delivery.detach(first)
	originalDeadline := currentDeliveryDeadline(delivery)

	// Create a clear gap between the original and restarted deadlines.
	time.Sleep(500 * time.Millisecond)
	second := &connectionWriter{conn: &recordingWebSocketWriteConn{}}
	if !delivery.attachIfIdle(second) {
		t.Fatal("delivery did not reconnect within retention window")
	}
	delivery.detach(second)
	restartedDeadline := currentDeliveryDeadline(delivery)
	if !restartedDeadline.After(originalDeadline) {
		t.Fatalf("reconnect deadline was not restarted: original=%v restarted=%v", originalDeadline, restartedDeadline)
	}

	// Cross the original deadline while keeping a generous margin before the
	// restarted deadline. A delivery that failed to restart expires here.
	crossOriginalAt := originalDeadline.Add(100 * time.Millisecond)
	if delay := time.Until(crossOriginalAt); delay > 0 {
		time.Sleep(delay)
	}
	now := time.Now()
	if now.Before(originalDeadline) {
		t.Fatalf("test did not cross original deadline: now=%v original=%v", now, originalDeadline)
	}
	if !now.Before(restartedDeadline) {
		t.Fatalf("test exceeded restarted deadline: now=%v restarted=%v", now, restartedDeadline)
	}

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

func currentDeliveryDeadline(delivery *sessionDelivery) time.Time {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	return delivery.expiresAt
}
