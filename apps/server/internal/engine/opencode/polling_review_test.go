package opencode

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEngineRecoversFromTransientNativeActivityPollFailure(t *testing.T) {
	var activeCalls atomic.Int32
	server := newPollingNativeServer(t, "ses_transient", func(w http.ResponseWriter, _ *http.Request) {
		switch activeCalls.Add(1) {
		case 1:
			http.Error(w, "temporary", http.StatusBadGateway)
		case 2:
			writeNativeJSON(t, w, map[string]any{"data": map[string]any{"ses_transient": map[string]any{"type": "running"}}})
		default:
			writeNativeJSON(t, w, map[string]any{"data": map[string]any{}})
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	launcher, adapter := pollingAdapterForServer(t, server)
	if _, err := adapter.Execute(ctx, nativeStateRequest(launcher)); err != nil {
		t.Fatalf("Execute() error=%v", err)
	}
	if calls := activeCalls.Load(); calls < 4 {
		t.Fatalf("activity poll calls=%d want transient failure, active state, and two inactive confirmations", calls)
	}
}

func TestEngineBoundsConsecutiveNativeActivityPollFailures(t *testing.T) {
	var activeCalls atomic.Int32
	server := newPollingNativeServer(t, "ses_poll_failures", func(w http.ResponseWriter, _ *http.Request) {
		activeCalls.Add(1)
		http.Error(w, "temporary", http.StatusBadGateway)
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	launcher, adapter := pollingAdapterForServer(t, server)
	_, err := adapter.Execute(ctx, nativeStateRequest(launcher))
	if err == nil || !strings.Contains(err.Error(), "after 3 consecutive failures") || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("Execute() error=%v", err)
	}
	if calls := activeCalls.Load(); calls != nativeStatePollFailureLimit {
		t.Fatalf("activity poll calls=%d want %d", calls, nativeStatePollFailureLimit)
	}
}
