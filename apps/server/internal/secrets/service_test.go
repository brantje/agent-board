package secrets

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
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

type failingStore struct {
	putErr error
	getErr error
}

func (s *failingStore) PutSecret(context.Context, Record) (Record, error) {
	return Record{}, s.putErr
}

func (s *failingStore) GetSecret(context.Context, *string, string) (Record, error) {
	return Record{}, s.getErr
}

type failingCipher struct {
	encryptErr error
	decryptErr error
}

func (c *failingCipher) Encrypt([]byte) ([]byte, int, error) {
	return nil, 0, c.encryptErr
}

func (c *failingCipher) Decrypt([]byte, int) ([]byte, error) {
	return nil, c.decryptErr
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

func TestServiceRejectsInvalidConstructionAndInputs(t *testing.T) {
	key := make([]byte, 32)
	cipher, err := NewAESGCM(1, map[int][]byte{1: key})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(nil, cipher); err == nil {
		t.Fatal("expected nil store error")
	}
	if _, err := NewService(&memoryStore{}, nil); err == nil {
		t.Fatal("expected nil cipher error")
	}
	service, err := NewService(&memoryStore{}, cipher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Put(context.Background(), Scope{}, " ", []byte("value")); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("blank ref error = %v", err)
	}
	if _, err := service.Put(context.Background(), Scope{}, "token", nil); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("empty plaintext error = %v", err)
	}
	if _, err := service.Resolve(context.Background(), Scope{}, " "); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("blank resolve error = %v", err)
	}
	if _, err := service.Resolve(context.Background(), Scope{}, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing secret error = %v", err)
	}
}

func TestServiceWrapsEncryptPersistAndLoadFailures(t *testing.T) {
	boom := errors.New("boom")
	service, err := NewService(&memoryStore{}, &failingCipher{encryptErr: boom})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Put(context.Background(), Scope{}, "token", []byte("value")); err == nil || !errors.Is(err, boom) || !strings.Contains(err.Error(), "encrypt secret") {
		t.Fatalf("encrypt error = %v", err)
	}

	key := make([]byte, 32)
	cipher, err := NewAESGCM(1, map[int][]byte{1: key})
	if err != nil {
		t.Fatal(err)
	}
	service, err = NewService(&failingStore{putErr: boom}, cipher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Put(context.Background(), Scope{}, "token", []byte("value")); err == nil || !errors.Is(err, boom) || !strings.Contains(err.Error(), "persist secret") {
		t.Fatalf("persist error = %v", err)
	}

	service, err = NewService(&failingStore{getErr: boom}, cipher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve(context.Background(), Scope{}, "token"); err == nil || !errors.Is(err, boom) || !strings.Contains(err.Error(), "load secret") {
		t.Fatalf("load error = %v", err)
	}
}

func TestAESGCMValidationFailures(t *testing.T) {
	key := make([]byte, 32)
	if _, err := NewAESGCM(0, map[int][]byte{1: key}); err == nil {
		t.Fatal("expected non-positive current version error")
	}
	if _, err := NewAESGCM(1, map[int][]byte{1: make([]byte, 31)}); err == nil {
		t.Fatal("expected invalid key length error")
	}
	if _, err := NewAESGCM(2, map[int][]byte{1: key}); err == nil {
		t.Fatal("expected missing current key version error")
	}
	cipher, err := NewAESGCM(1, map[int][]byte{1: key})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := cipher.Encrypt(nil); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("empty encrypt error = %v", err)
	}
	if _, err := cipher.Decrypt([]byte("ciphertext"), 99); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("unknown version error = %v", err)
	}
	if _, err := cipher.Decrypt([]byte("short"), 1); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("short ciphertext error = %v", err)
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
	if _, err := ParseKey(""); err == nil {
		t.Fatal("expected empty key error")
	}
	if _, err := ParseKey("%%%not-base64%%%"); err == nil {
		t.Fatal("expected base64 decode error")
	}
	if _, err := ParseKey("short"); err == nil {
		t.Fatal("expected key length error")
	}
}
