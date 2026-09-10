package runner

import "testing"

func TestCompleteTransferRetainsOnlyUnobservedResults(t *testing.T) {
	c := &Connection{
		transferWaiters: map[string]*transferWaiter{},
		transferDone:    map[string]transferResult{},
		done:            make(chan struct{}),
	}

	waiter := &transferWaiter{result: make(chan transferResult, 1)}
	c.transferWaiters["session-waiting"] = waiter
	observed := transferResult{transferID: "transfer-waiting", payload: []byte("observed")}
	if err := c.completeTransfer("session-waiting", observed); err != nil {
		t.Fatal(err)
	}
	result := <-waiter.result
	if result.transferID != observed.transferID || string(result.payload) != string(observed.payload) {
		t.Fatalf("waiter result=%+v", result)
	}
	c.mu.Lock()
	_, waiterStillRegistered := c.transferWaiters["session-waiting"]
	_, observedStillCached := c.transferDone["session-waiting"]
	c.mu.Unlock()
	if waiterStillRegistered || observedStillCached {
		t.Fatalf("consumed transfer retained: waiter=%v cached=%v", waiterStillRegistered, observedStillCached)
	}

	unobserved := transferResult{transferID: "transfer-early", payload: []byte("early")}
	if err := c.completeTransfer("session-early", unobserved); err != nil {
		t.Fatal(err)
	}
	id, payload, err := c.ReceiveTransfer(t.Context(), "session-early", nil)
	if err != nil || id != unobserved.transferID || string(payload) != string(unobserved.payload) {
		t.Fatalf("early transfer id=%q payload=%q err=%v", id, payload, err)
	}
	c.mu.Lock()
	_, earlyStillCached := c.transferDone["session-early"]
	c.mu.Unlock()
	if earlyStillCached {
		t.Fatal("early transfer remained cached after ReceiveTransfer")
	}
}
