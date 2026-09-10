package opencode

import (
	"net"
	"testing"

	"github.com/brantje/agent-board/apps/server/internal/executioncontext"
)

func TestNativeServerAddressIsStableAndRunScoped(t *testing.T) {
	adapter := New()
	first := executioncontext.SafeContext{Run: executioncontext.RunContext{ID: "run-a"}}
	second := executioncontext.SafeContext{Run: executioncontext.RunContext{ID: "run-b"}}

	firstAddress := adapter.nativeServerAddress(first)
	if firstAddress != adapter.nativeServerAddress(first) {
		t.Fatalf("same Run produced unstable native server address: %q then %q", firstAddress, adapter.nativeServerAddress(first))
	}
	secondAddress := adapter.nativeServerAddress(second)
	if firstAddress == secondAddress {
		t.Fatalf("different Runs share native server address %q", firstAddress)
	}

	for _, address := range []string{firstAddress, secondAddress} {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			t.Fatalf("SplitHostPort(%q): %v", address, err)
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			t.Fatalf("native server address %q is not loopback", address)
		}
		if port == "4096" {
			t.Fatalf("run-scoped native server address unexpectedly uses shared default port: %q", address)
		}
	}
}

func TestNativeServerAddressPreservesExplicitTestAddress(t *testing.T) {
	const address = "127.0.0.1:54321"
	adapter := newWithAddress(address)
	got := adapter.nativeServerAddress(executioncontext.SafeContext{Run: executioncontext.RunContext{ID: "run-a"}})
	if got != address {
		t.Fatalf("nativeServerAddress()=%q want %q", got, address)
	}
}

func TestNativeServerAddressFallsBackWithoutRunID(t *testing.T) {
	if got := New().nativeServerAddress(executioncontext.SafeContext{}); got != defaultAddress {
		t.Fatalf("nativeServerAddress()=%q want fallback %q", got, defaultAddress)
	}
}
