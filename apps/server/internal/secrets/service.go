package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	ErrNotFound      = errors.New("secret not found")
	ErrInvalidSecret = errors.New("invalid secret")
	ErrDecrypt       = errors.New("secret decryption failed")
)

type Scope struct {
	ProjectID *string
}

type Record struct {
	ID         string
	ProjectID  *string
	Ref        string
	Ciphertext []byte
	KeyVersion int
}

type Store interface {
	PutSecret(context.Context, Record) (Record, error)
	GetSecret(context.Context, *string, string) (Record, error)
}

type Cipher interface {
	Encrypt([]byte) ([]byte, int, error)
	Decrypt([]byte, int) ([]byte, error)
}

type Metadata struct {
	Ref       string
	ProjectID *string
}

type Service struct {
	store  Store
	cipher Cipher
}

func NewService(store Store, cipher Cipher) (*Service, error) {
	if store == nil || cipher == nil {
		return nil, fmt.Errorf("secret store and cipher are required")
	}
	return &Service{store: store, cipher: cipher}, nil
}

func (s *Service) Put(ctx context.Context, scope Scope, ref string, plaintext []byte) (Metadata, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || len(plaintext) == 0 {
		return Metadata{}, ErrInvalidSecret
	}
	projectID := cloneString(scope.ProjectID)
	payload, err := encodeSecretPayload(projectID, ref, plaintext)
	if err != nil {
		return Metadata{}, fmt.Errorf("encode secret payload: %w", err)
	}
	ciphertext, keyVersion, err := s.cipher.Encrypt(payload)
	if err != nil {
		return Metadata{}, fmt.Errorf("encrypt secret: %w", err)
	}
	record, err := s.store.PutSecret(ctx, Record{ProjectID: projectID, Ref: ref, Ciphertext: ciphertext, KeyVersion: keyVersion})
	if err != nil {
		return Metadata{}, fmt.Errorf("persist secret: %w", err)
	}
	return Metadata{Ref: record.Ref, ProjectID: cloneString(record.ProjectID)}, nil
}

func (s *Service) Resolve(ctx context.Context, scope Scope, ref string) ([]byte, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrInvalidSecret
	}

	record, err := s.store.GetSecret(ctx, scope.ProjectID, ref)
	if errors.Is(err, ErrNotFound) && scope.ProjectID != nil {
		record, err = s.store.GetSecret(ctx, nil, ref)
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load secret: %w", err)
	}
	payload, err := s.cipher.Decrypt(record.Ciphertext, record.KeyVersion)
	if err != nil {
		return nil, ErrDecrypt
	}
	plaintext, err := decodeSecretPayload(payload, record.ProjectID, record.Ref)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

type secretPayload struct {
	ProjectID *string `json:"projectId"`
	Ref       string  `json:"ref"`
	Value     []byte  `json:"value"`
}

func encodeSecretPayload(projectID *string, ref string, plaintext []byte) ([]byte, error) {
	return json.Marshal(secretPayload{ProjectID: cloneString(projectID), Ref: ref, Value: append([]byte(nil), plaintext...)})
}

func decodeSecretPayload(payload []byte, projectID *string, ref string) ([]byte, error) {
	var decoded secretPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, err
	}
	if decoded.Ref != ref || !sameProjectID(decoded.ProjectID, projectID) || len(decoded.Value) == 0 {
		return nil, ErrDecrypt
	}
	return append([]byte(nil), decoded.Value...), nil
}

func sameProjectID(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

type AESGCM struct {
	currentVersion int
	keys           map[int][]byte
}

func NewAESGCM(currentVersion int, keys map[int][]byte) (*AESGCM, error) {
	if currentVersion < 1 {
		return nil, fmt.Errorf("current key version must be positive")
	}
	cloned := make(map[int][]byte, len(keys))
	for version, key := range keys {
		if len(key) != 32 {
			return nil, fmt.Errorf("secret key version %d must be 32 bytes", version)
		}
		cloned[version] = append([]byte(nil), key...)
	}
	if _, ok := cloned[currentVersion]; !ok {
		return nil, fmt.Errorf("current key version %d is not configured", currentVersion)
	}
	return &AESGCM{currentVersion: currentVersion, keys: cloned}, nil
}

func ParseKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("secret encryption key is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode secret encryption key: %w", err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("secret encryption key must decode to 32 bytes")
	}
	return decoded, nil
}

func (c *AESGCM) Encrypt(plaintext []byte) ([]byte, int, error) {
	if len(plaintext) == 0 {
		return nil, 0, ErrInvalidSecret
	}
	aead, err := c.aead(c.currentVersion)
	if err != nil {
		return nil, 0, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, 0, fmt.Errorf("generate secret nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, plaintext, nil)
	ciphertext := append(append([]byte(nil), nonce...), sealed...)
	return ciphertext, c.currentVersion, nil
}

func (c *AESGCM) Decrypt(ciphertext []byte, keyVersion int) ([]byte, error) {
	aead, err := c.aead(keyVersion)
	if err != nil {
		return nil, err
	}
	nonceSize := aead.NonceSize()
	if len(ciphertext) <= nonceSize {
		return nil, ErrDecrypt
	}
	plaintext, err := aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

func (c *AESGCM) aead(version int) (cipher.AEAD, error) {
	key, ok := c.keys[version]
	if !ok {
		return nil, ErrDecrypt
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrDecrypt
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrDecrypt
	}
	return aead, nil
}
