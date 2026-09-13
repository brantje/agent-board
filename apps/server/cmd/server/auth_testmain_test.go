package main

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("AGENT_BOARD_AUTH_SIGNING_KEY") == "" {
		_ = os.Setenv("AGENT_BOARD_AUTH_SIGNING_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("j", 32))))
	}
	os.Exit(m.Run())
}
