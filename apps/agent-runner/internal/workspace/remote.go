package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	sharedworkspace "github.com/brantje/agent-board/packages/workspacegit"
)

const remoteIssueBranchPrefix = "agent-board/"

type RemoteCheckout struct {
	CachePath    string
	WorktreePath string
	Branch       string
	StartRevision string
}

// RemoteRepositoryManager owns only the Runner-side Git cache/worktree mechanics
// needed by remote Project execution. Durable Issue ownership remains a server
// concern; callers provide the recorded revision that must match remote truth.
type RemoteRepositoryManager struct {
	root string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewRemoteRepositoryManager(workspaceRoot string) *RemoteRepositoryManager {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = "/workspace"
	}
	return &RemoteRepositoryManager{
		root:  filepath.Join(workspaceRoot, ".agent-board", "git-cache"),
		locks: make(map[string]*sync.Mutex),
	}
}

func (m *RemoteRepositoryManager) Prepare(ctx context.Context, cloneURL, sourceRef, issueBranch, recordedRevision, worktreePath string) (RemoteCheckout, error) {
	cloneURL = strings.TrimSpace(cloneURL)
	issueBranch = strings.TrimSpace(issueBranch)
	recordedRevision = strings.TrimSpace(recordedRevision)
	worktreePath = strings.TrimSpace(worktreePath)
	if cloneURL == "" || worktreePath == "" {
		return RemoteCheckout{}, fmt.Errorf("remote repository URL and worktree path are required")
	}
	if !strings.HasPrefix(issueBranch, remoteIssueBranchPrefix) {
		return RemoteCheckout{}, fmt.Errorf("remote Issue branch must use %q namespace", remoteIssueBranchPrefix)
	}
	if _, err := runGit(ctx, "check-ref-format", "--branch", issueBranch); err != nil {
		return RemoteCheckout{}, fmt.Errorf("invalid remote Issue branch %q: %w", issueBranch, err)
	}

	key := remoteRepositoryKey(cloneURL)
	lock := m.repositoryLock(key)
	lock.Lock()
	defer lock.Unlock()

	cachePath := filepath.Join(m.root, key+".git")
	if err := m.ensureCache(ctx, cachePath, cloneURL); err != nil {
		return RemoteCheckout{}, err
	}
	if err := fetchRemote(ctx, cachePath); err != nil {
		return RemoteCheckout{}, err
	}
	if err := refreshOriginHead(ctx, cachePath); err != nil {
		return RemoteCheckout{}, err
	}
	if _, err := os.Stat(worktreePath); err == nil {
		return RemoteCheckout{}, fmt.Errorf("remote Issue worktree already exists at %s", worktreePath)
	} else if !os.IsNotExist(err) {
		return RemoteCheckout{}, fmt.Errorf("inspect remote Issue worktree: %w", err)
	}
	if _, err := runGit(ctx, "-C", cachePath, "worktree", "prune"); err != nil {
		return RemoteCheckout{}, fmt.Errorf("prune stale remote worktrees: %w", err)
	}

	issueRef := "refs/heads/" + issueBranch
	remoteIssueRef := "refs/remotes/origin/" + issueBranch
	remoteRevision, remoteExists := resolveCommit(ctx, cachePath, remoteIssueRef)
	if remoteExists {
		if recordedRevision == "" {
			return RemoteCheckout{}, fmt.Errorf("remote Issue branch already exists but Agent Board has no recorded revision")
		}
		if remoteRevision != recordedRevision {
			return RemoteCheckout{}, fmt.Errorf("remote Issue branch advanced unexpectedly: recorded %s, remote %s", recordedRevision, remoteRevision)
		}
		if _, err := runGit(ctx, "-C", cachePath, "update-ref", issueRef, remoteRevision); err != nil {
			return RemoteCheckout{}, fmt.Errorf("synchronize local Issue branch: %w", err)
		}
	} else {
		if recordedRevision != "" {
			return RemoteCheckout{}, fmt.Errorf("recorded remote Issue branch revision %s is missing from origin", recordedRevision)
		}
		if localRevision, localExists := resolveCommit(ctx, cachePath, issueRef); localExists {
			return RemoteCheckout{}, fmt.Errorf("unpublished local Issue branch already exists at %s", localRevision)
		}
		targetRevision, err := resolveSourceRevision(ctx, cachePath, sourceRef)
		if err != nil {
			return RemoteCheckout{}, err
		}
		if _, err := runGit(ctx, "-C", cachePath, "update-ref", issueRef, targetRevision); err != nil {
			return RemoteCheckout{}, fmt.Errorf("create remote Issue branch: %w", err)
		}
		remoteRevision = targetRevision
	}

	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return RemoteCheckout{}, fmt.Errorf("prepare remote Issue worktree parent: %w", err)
	}
	if _, err := runGit(ctx, "-C", cachePath, "worktree", "add", "--no-checkout", worktreePath, issueBranch); err != nil {
		return RemoteCheckout{}, fmt.Errorf("create remote Issue worktree: %w", err)
	}
	if _, err := runGit(ctx, "-C", worktreePath, "checkout", "-q", issueBranch); err != nil {
		return RemoteCheckout{}, fmt.Errorf("checkout remote Issue branch: %w", err)
	}
	actualRevision, err := sharedworkspace.HeadRevision(ctx, worktreePath, "git", commandTimeout)
	if err != nil {
		return RemoteCheckout{}, err
	}
	if actualRevision != remoteRevision {
		return RemoteCheckout{}, fmt.Errorf("remote Issue worktree started at %s, expected %s", actualRevision, remoteRevision)
	}
	return RemoteCheckout{CachePath: cachePath, WorktreePath: worktreePath, Branch: issueBranch, StartRevision: actualRevision}, nil
}

func (m *RemoteRepositoryManager) Publish(ctx context.Context, cloneURL string, checkout RemoteCheckout) (string, error) {
	cloneURL = strings.TrimSpace(cloneURL)
	if cloneURL == "" || strings.TrimSpace(checkout.CachePath) == "" || strings.TrimSpace(checkout.WorktreePath) == "" || strings.TrimSpace(checkout.Branch) == "" || strings.TrimSpace(checkout.StartRevision) == "" {
		return "", fmt.Errorf("complete remote checkout identity is required")
	}
	key := remoteRepositoryKey(cloneURL)
	if filepath.Clean(checkout.CachePath) != filepath.Join(m.root, key+".git") {
		return "", fmt.Errorf("remote checkout does not belong to repository cache")
	}
	lock := m.repositoryLock(key)
	lock.Lock()
	defer lock.Unlock()

	head, err := FinalizeCheckout(ctx, checkout.WorktreePath, CheckoutState{Branch: checkout.Branch, StartRevision: checkout.StartRevision})
	if err != nil {
		return "", err
	}
	ref := "refs/heads/" + checkout.Branch
	if _, err := runGit(ctx, "-C", checkout.WorktreePath, "push", "origin", ref+":"+ref); err != nil {
		return "", fmt.Errorf("publish remote Issue branch without force: %w", err)
	}
	return head, nil
}

func (m *RemoteRepositoryManager) repositoryLock(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock := m.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		m.locks[key] = lock
	}
	return lock
}

func (m *RemoteRepositoryManager) ensureCache(ctx context.Context, cachePath, cloneURL string) error {
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return fmt.Errorf("prepare remote Git cache root: %w", err)
	}
	if _, err := os.Stat(cachePath); err == nil {
		bare, bareErr := runGit(ctx, "-C", cachePath, "rev-parse", "--is-bare-repository")
		if bareErr != nil || strings.TrimSpace(bare) != "true" {
			return fmt.Errorf("remote Git cache is not a bare repository")
		}
		origin, originErr := runGit(ctx, "-C", cachePath, "remote", "get-url", "origin")
		if originErr != nil || strings.TrimSpace(origin) != cloneURL {
			return fmt.Errorf("remote Git cache origin does not match Project clone URL")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect remote Git cache: %w", err)
	}

	if _, err := runGit(ctx, "init", "--bare", "-q", cachePath); err != nil {
		return fmt.Errorf("initialize bare remote Git cache: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(cachePath)
		}
	}()
	if _, err := runGit(ctx, "-C", cachePath, "remote", "add", "origin", cloneURL); err != nil {
		return fmt.Errorf("configure remote Git cache origin: %w", err)
	}
	if _, err := runGit(ctx, "-C", cachePath, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return fmt.Errorf("configure remote Git cache fetch refspec: %w", err)
	}
	cleanup = false
	return nil
}

func fetchRemote(ctx context.Context, cachePath string) error {
	if _, err := runGit(ctx, "-C", cachePath, "fetch", "--prune", "origin", "+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*"); err != nil {
		return fmt.Errorf("fetch remote Project source: %w", err)
	}
	return nil
}

func refreshOriginHead(ctx context.Context, cachePath string) error {
	if _, err := runGit(ctx, "-C", cachePath, "remote", "set-head", "origin", "--auto"); err != nil {
		return fmt.Errorf("refresh origin/HEAD: %w", err)
	}
	return nil
}

func resolveSourceRevision(ctx context.Context, cachePath, sourceRef string) (string, error) {
	sourceRef = strings.TrimSpace(sourceRef)
	if sourceRef == "" {
		if revision, ok := resolveCommit(ctx, cachePath, "refs/remotes/origin/HEAD"); ok {
			return revision, nil
		}
		return "", fmt.Errorf("remote default branch could not be resolved")
	}
	candidates := []string{}
	switch {
	case strings.HasPrefix(sourceRef, "refs/heads/"):
		candidates = append(candidates, "refs/remotes/origin/"+strings.TrimPrefix(sourceRef, "refs/heads/"))
	case strings.HasPrefix(sourceRef, "refs/tags/") || strings.HasPrefix(sourceRef, "refs/remotes/origin/"):
		candidates = append(candidates, sourceRef)
	default:
		candidates = append(candidates, "refs/remotes/origin/"+sourceRef, "refs/tags/"+sourceRef, sourceRef)
	}
	for _, candidate := range candidates {
		if revision, ok := resolveCommit(ctx, cachePath, candidate); ok {
			return revision, nil
		}
	}
	return "", fmt.Errorf("configured remote source ref %q could not be resolved", sourceRef)
}

func resolveCommit(ctx context.Context, repositoryPath, ref string) (string, bool) {
	value, err := runGit(ctx, "-C", repositoryPath, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func remoteRepositoryKey(cloneURL string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(cloneURL)))
	return hex.EncodeToString(digest[:])
}
