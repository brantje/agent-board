package secrets

import (
	"context"
	"errors"
	"testing"
)

func TestResolveRejectsCiphertextSwappedBetweenSecretIdentities(t *testing.T) {
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 1)
	}
	cipher, err := NewAESGCM(1, map[int][]byte{1: key})
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{}
	service, err := NewService(store, cipher)
	if err != nil {
		t.Fatal(err)
	}
	projectA := "project-a"
	projectB := "project-b"
	if _, err := service.Put(context.Background(), Scope{ProjectID: &projectA}, "token", []byte("secret-a")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Put(context.Background(), Scope{ProjectID: &projectB}, "token", []byte("secret-b")); err != nil {
		t.Fatal(err)
	}

	source := store.records[recordKey(&projectA, "token")]
	destination := store.records[recordKey(&projectB, "token")]
	destination.Ciphertext = append([]byte(nil), source.Ciphertext...)
	destination.KeyVersion = source.KeyVersion
	store.records[recordKey(&projectB, "token")] = destination

	if _, err := service.Resolve(context.Background(), Scope{ProjectID: &projectB}, "token"); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("swapped ciphertext error = %v, want ErrDecrypt", err)
	}
}
