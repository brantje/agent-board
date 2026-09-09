package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"github.com/brantje/agent-board/apps/server/internal/runner"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

var ErrRunnerAuthentication = errors.New("runner authentication failed")

func (s *RunnerService) ProjectRunners(ctx context.Context, id string) ([]string, error) {
	ids, err := s.store.ListProjectRunnerIDs(ctx, id)
	return ids, translateStoreError(err, "project")
}
func (s *RunnerService) SetProjectRunners(ctx context.Context, id string, ids []string) error {
	return translateStoreError(s.store.SetProjectRunnerIDs(ctx, id, ids), "project")
}

type RunnerService struct {
	store       store.RunnerStore
	Connections *runner.Registry
}

func NewRunnerService(s store.RunnerStore) *RunnerService {
	service := &RunnerService{store: s}
	service.Connections = runner.NewRegistry(service, s)
	return service
}

func runnerCredential() (string, []byte, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}
func (s *RunnerService) Create(ctx context.Context, name string) (store.Runner, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.Runner{}, "", invalid("runner name is required")
	}
	token, hash, err := runnerCredential()
	if err != nil {
		return store.Runner{}, "", err
	}
	r, err := s.store.CreateRunner(ctx, store.Runner{Name: name, TokenHash: hash})
	if err != nil {
		return store.Runner{}, "", translateStoreError(err, "runner")
	}
	return r, token, nil
}
func (s *RunnerService) Get(ctx context.Context, id string) (store.Runner, error) {
	r, err := s.store.GetRunner(ctx, id)
	return r, translateStoreError(err, "runner")
}
func (s *RunnerService) List(ctx context.Context) ([]store.Runner, error) {
	return s.store.ListRunners(ctx)
}
func (s *RunnerService) Rename(ctx context.Context, id, name string) (store.Runner, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.Runner{}, invalid("runner name is required")
	}
	r, err := s.store.RenameRunner(ctx, id, name)
	return r, translateStoreError(err, "runner")
}
func (s *RunnerService) Rotate(ctx context.Context, id string) (store.Runner, string, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return store.Runner{}, "", err
	}
	if r.Internal {
		return store.Runner{}, "", invalid("internal runner credentials are server managed")
	}
	token, hash, err := runnerCredential()
	if err != nil {
		return store.Runner{}, "", err
	}
	r, err = s.store.RotateRunnerCredential(ctx, id, hash)
	if err != nil {
		return store.Runner{}, "", translateStoreError(err, "runner")
	}
	return r, token, nil
}
func (s *RunnerService) Revoke(ctx context.Context, id string, deleted bool) (store.Runner, error) {
	r, err := s.store.RevokeRunner(ctx, id, deleted)
	if err == nil {
		s.Connections.Disconnect(id)
	}
	return r, translateStoreError(err, "runner")
}
func (s *RunnerService) Authenticate(ctx context.Context, id, token string) (store.Runner, error) {
	r, err := s.store.GetRunner(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
			return store.Runner{}, ErrRunnerAuthentication
		}
		return store.Runner{}, err
	}
	hash := sha256.Sum256([]byte(token))
	if token == "" || r.RevokedAt != nil || r.DeletedAt != nil || subtle.ConstantTimeCompare(hash[:], r.TokenHash) != 1 {
		return store.Runner{}, ErrRunnerAuthentication
	}
	return r, nil
}
