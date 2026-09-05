package server

import "testing"

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

func currentDeliveryWriter(delivery *sessionDelivery) *connectionWriter {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	return delivery.writer
}
