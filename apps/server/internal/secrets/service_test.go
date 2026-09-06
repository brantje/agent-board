package secrets

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

type memoryStore struct{ records map[string]Record }

func (m *memoryStore) PutSecret(_ context.Context, r Record) (Record, error) {
	if m.records == nil {
		m.records = map[string]Record{}
	}
	r.ID = "id"
	m.records[recordKey(r.ProjectID, r.Ref)] = r
	return r, nil
}

func (m *memoryStore) GetSecret(_ context.Context, projectID *string, ref string) (Record, error) {
	r, ok := m.records[recordKey(projectID, ref)]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}

func recordKey(projectID *string, ref string) string {
	if projectID == nil {
		return "global/" + ref
	}
	return *projectID + "/" + ref
}

func TestAESGCMRoundTripUsesUniqueNonce(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cipher, err := NewAESGCM(1, map[int][]byte{1: key})
	if err != nil {
		t.Fatal(err)
	}
	first, version, err := cipher.Encrypt([]byte("secret-value"))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := cipher.Encrypt([]byte("secret-value"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) == string(second) {
		t.Fatal("ciphertexts unexpectedly identical")
	}
	plain, err := cipher.Decrypt(first, version)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(plain); got != "secret-value" {
		t.Fatalf("plaintext = %q", got)
	}
}

func TestResolvePrefersProjectThenFallsBackToGlobal(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = 7
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
	project := "project-1"
	if _, err := service.Put(context.Background(), Scope{}, "token", []byte("global")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Put(context.Background(), Scope{ProjectID: &project}, "token", []byte("project")); err != nil {
		t.Fatal(err)
	}
	got, err := service.Resolve(context.Background(), Scope{ProjectID: &project}, "token")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "project" {
		t.Fatalf("project resolution = %q", got)
	}
	other := "other"
	got, err = service.Resolve(context.Background(), Scope{ProjectID: &other}, "token")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "global" {
		t.Fatalf("global fallback = %q", got)
	}
}

func TestResolveHidesDecryptDetails(t *testing.T) {
	key := make([]byte, 32)
	cipher, err := NewAESGCM(1, map[int][]byte{1: key})
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{records: map[string]Record{
		"global/token": {Ref: "token", Ciphertext: []byte("bad"), KeyVersion: 1},
	}}
	service, err := NewService(store, cipher)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Resolve(context.Background(), Scope{}, "token")
	if !errors.Is(err, ErrDecrypt) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseKey(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString(make([]byte, 32))
	got, err := ParseKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 {
		t.Fatalf("len = %d", len(got))
	}
	if _, err := ParseKey("short"); err == nil {
		t.Fatal("expected parse error")
	}
}
