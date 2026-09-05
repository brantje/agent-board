package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrNoAuthorizedRoots = errors.New("repository: no authorized roots configured")
	ErrPathNotAbsolute   = errors.New("repository: path must be absolute")
	ErrPathNotAuthorized = errors.New("repository: path is outside authorized roots")
	ErrPathUnavailable   = errors.New("repository: path is unavailable")
	ErrPathNotDirectory  = errors.New("repository: path is not a directory")
)

// Policy validates backend-visible local repository paths against a deployment
// allowlist. Canonicalization is repeated for every Resolve call so symlink
// traversal cannot turn Project configuration into arbitrary filesystem access.
type Policy struct {
	roots []string
}

func NewPolicy(roots []string) (*Policy, error) {
	cleaned := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("authorized root %q: %w", root, ErrPathNotAbsolute)
		}
		canonical, err := filepath.EvalSymlinks(filepath.Clean(root))
		if err != nil {
			return nil, fmt.Errorf("resolve authorized root %q: %w: %v", root, ErrPathUnavailable, err)
		}
		info, err := os.Stat(canonical)
		if err != nil {
			return nil, fmt.Errorf("stat authorized root %q: %w: %v", canonical, ErrPathUnavailable, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("authorized root %q: %w", canonical, ErrPathNotDirectory)
		}
		cleaned = append(cleaned, canonical)
	}
	return &Policy{roots: cleaned}, nil
}

func ParseRoots(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := filepath.SplitList(value)
	roots := make([]string, 0, len(parts))
	for _, part := range parts {
		if root := strings.TrimSpace(part); root != "" {
			roots = append(roots, root)
		}
	}
	return roots
}

func (p *Policy) Resolve(path string) (string, error) {
	if p == nil || len(p.roots) == 0 {
		return "", ErrNoAuthorizedRoots
	}
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("repository path %q: %w", path, ErrPathNotAbsolute)
	}

	canonical, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w: %v", ErrPathUnavailable, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("stat repository path: %w: %v", ErrPathUnavailable, err)
	}
	if !info.IsDir() {
		return "", ErrPathNotDirectory
	}

	for _, configuredRoot := range p.roots {
		root, err := filepath.EvalSymlinks(configuredRoot)
		if err != nil {
			continue
		}
		rootInfo, err := os.Stat(root)
		if err != nil || !rootInfo.IsDir() {
			continue
		}
		if within(root, canonical) {
			return canonical, nil
		}
	}
	return "", fmt.Errorf("repository path %q: %w", canonical, ErrPathNotAuthorized)
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
