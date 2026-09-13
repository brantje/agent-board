package app

import "testing"

func TestDummyPasswordHashIsValidArgon2id(t *testing.T) {
	ok, err := verifyPassword(dummyPasswordHash, "not-the-dummy-password")
	if err != nil {
		t.Fatalf("verifyPassword(dummyPasswordHash) error = %v", err)
	}
	if ok {
		t.Fatal("dummy password hash unexpectedly authenticated test password")
	}
}
