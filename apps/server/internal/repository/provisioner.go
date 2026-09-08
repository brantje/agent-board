package repository

import (
	"context"
	"fmt"
	"strings"
)

// GitInitializer initializes a local repository checkout for Project provisioning.
type GitInitializer interface {
	ValidateBranch(context.Context, string) error
	InitRepository(context.Context, string, string) error
}

// ProjectRepositoryProvisioner ensures a configured Project repository path exists,
// is writable inside authorized roots, and is initialized as a Git repository.
type ProjectRepositoryProvisioner interface {
	EnsureProjectRepository(context.Context, string, string) (string, error)
}

type Provisioner struct {
	policy *Policy
	git    GitInitializer
}

func NewProvisioner(policy *Policy, git GitInitializer) (*Provisioner, error) {
	if policy == nil || git == nil {
		return nil, fmt.Errorf("repository provisioner dependencies are required")
	}
	return &Provisioner{policy: policy, git: git}, nil
}

func (p *Provisioner) EnsureProjectRepository(ctx context.Context, repositoryPath, defaultBranch string) (string, error) {
	defaultBranch = strings.TrimSpace(defaultBranch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	canonical, err := p.policy.Ensure(repositoryPath)
	if err != nil {
		return "", err
	}
	if err := p.git.ValidateBranch(ctx, defaultBranch); err != nil {
		return "", fmt.Errorf("validate default branch: %w", err)
	}
	if err := p.git.InitRepository(ctx, canonical, defaultBranch); err != nil {
		return "", fmt.Errorf("initialize repository: %w", err)
	}
	return canonical, nil
}
