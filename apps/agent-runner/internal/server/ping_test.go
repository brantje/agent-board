package server

import (
	"testing"
	"time"
)

func TestPingDeadlineIsBasedOnSendTime(t *testing.T) {
	before := time.Now()
	deadline := nextPingDeadline()
	after := time.Now()

	if deadline.Before(before.Add(pingWriteTimeout)) {
		t.Fatalf("ping deadline %v predates send-time window", deadline)
	}
	if deadline.After(after.Add(pingWriteTimeout)) {
		t.Fatalf("ping deadline %v exceeds send-time window", deadline)
	}
}
