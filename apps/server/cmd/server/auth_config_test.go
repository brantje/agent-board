package main

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestConfiguredAuthSigningKeyRequiresStableHighEntropyKey(t *testing.T) {
	t.Setenv("AGENT_BOARD_AUTH_SIGNING_KEY", "")
	if _, err := configuredAuthSigningKey(); err == nil {
		t.Fatal("missing auth signing key unexpectedly accepted")
	}

	t.Setenv("AGENT_BOARD_AUTH_SIGNING_KEY", "not-base64!")
	if _, err := configuredAuthSigningKey(); err == nil {
		t.Fatal("malformed auth signing key unexpectedly accepted")
	}

	t.Setenv("AGENT_BOARD_AUTH_SIGNING_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 31)))
	if _, err := configuredAuthSigningKey(); err == nil {
		t.Fatal("short auth signing key unexpectedly accepted")
	}

	want := bytes.Repeat([]byte{2}, 32)
	t.Setenv("AGENT_BOARD_AUTH_SIGNING_KEY", base64.StdEncoding.EncodeToString(want))
	first, err := configuredAuthSigningKey()
	if err != nil {
		t.Fatalf("configuredAuthSigningKey() first read: %v", err)
	}
	second, err := configuredAuthSigningKey()
	if err != nil {
		t.Fatalf("configuredAuthSigningKey() second read: %v", err)
	}
	if !bytes.Equal(first, want) || !bytes.Equal(second, want) || !bytes.Equal(first, second) {
		t.Fatalf("configured auth signing key was not stable")
	}
}
