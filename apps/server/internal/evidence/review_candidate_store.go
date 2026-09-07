package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	reviewCandidateManifestVersion = 1
	reviewCandidatePatchMaxBytes   = 64 << 20
	reviewCandidateFileMaxBytes    = int64(maxCandidateFileChunks) * (64 << 20)
)

var ErrReviewCandidateNotFound = errors.New("review candidate archive not found")

type ReviewCandidateBlobSource func(context.Context) (io.ReadCloser, error)

type ReviewCandidateFile struct {
	Path       string
	Executable bool
	Source     ReviewCandidateBlobSource
}

type ReviewCandidateSnapshot struct {
	Candidate     Candidate
	StagedPatch   ReviewCandidateBlobSource
	UnstagedPatch ReviewCandidateBlobSource
	Files         []ReviewCandidateFile
}

type ReviewCandidateWriter interface {
	Capture(context.Context, string, string, Candidate) error
}

type ReviewCandidateReader interface {
	Open(context.Context, string) (ReviewCandidateSnapshot, error)
}

type ReviewCandidateArchive interface {
	ReviewCandidateWriter
	ReviewCandidateReader
}

type ReviewCandidateStore struct {
	root string
}

type reviewCandidateManifest struct {
	Version           int                           `json:"version"`
	Candidate         Candidate                     `json:"candidate"`
	StagedPatch       string                        `json:"stagedPatch,omitempty"`
	StagedPatchSize   int64                         `json:"stagedPatchSize,omitempty"`
	UnstagedPatch     string                        `json:"unstagedPatch,omitempty"`
	UnstagedPatchSize int64                         `json:"unstagedPatchSize,omitempty"`
	Files             []reviewCandidateManifestFile `json:"files,omitempty"`
}

type reviewCandidateManifestFile struct {
	Path       string `json:"path"`
	Executable bool   `json:"executable,omitempty"`
	Storage    string `json:"storage"`
	SizeBytes  int64  `json:"sizeBytes"`
}

func NewReviewCandidateStore(root string) (*ReviewCandidateStore, error) {
	root = strings.TrimSpace(root)
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("review candidate root must be absolute")
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create review candidate root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure review candidate root: %w", err)
	}
	return &ReviewCandidateStore{root: root}, nil
}

// Capture persists the exact, unredacted candidate used for trusted approval.
// It is deliberately separate from public Artifacts, which remain redacted.
func (s *ReviewCandidateStore) Capture(ctx context.Context, runID, workspace string, candidate Candidate) error {
	if s == nil {
		return fmt.Errorf("review candidate store is required")
	}
	finalPath, err := s.runPath(runID)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(finalPath); statErr == nil {
		if _, err := s.Open(ctx, runID); err != nil {
			return fmt.Errorf("validate existing review candidate archive: %w", err)
		}
		return nil
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect review candidate archive: %w", statErr)
	}
	if err := cleanupReviewCandidateTemps(s.root, runID); err != nil {
		return fmt.Errorf("cleanup review candidate archive: %w", err)
	}
	temporary, err := os.MkdirTemp(s.root, "."+runID+".candidate-")
	if err != nil {
		return fmt.Errorf("create review candidate archive: %w", err)
	}
	if err := os.Chmod(temporary, 0o700); err != nil {
		_ = os.RemoveAll(temporary)
		return fmt.Errorf("secure review candidate archive: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(temporary)
		}
	}()

	manifest := reviewCandidateManifest{Version: reviewCandidateManifestVersion, Candidate: candidate}
	staged, err := gitOutput(ctx, workspace, "diff", "--binary", "--cached", "HEAD")
	if err != nil {
		return err
	}
	if len(staged) > reviewCandidatePatchMaxBytes {
		return fmt.Errorf("review candidate staged patch exceeds %d bytes", reviewCandidatePatchMaxBytes)
	}
	if len(staged) > 0 {
		manifest.StagedPatch = "staged.patch"
		manifest.StagedPatchSize = int64(len(staged))
		if err := writePrivateCandidateFile(filepath.Join(temporary, manifest.StagedPatch), staged); err != nil {
			return err
		}
	}
	unstaged, err := gitOutput(ctx, workspace, "diff", "--binary")
	if err != nil {
		return err
	}
	if len(unstaged) > reviewCandidatePatchMaxBytes {
		return fmt.Errorf("review candidate unstaged patch exceeds %d bytes", reviewCandidatePatchMaxBytes)
	}
	if len(unstaged) > 0 {
		manifest.UnstagedPatch = "unstaged.patch"
		manifest.UnstagedPatchSize = int64(len(unstaged))
		if err := writePrivateCandidateFile(filepath.Join(temporary, manifest.UnstagedPatch), unstaged); err != nil {
			return err
		}
	}

	files := make([]CandidateChange, 0)
	for _, change := range candidate.Changes {
		if change.Untracked {
			files = append(files, change)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if len(files) > 0 {
		if err := os.Mkdir(filepath.Join(temporary, "files"), 0o700); err != nil {
			return fmt.Errorf("create review candidate file archive: %w", err)
		}
	}
	for index, change := range files {
		file, info, err := openCandidateRegularFile(workspace, change.Path)
		if err != nil {
			return err
		}
		storage := filepath.ToSlash(filepath.Join("files", fmt.Sprintf("%06d", index)))
		size, copyErr := copyPrivateCandidate(ctx, filepath.Join(temporary, filepath.FromSlash(storage)), file)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("archive review candidate %q: %w", change.Path, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close review candidate %q: %w", change.Path, closeErr)
		}
		manifest.Files = append(manifest.Files, reviewCandidateManifestFile{
			Path:       change.Path,
			Executable: info.Mode().Perm()&0o111 != 0,
			Storage:    storage,
			SizeBytes:  size,
		})
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode review candidate manifest: %w", err)
	}
	if err := writePrivateCandidateFile(filepath.Join(temporary, "manifest.json"), manifestData); err != nil {
		return err
	}
	if err := os.Rename(temporary, finalPath); err != nil {
		if _, statErr := os.Stat(finalPath); statErr == nil {
			if _, openErr := s.Open(ctx, runID); openErr == nil {
				return nil
			}
		}
		return fmt.Errorf("publish review candidate archive: %w", err)
	}
	published = true
	return nil
}

func (s *ReviewCandidateStore) Open(ctx context.Context, runID string) (ReviewCandidateSnapshot, error) {
	if s == nil {
		return ReviewCandidateSnapshot{}, fmt.Errorf("review candidate store is required")
	}
	if err := ctx.Err(); err != nil {
		return ReviewCandidateSnapshot{}, err
	}
	root, err := s.runPath(runID)
	if err != nil {
		return ReviewCandidateSnapshot{}, err
	}
	manifestData, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if os.IsNotExist(err) {
		return ReviewCandidateSnapshot{}, ErrReviewCandidateNotFound
	}
	if err != nil {
		return ReviewCandidateSnapshot{}, fmt.Errorf("read review candidate manifest: %w", err)
	}
	var manifest reviewCandidateManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return ReviewCandidateSnapshot{}, fmt.Errorf("decode review candidate manifest: %w", err)
	}
	if manifest.Version != reviewCandidateManifestVersion {
		return ReviewCandidateSnapshot{}, fmt.Errorf("unsupported review candidate manifest version %d", manifest.Version)
	}
	if err := validateReviewCandidateManifest(manifest); err != nil {
		return ReviewCandidateSnapshot{}, err
	}

	snapshot := ReviewCandidateSnapshot{Candidate: manifest.Candidate}
	if manifest.StagedPatch != "" {
		source, err := reviewCandidateSource(root, manifest.StagedPatch, manifest.StagedPatchSize)
		if err != nil {
			return ReviewCandidateSnapshot{}, err
		}
		snapshot.StagedPatch = source
	}
	if manifest.UnstagedPatch != "" {
		source, err := reviewCandidateSource(root, manifest.UnstagedPatch, manifest.UnstagedPatchSize)
		if err != nil {
			return ReviewCandidateSnapshot{}, err
		}
		snapshot.UnstagedPatch = source
	}
	for _, file := range manifest.Files {
		source, err := reviewCandidateSource(root, file.Storage, file.SizeBytes)
		if err != nil {
			return ReviewCandidateSnapshot{}, err
		}
		snapshot.Files = append(snapshot.Files, ReviewCandidateFile{Path: file.Path, Executable: file.Executable, Source: source})
	}
	return snapshot, nil
}

func validateReviewCandidateManifest(manifest reviewCandidateManifest) error {
	if manifest.StagedPatch == "" && manifest.StagedPatchSize != 0 {
		return fmt.Errorf("review candidate staged patch metadata is inconsistent")
	}
	if manifest.UnstagedPatch == "" && manifest.UnstagedPatchSize != 0 {
		return fmt.Errorf("review candidate unstaged patch metadata is inconsistent")
	}
	if manifest.StagedPatchSize < 0 || manifest.UnstagedPatchSize < 0 {
		return fmt.Errorf("review candidate patch size is invalid")
	}

	untracked := make(map[string]struct{})
	seenChanges := make(map[string]struct{}, len(manifest.Candidate.Changes))
	for _, change := range manifest.Candidate.Changes {
		path := strings.TrimSpace(change.Path)
		if path == "" {
			return fmt.Errorf("review candidate change path is missing")
		}
		if _, exists := seenChanges[path]; exists {
			return fmt.Errorf("duplicate review candidate change path %q", path)
		}
		seenChanges[path] = struct{}{}
		if change.Untracked {
			untracked[path] = struct{}{}
		}
	}

	seenFiles := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		if strings.TrimSpace(file.Path) == "" || strings.TrimSpace(file.Storage) == "" || file.SizeBytes < 0 {
			return fmt.Errorf("invalid review candidate file metadata")
		}
		if _, exists := seenFiles[file.Path]; exists {
			return fmt.Errorf("duplicate review candidate path %q", file.Path)
		}
		if _, ok := untracked[file.Path]; !ok {
			return fmt.Errorf("review candidate file %q is not marked untracked", file.Path)
		}
		seenFiles[file.Path] = struct{}{}
	}
	if len(seenFiles) != len(untracked) {
		return fmt.Errorf("review candidate untracked file set is incomplete")
	}
	return nil
}

func (s *ReviewCandidateStore) runPath(runID string) (string, error) {
	if strings.TrimSpace(runID) == "" || runID != filepath.Base(runID) || strings.ContainsAny(runID, `/\\`) {
		return "", fmt.Errorf("invalid review candidate run id %q", runID)
	}
	path := filepath.Join(s.root, runID)
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("review candidate path escapes root")
	}
	return path, nil
}

func writePrivateCandidateFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write review candidate archive: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure review candidate archive: %w", err)
	}
	return nil
}

func copyPrivateCandidate(ctx context.Context, path string, source io.Reader) (int64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	written, copyErr := copyContext(ctx, file, io.LimitReader(source, reviewCandidateFileMaxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return written, copyErr
	}
	if closeErr != nil {
		return written, closeErr
	}
	if written > reviewCandidateFileMaxBytes {
		return written, fmt.Errorf("candidate file exceeds trusted archive limit")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return written, err
	}
	return written, nil
}

func reviewCandidateSource(root, relative string, expectedSize int64) (ReviewCandidateBlobSource, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("review candidate storage path escapes archive")
	}
	path := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("review candidate storage path escapes archive")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect review candidate storage: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("review candidate storage is not a regular file")
	}
	if expectedSize >= 0 && info.Size() != expectedSize {
		return nil, fmt.Errorf("review candidate storage size changed")
	}
	return func(ctx context.Context) (io.ReadCloser, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open review candidate storage: %w", err)
		}
		return file, nil
	}, nil
}

func cleanupReviewCandidateTemps(root, runID string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	prefix := "." + runID + ".candidate-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
