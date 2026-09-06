package session

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

const DefaultWorkspaceRoot = "/workspace"

var (
	ErrCapacityReached = errors.New("runner session capacity reached")
	ErrDuplicateID     = errors.New("execution session id already exists")
	ErrSessionNotFound = errors.New("execution session not found")
)

type Manager struct {
	mu               sync.Mutex
	capacity         int
	workspaceRoot    string
	sessions         map[string]*Session
	workspaceSecrets map[string]struct{}
}

func NewManager(capacity int) *Manager {
	return NewManagerWithWorkspace(capacity, DefaultWorkspaceRoot)
}

func NewManagerWithWorkspace(capacity int, workspaceRoot string) *Manager {
	if capacity < 1 {
		capacity = 1
	}
	if workspaceRoot == "" {
		workspaceRoot = DefaultWorkspaceRoot
	}
	return &Manager{
		capacity:         capacity,
		workspaceRoot:    workspaceRoot,
		sessions:         make(map[string]*Session),
		workspaceSecrets: make(map[string]struct{}),
	}
}

func (m *Manager) Capacity() int {
	return m.capacity
}

func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func (m *Manager) ActiveIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (m *Manager) Start(id string, request Request) (*Session, error) {
	if id == "" {
		return nil, errors.New("execution session id is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; exists {
		return nil, fmt.Errorf("%w: %s", ErrDuplicateID, id)
	}
	if len(m.sessions) >= m.capacity {
		return nil, ErrCapacityReached
	}

	currentSecrets := secretValues(request.Secrets)
	redactionValues := make([]string, 0, len(m.workspaceSecrets)+len(currentSecrets))
	for value := range m.workspaceSecrets {
		redactionValues = append(redactionValues, value)
	}
	for _, value := range currentSecrets {
		if _, known := m.workspaceSecrets[value]; !known {
			redactionValues = append(redactionValues, value)
		}
	}

	s, err := start(id, m.workspaceRoot, request, redactionValues)
	if err != nil {
		return nil, err
	}
	for _, value := range currentSecrets {
		m.workspaceSecrets[value] = struct{}{}
	}
	m.sessions[id] = s
	go func() {
		<-s.Done()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
	}()
	return s, nil
}

func (m *Manager) Get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	return s, nil
}
