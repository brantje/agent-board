package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const maxCandidatePatchBytes = 64 << 20

type CandidateBlobSource func(context.Context) (io.ReadCloser, error)

type CandidateFileSource struct {
	Path   string
	Chunks []CandidateBlobSource
}

type AcceptedCandidate struct {
	StagedPatch   CandidateBlobSource
	UnstagedPatch CandidateBlobSource
	Files         []CandidateFileSource
}

type candidateGit interface {
	findAcceptedReview(context.Context, string, string) (string, bool, error)
	resetAcceptedCheckout(context.Context, string) error
	applyCandidatePatch(context.Context, string, []byte, bool) error
	commitAcceptedCandidate(context.Context, string, string) (string, error)
}

type CandidateApplier struct {
	locks    ProjectWorkspaceLockStore
	projects ProjectWorkspaceSource
	git      candidateGit
}

func NewCandidateApplier(locks ProjectWorkspaceLockStore, projects ProjectWorkspaceSource, git candidateGit) (*CandidateApplier, error) {
	if locks == nil || projects == nil || git == nil {
		return nil, fmt.Errorf("candidate applier dependencies: %w", ErrInvalidMetadata)
	}
	return &CandidateApplier{locks: locks, projects: projects, git: git}, nil
}

func (a *CandidateApplier) Apply(ctx context.Context, project store.Project, reviewID string, candidate AcceptedCandidate) (revision string, err error) {
	if strings.TrimSpace(reviewID) == "" {
		return "", ErrInvalidMetadata
	}
	accepted, err := a.projects.EnsureProjectWorkspace(ctx, project)
	if err != nil {
		return "", err
	}
	lock, err := a.locks.AcquireWorkspaceBootstrapLock(ctx, "project:"+project.ID)
	if err != nil {
		return "", fmt.Errorf("acquire Project Workspace approval lock: %w", err)
	}
	defer func() {
		if releaseErr := lock.Release(); err == nil && releaseErr != nil {
			revision = ""
			err = fmt.Errorf("release Project Workspace approval lock: %w", releaseErr)
		}
	}()

	if commit, found, err := a.git.findAcceptedReview(ctx, accepted.Path, reviewID); err != nil {
		return "", err
	} else if found {
		return commit, nil
	}
	if err := a.git.resetAcceptedCheckout(ctx, accepted.Path); err != nil {
		return "", fmt.Errorf("reset Project Workspace before approval: %w", err)
	}
	rollback := true
	defer func() {
		if rollback {
			_ = a.git.resetAcceptedCheckout(context.WithoutCancel(ctx), accepted.Path)
		}
	}()

	staged, err := readCandidateBlob(ctx, candidate.StagedPatch)
	if err != nil {
		return "", err
	}
	if err := a.git.applyCandidatePatch(ctx, accepted.Path, staged, true); err != nil {
		return "", err
	}
	unstaged, err := readCandidateBlob(ctx, candidate.UnstagedPatch)
	if err != nil {
		return "", err
	}
	if err := a.git.applyCandidatePatch(ctx, accepted.Path, unstaged, false); err != nil {
		return "", err
	}
	if err := writeCandidateFiles(ctx, accepted.Path, candidate.Files); err != nil {
		return "", err
	}
	revision, err = a.git.commitAcceptedCandidate(ctx, accepted.Path, reviewID)
	if err != nil {
		return "", fmt.Errorf("commit accepted candidate: %w", err)
	}
	rollback = false
	return revision, nil
}

func readCandidateBlob(ctx context.Context, source CandidateBlobSource) ([]byte, error) {
	if source == nil {
		return nil, nil
	}
	reader, err := source(ctx)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxCandidatePatchBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCandidatePatchBytes {
		return nil, fmt.Errorf("candidate patch exceeds %d bytes", maxCandidatePatchBytes)
	}
	return data, nil
}

func writeCandidateFiles(ctx context.Context, root string, files []CandidateFileSource) error {
	ordered := append([]CandidateFileSource(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	for _, source := range ordered {
		path, err := acceptedCandidatePath(root, source.Path)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("candidate file %q conflicts with an existing Project Workspace path", source.Path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect accepted candidate %q: %w", source.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return fmt.Errorf("create accepted candidate parent: %w", err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("create accepted candidate %q: %w", source.Path, err)
		}
		writeErr := copyCandidateChunks(ctx, file, source.Chunks)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return fmt.Errorf("close accepted candidate %q: %w", source.Path, closeErr)
		}
	}
	return nil
}

func copyCandidateChunks(ctx context.Context, destination io.Writer, chunks []CandidateBlobSource) error {
	buffer := make([]byte, 64<<10)
	for _, source := range chunks {
		if source == nil {
			return fmt.Errorf("candidate file chunk source is required")
		}
		reader, err := source(ctx)
		if err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				_ = reader.Close()
				return err
			}
			n, readErr := reader.Read(buffer)
			if n > 0 {
				if _, err := destination.Write(buffer[:n]); err != nil {
					_ = reader.Close()
					return err
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				_ = reader.Close()
				return readErr
			}
		}
		if err := reader.Close(); err != nil {
			return err
		}
	}
	return nil
}

func acceptedCandidatePath(root, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("candidate path %q: %w", relative, ErrInvalidMetadata)
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("candidate path %q escapes Project Workspace: %w", relative, ErrInvalidMetadata)
	}
	path := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("candidate path %q escapes Project Workspace: %w", relative, ErrInvalidMetadata)
	}

	current := root
	parts := strings.Split(clean, string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			break
		}
		if statErr != nil {
			return "", fmt.Errorf("inspect candidate path %q: %w", relative, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("candidate path %q traverses a symbolic link: %w", relative, ErrInvalidMetadata)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("candidate path %q has a non-directory parent: %w", relative, ErrInvalidMetadata)
		}
	}
	return path, nil
}
