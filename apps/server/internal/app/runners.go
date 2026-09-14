package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/runner"
	"github.com/brantje/agent-board/apps/server/internal/store"
)

var ErrRunnerAuthentication = errors.New("runner authentication failed")

type ProjectRunnerSettings struct {
	RunnerIDs       []string
	ProjectRunners []store.Runner
	SharedRunners  []store.Runner
}

func (s *RunnerService) ProjectRunners(ctx context.Context, id string) ([]string, error) {
	ids, err := s.store.ListProjectRunnerIDs(ctx, id)
	return ids, translateStoreError(err, "project")
}

func (s *RunnerService) ProjectRunnerSettings(ctx context.Context, id string) (ProjectRunnerSettings, error) {
	ids, err := s.ProjectRunners(ctx, id)
	if err != nil {
		return ProjectRunnerSettings{}, err
	}
	values, err := s.store.ListRunners(ctx)
	if err != nil {
		return ProjectRunnerSettings{}, err
	}
	settings := ProjectRunnerSettings{RunnerIDs: []string{}, ProjectRunners: []store.Runner{}, SharedRunners: []store.Runner{}}
	settings.RunnerIDs = append(settings.RunnerIDs, ids...)
	for _, value := range values {
		if value.Internal {
			continue
		}
		if value.ProjectID == nil {
			settings.SharedRunners = append(settings.SharedRunners, value)
			continue
		}
		if *value.ProjectID == id {
			settings.ProjectRunners = append(settings.ProjectRunners, value)
		}
	}
	return settings, nil
}

func (s *RunnerService) SetProjectRunners(ctx context.Context, id string, ids []string) error {
	return translateStoreError(s.store.SetProjectRunnerIDs(ctx, id, ids), "project")
}

type RunnerSessionTerminator interface {
	TerminateRunnerSessions(context.Context, string) error
}

type RunnerService struct {
	store       store.RunnerStore
	Connections *runner.Registry
	sessions    RunnerSessionTerminator
}

func NewRunnerService(s store.RunnerStore) *RunnerService {
	service := &RunnerService{store: s}
	service.Connections = runner.NewRegistry(service, s)
	return service
}

func (s *RunnerService) SetSessionTerminator(terminator RunnerSessionTerminator) {
	s.sessions = terminator
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

func (s *RunnerService) Create(ctx context.Context) (store.Runner, string, error) {
	return s.create(ctx, nil)
}

func (s *RunnerService) CreateForProject(ctx context.Context, projectID string) (store.Runner, string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return store.Runner{}, "", invalid("projectId is required")
	}
	if _, err := s.store.ListProjectRunnerIDs(ctx, projectID); err != nil {
		return store.Runner{}, "", translateStoreError(err, "project")
	}
	return s.create(ctx, &projectID)
}

func (s *RunnerService) create(ctx context.Context, projectID *string) (store.Runner, string, error) {
	registrationToken, registrationHash, err := runnerCredential()
	if err != nil {
		return store.Runner{}, "", err
	}
	r, err := s.store.CreateRunner(ctx, store.Runner{ProjectID: projectID, RegistrationTokenHash: registrationHash})
	if err != nil {
		return store.Runner{}, "", translateStoreError(err, "runner")
	}
	return r, registrationToken, nil
}

func (s *RunnerService) Register(ctx context.Context, registrationToken, hostname string) (store.Runner, string, error) {
	registrationToken = strings.TrimSpace(registrationToken)
	hostname = strings.TrimSpace(hostname)
	if registrationToken == "" {
		return store.Runner{}, "", invalid("runner registration token is required")
	}
	if hostname == "" {
		return store.Runner{}, "", invalid("runner hostname is required")
	}
	runnerToken, tokenHash, err := runnerCredential()
	if err != nil {
		return store.Runner{}, "", err
	}
	registrationHash := sha256.Sum256([]byte(registrationToken))
	r, err := s.store.RegisterRunner(ctx, registrationHash[:], store.Runner{Name: hostname, TokenHash: tokenHash})
	if err != nil {
		return store.Runner{}, "", translateStoreError(err, "runner")
	}
	return r, runnerToken, nil
}

func (s *RunnerService) Authenticate(ctx context.Context, id, token string) (store.Runner, error) {
	id = strings.TrimSpace(id)
	token = strings.TrimSpace(token)
	if id == "" || token == "" {
		return store.Runner{}, ErrRunnerAuthentication
	}
	r, err := s.store.GetRunner(ctx, id)
	if err != nil || r.RegisteredAt == nil || r.RevokedAt != nil || r.DeletedAt != nil || len(r.TokenHash) != sha256.Size {
		return store.Runner{}, ErrRunnerAuthentication
	}
	hash := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(hash[:], r.TokenHash) != 1 {
		return store.Runner{}, ErrRunnerAuthentication
	}
	return r, nil
}

func (s *RunnerService) List(ctx context.Context) ([]store.Runner, error) {
	values, err := s.store.ListRunners(ctx)
	return values, translateStoreError(err, "runner")
}

func (s *RunnerService) Get(ctx context.Context, id string) (store.Runner, error) {
	value, err := s.store.GetRunner(ctx, strings.TrimSpace(id))
	return value, translateStoreError(err, "runner")
}

func (s *RunnerService) Rename(ctx context.Context, id, name string) (store.Runner, error) {
	return s.Update(ctx, id, name, nil)
}

func (s *RunnerService) Update(ctx context.Context, id, name string, maxActiveSessions *int) (store.Runner, error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" {
		return store.Runner{}, invalid("runner id is required")
	}
	if name == "" {
		return store.Runner{}, invalid("runner name is required")
	}
	if maxActiveSessions != nil && *maxActiveSessions < 1 {
		return store.Runner{}, invalid("maxActiveSessions must be at least 1")
	}
	value, err := s.store.UpdateRunner(ctx, id, name, maxActiveSessions)
	return value, translateStoreError(err, "runner")
}

func (s *RunnerService) UpdateForProject(ctx context.Context, projectID, id, name string, maxActiveSessions *int) (store.Runner, error) {
	if _, err := s.projectOwnedRunner(ctx, projectID, id); err != nil {
		return store.Runner{}, err
	}
	return s.Update(ctx, id, name, maxActiveSessions)
}

func (s *RunnerService) Rotate(ctx context.Context, id string) (store.Runner, string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Runner{}, "", invalid("runner id is required")
	}
	token, hash, err := runnerCredential()
	if err != nil {
		return store.Runner{}, "", err
	}
	value, err := s.store.RotateRunnerCredential(ctx, id, hash)
	if err != nil {
		return store.Runner{}, "", translateStoreError(err, "runner")
	}
	return value, token, nil
}

func (s *RunnerService) RotateForProject(ctx context.Context, projectID, id string) (store.Runner, string, error) {
	if _, err := s.projectOwnedRunner(ctx, projectID, id); err != nil {
		return store.Runner{}, "", err
	}
	return s.Rotate(ctx, id)
}

func (s *RunnerService) Revoke(ctx context.Context, id string, deleted bool) (store.Runner, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Runner{}, invalid("runner id is required")
	}
	value, err := s.store.RevokeRunner(ctx, id, deleted)
	if err != nil {
		return store.Runner{}, translateStoreError(err, "runner")
	}
	if s.sessions != nil {
		if err := s.sessions.TerminateRunnerSessions(ctx, id); err != nil {
			return store.Runner{}, err
		}
	}
	return value, nil
}

func (s *RunnerService) RevokeForProject(ctx context.Context, projectID, id string, deleted bool) (store.Runner, error) {
	if _, err := s.projectOwnedRunner(ctx, projectID, id); err != nil {
		return store.Runner{}, err
	}
	return s.Revoke(ctx, id, deleted)
}

func (s *RunnerService) CountReservations(ctx context.Context, ids []string) (map[string]int, error) {
	counts, err := s.store.CountRunnerReservations(ctx, ids)
	return counts, translateStoreError(err, "runner")
}

func (s *RunnerService) projectOwnedRunner(ctx context.Context, projectID, id string) (store.Runner, error) {
	projectID = strings.TrimSpace(projectID)
	id = strings.TrimSpace(id)
	if projectID == "" || id == "" {
		return store.Runner{}, invalid("projectId and runner id are required")
	}
	value, err := s.Get(ctx, id)
	if err != nil {
		return store.Runner{}, err
	}
	if value.ProjectID == nil || *value.ProjectID != projectID {
		return store.Runner{}, notFound("runner")
	}
	return value, nil
}
