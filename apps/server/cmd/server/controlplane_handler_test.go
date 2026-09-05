package main

import (
	"context"
	"testing"
)

func TestControlPlaneHandlerRequiresDatabaseURL(t *testing.T) {
	handler, closeStore, err := controlPlaneHandler(context.Background(), "")
	if err == nil {
		t.Fatal("expected missing database URL to fail")
	}
	if handler != nil || closeStore != nil {
		t.Fatal("missing database URL must not return initialized resources")
	}
}
