//go:build linux

package main

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestReadRegistrationTokenFromRegularFile(t *testing.T) {
	path := t.TempDir() + "/token"
	if err := os.WriteFile(path, []byte("registration-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var output strings.Builder
	value, err := readRegistrationToken(bufio.NewReader(file), file, &output)
	if err != nil {
		t.Fatal(err)
	}
	if value != "registration-token\n" {
		t.Fatalf("token=%q", value)
	}
	if output.Len() != 0 {
		t.Fatalf("regular-file input unexpectedly wrote output %q", output.String())
	}
}

func TestReadRegistrationTokenRejectsCharDeviceWithoutTerminalState(t *testing.T) {
	file, err := os.Open("/dev/null")
	if err != nil {
		t.Skipf("open /dev/null: %v", err)
	}
	defer file.Close()

	_, err = readRegistrationToken(bufio.NewReader(file), file, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "disable terminal echo") {
		t.Fatalf("error=%v", err)
	}
}
