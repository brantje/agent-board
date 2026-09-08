package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

type CandidateChange struct {
	Path           string `json:"path"`
	OldPath        string `json:"oldPath,omitempty"`
	StagedStatus   string `json:"stagedStatus,omitempty"`
	UnstagedStatus string `json:"unstagedStatus,omitempty"`
	Untracked      bool   `json:"untracked,omitempty"`
}

type Candidate struct {
	Changes []CandidateChange `json:"changes"`
}

type CandidateCollector struct{}

func NewCandidateCollector() *CandidateCollector { return &CandidateCollector{} }

func (c *CandidateCollector) Collect(ctx context.Context, workspace string) (Candidate, error) {
	workspace, err := canonicalWorkspace(workspace)
	if err != nil {
		return Candidate{}, err
	}
	staged, err := gitNameStatus(ctx, workspace, true)
	if err != nil {
		return Candidate{}, err
	}
	unstaged, err := gitNameStatus(ctx, workspace, false)
	if err != nil {
		return Candidate{}, err
	}
	untracked, err := gitUntracked(ctx, workspace)
	if err != nil {
		return Candidate{}, err
	}

	changes := make(map[string]CandidateChange)
	merge := func(items []nameStatus, staged bool) {
		for _, item := range items {
			change := changes[item.path]
			change.Path = item.path
			if item.oldPath != "" {
				change.OldPath = item.oldPath
			}
			if staged {
				change.StagedStatus = item.status
			} else {
				change.UnstagedStatus = item.status
			}
			changes[item.path] = change
		}
	}
	merge(staged, true)
	merge(unstaged, false)
	for _, path := range untracked {
		change := changes[path]
		change.Path = path
		change.Untracked = true
		changes[path] = change
	}

	candidate := Candidate{Changes: make([]CandidateChange, 0, len(changes))}
	for _, change := range changes {
		candidate.Changes = append(candidate.Changes, change)
	}
	sort.Slice(candidate.Changes, func(i, j int) bool { return candidate.Changes[i].Path < candidate.Changes[j].Path })
	return candidate, nil
}

type nameStatus struct {
	status  string
	path    string
	oldPath string
}

func gitNameStatus(ctx context.Context, workspace string, staged bool) ([]nameStatus, error) {
	args := []string{"diff", "--name-status", "-z", "--find-renames"}
	if staged {
		args = append(args, "--cached", "HEAD")
	}
	out, err := gitOutput(ctx, workspace, args...)
	if err != nil {
		return nil, err
	}
	parts := splitNUL(out)
	items := make([]nameStatus, 0)
	for i := 0; i < len(parts); {
		status := parts[i]
		i++
		if status == "" || i >= len(parts) {
			break
		}
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if i+1 >= len(parts) {
				return nil, fmt.Errorf("evidence: malformed git rename status")
			}
			oldPath := parts[i]
			path := parts[i+1]
			i += 2
			items = append(items, nameStatus{status: normalizeStatus(status), path: path, oldPath: oldPath})
			continue
		}
		path := parts[i]
		i++
		items = append(items, nameStatus{status: normalizeStatus(status), path: path})
	}
	return items, nil
}

func gitUntracked(ctx context.Context, workspace string) ([]string, error) {
	out, err := gitOutput(ctx, workspace, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	return splitNUL(out), nil
}

func normalizeStatus(status string) string {
	if status == "" {
		return status
	}
	switch status[0] {
	case 'A':
		return "created"
	case 'M':
		return "modified"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'T':
		return "type_changed"
	default:
		return strings.ToLower(status)
	}
}

func splitNUL(data []byte) []string {
	raw := bytes.Split(data, []byte{0})
	result := make([]string, 0, len(raw))
	for _, part := range raw {
		if len(part) > 0 {
			result = append(result, string(part))
		}
	}
	return result
}

func gitOutput(ctx context.Context, workspace string, args ...string) ([]byte, error) {
	safeArgs := make([]string, 0, len(args)+len(hardenedGitConfig())+4)
	safeArgs = append(safeArgs, hardenedGitConfig()...)
	if len(args) > 0 && args[0] == "diff" {
		safeArgs = append(safeArgs, "diff", "--no-ext-diff", "--no-textconv")
		safeArgs = append(safeArgs, args[1:]...)
	} else {
		safeArgs = append(safeArgs, args...)
	}

	command := exec.CommandContext(ctx, "git", safeArgs...)
	command.Dir = workspace
	command.Env = hardenedGitEnv()
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("evidence: git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

func canonicalWorkspace(workspace string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("evidence: workspace path is required")
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("evidence: resolve workspace path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("evidence: inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("evidence: workspace is not a directory")
	}
	return absolute, nil
}

type ArtifactStore interface {
	CreateArtifact(context.Context, store.Artifact) (store.Artifact, error)
}

type CandidateSnapshot struct {
	Manifest  store.Artifact
	Artifacts []store.Artifact
	Candidate Candidate
}

type CandidateSnapshotter struct {
	collector        *CandidateCollector
	store            ArtifactStore
	blobs            BlobStore
	reviewCandidates ReviewCandidateArchive
}

const maxCandidateFileChunks int64 = 4096

func NewCandidateSnapshotter(collector *CandidateCollector, store ArtifactStore, blobs BlobStore) (*CandidateSnapshotter, error) {
	return newCandidateSnapshotter(collector, store, blobs, nil)
}

func NewCandidateSnapshotterWithReviewCandidates(collector *CandidateCollector, store ArtifactStore, blobs BlobStore, reviewCandidates ReviewCandidateArchive) (*CandidateSnapshotter, error) {
	if reviewCandidates == nil {
		return nil, fmt.Errorf("evidence: review candidate store is required")
	}
	return newCandidateSnapshotter(collector, store, blobs, reviewCandidates)
}

func newCandidateSnapshotter(collector *CandidateCollector, store ArtifactStore, blobs BlobStore, reviewCandidates ReviewCandidateArchive) (*CandidateSnapshotter, error) {
	if collector == nil || store == nil || blobs == nil {
		return nil, fmt.Errorf("evidence: candidate collector, artifact store and blob store are required")
	}
	return &CandidateSnapshotter{collector: collector, store: store, blobs: blobs, reviewCandidates: reviewCandidates}, nil
}

func (s *CandidateSnapshotter) Snapshot(ctx context.Context, scope RunScope, workspace string) (CandidateSnapshot, error) {
	if s.reviewCandidates != nil {
		pinned, err := s.reviewCandidates.Open(ctx, scope.RunID)
		if err == nil {
			return s.snapshotPrivateCandidate(ctx, scope, pinned)
		}
		if !errors.Is(err, ErrReviewCandidateNotFound) {
			return CandidateSnapshot{}, fmt.Errorf("evidence: open private review candidate: %w", err)
		}
	}

	candidate, err := s.collector.Collect(ctx, workspace)
	if err != nil {
		return CandidateSnapshot{}, err
	}
	if s.reviewCandidates == nil {
		return s.snapshotWorkspaceCandidate(ctx, scope, workspace, candidate)
	}
	if err := s.reviewCandidates.Capture(ctx, scope.RunID, workspace, candidate); err != nil {
		return CandidateSnapshot{}, fmt.Errorf("evidence: capture private review candidate: %w", err)
	}
	pinned, err := s.reviewCandidates.Open(ctx, scope.RunID)
	if err != nil {
		return CandidateSnapshot{}, fmt.Errorf("evidence: reopen private review candidate: %w", err)
	}
	return s.snapshotPrivateCandidate(ctx, scope, pinned)
}

func (s *CandidateSnapshotter) snapshotWorkspaceCandidate(ctx context.Context, scope RunScope, workspace string, candidate Candidate) (CandidateSnapshot, error) {
	manifest, err := s.snapshotCandidateManifest(ctx, scope, candidate)
	if err != nil {
		return CandidateSnapshot{}, err
	}
	snapshot := CandidateSnapshot{Manifest: manifest, Candidate: candidate}

	for _, patch := range []struct {
		name string
		args []string
	}{
		{name: "candidate-staged.patch", args: []string{"diff", "--binary", "--cached", "HEAD"}},
		{name: "candidate-unstaged.patch", args: []string{"diff", "--binary"}},
	} {
		data, err := gitOutput(ctx, workspace, patch.args...)
		if err != nil {
			return CandidateSnapshot{}, err
		}
		if len(data) == 0 {
			continue
		}
		artifact, err := s.createArtifact(ctx, scope, patch.name, "candidate_patch", "text/x-diff", bytes.NewReader(data), store.EmptyObject)
		if err != nil {
			return CandidateSnapshot{}, err
		}
		snapshot.Artifacts = append(snapshot.Artifacts, artifact)
	}

	for _, change := range candidate.Changes {
		if !change.Untracked {
			continue
		}
		artifacts, err := s.snapshotUntrackedFile(ctx, scope, workspace, change.Path)
		if err != nil {
			return CandidateSnapshot{}, err
		}
		snapshot.Artifacts = append(snapshot.Artifacts, artifacts...)
	}
	return snapshot, nil
}

func (s *CandidateSnapshotter) snapshotPrivateCandidate(ctx context.Context, scope RunScope, candidate ReviewCandidateSnapshot) (CandidateSnapshot, error) {
	manifest, err := s.snapshotCandidateManifest(ctx, scope, candidate.Candidate)
	if err != nil {
		return CandidateSnapshot{}, err
	}
	snapshot := CandidateSnapshot{Manifest: manifest, Candidate: candidate.Candidate}

	for _, patch := range []struct {
		name   string
		source ReviewCandidateBlobSource
	}{
		{name: "candidate-staged.patch", source: candidate.StagedPatch},
		{name: "candidate-unstaged.patch", source: candidate.UnstagedPatch},
	} {
		if patch.source == nil {
			continue
		}
		reader, err := patch.source(ctx)
		if err != nil {
			return CandidateSnapshot{}, err
		}
		artifact, createErr := s.createArtifact(ctx, scope, patch.name, "candidate_patch", "text/x-diff", reader, store.EmptyObject)
		closeErr := reader.Close()
		if createErr != nil {
			return CandidateSnapshot{}, createErr
		}
		if closeErr != nil {
			return CandidateSnapshot{}, fmt.Errorf("evidence: close private candidate patch: %w", closeErr)
		}
		snapshot.Artifacts = append(snapshot.Artifacts, artifact)
	}

	for _, file := range candidate.Files {
		artifacts, err := s.snapshotPrivateCandidateFile(ctx, scope, file)
		if err != nil {
			return CandidateSnapshot{}, err
		}
		snapshot.Artifacts = append(snapshot.Artifacts, artifacts...)
	}
	return snapshot, nil
}

func (s *CandidateSnapshotter) snapshotCandidateManifest(ctx context.Context, scope RunScope, candidate Candidate) (store.Artifact, error) {
	manifestData, err := json.Marshal(candidate)
	if err != nil {
		return store.Artifact{}, fmt.Errorf("evidence: encode candidate manifest: %w", err)
	}
	return s.createArtifact(ctx, scope, "candidate-manifest.json", "candidate_manifest", "application/json", bytes.NewReader(manifestData), store.EmptyObject)
}

func (s *CandidateSnapshotter) snapshotUntrackedFile(ctx context.Context, scope RunScope, workspace, relative string) ([]store.Artifact, error) {
	file, info, err := openCandidateRegularFile(workspace, relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return s.snapshotCandidateFileReader(ctx, scope, relative, info.Mode().Perm()&0o111 != 0, file)
}

func (s *CandidateSnapshotter) snapshotPrivateCandidateFile(ctx context.Context, scope RunScope, candidate ReviewCandidateFile) ([]store.Artifact, error) {
	if candidate.Source == nil {
		return nil, fmt.Errorf("evidence: private candidate file source is required")
	}
	reader, err := candidate.Source(ctx)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return s.snapshotCandidateFileReader(ctx, scope, candidate.Path, candidate.Executable, reader)
}

func (s *CandidateSnapshotter) snapshotCandidateFileReader(ctx context.Context, scope RunScope, relative string, executable bool, reader io.Reader) ([]store.Artifact, error) {
	blobs, source := prepareCandidateBlobSource(s.blobs, scope.RunID, reader)
	limit := maxBlobBytes(blobs)
	if limit <= 0 {
		metadata, _ := json.Marshal(map[string]any{"path": relative, "executable": executable})
		artifact, err := s.createArtifactWithBlobStore(ctx, scope, blobs, relative, "candidate_file", "application/octet-stream", source, metadata)
		if err != nil {
			return nil, err
		}
		return []store.Artifact{artifact}, nil
	}

	prepared, size, err := spoolCandidateSource(ctx, source, limit)
	if err != nil {
		return nil, fmt.Errorf("evidence: snapshot untracked candidate %q: %w", relative, err)
	}
	defer func() {
		_ = prepared.Close()
		_ = os.Remove(prepared.Name())
	}()

	if size <= limit {
		metadata, _ := json.Marshal(map[string]any{"path": relative, "executable": executable})
		artifact, err := s.createArtifactWithBlobStore(ctx, scope, blobs, relative, "candidate_file", "application/octet-stream", prepared, metadata)
		if err != nil {
			return nil, err
		}
		return []store.Artifact{artifact}, nil
	}

	chunkCount, err := candidateChunkCount(size, limit)
	if err != nil {
		return nil, fmt.Errorf("evidence: snapshot untracked candidate %q: %w", relative, err)
	}
	artifacts := make([]store.Artifact, 0, chunkCount)
	for index := 0; index < chunkCount; index++ {
		offset := int64(index) * limit
		metadata, _ := json.Marshal(map[string]any{
			"path":       relative,
			"executable": executable,
			"chunkIndex": index,
			"chunkCount": chunkCount,
			"offset":     offset,
		})
		name := fmt.Sprintf("%s.part-%06d-of-%06d", relative, index+1, chunkCount)
		artifact, err := s.createArtifactWithBlobStore(ctx, scope, blobs, name, "candidate_file_chunk", "application/octet-stream", io.LimitReader(prepared, limit), metadata)
		if err != nil {
			return nil, fmt.Errorf("evidence: snapshot untracked candidate %q chunk %d/%d: %w", relative, index+1, chunkCount, err)
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func prepareCandidateBlobSource(blobs BlobStore, runID string, source io.Reader) (BlobStore, io.Reader) {
	for {
		redacting, ok := blobs.(*RedactingBlobStore)
		if !ok {
			return blobs, source
		}
		source = redacting.redactor.Reader(runID, source)
		blobs = redacting.base
	}
}

func spoolCandidateSource(ctx context.Context, source io.Reader, limit int64) (*os.File, int64, error) {
	if source == nil || limit <= 0 {
		return nil, 0, fmt.Errorf("invalid candidate source or chunk limit")
	}
	maxBytes := int64(math.MaxInt64)
	if limit <= math.MaxInt64/maxCandidateFileChunks {
		maxBytes = limit * maxCandidateFileChunks
	}

	prepared, err := os.CreateTemp("", ".agent-board-candidate-*")
	if err != nil {
		return nil, 0, fmt.Errorf("create prepared candidate file: %w", err)
	}
	cleanup := func() {
		_ = prepared.Close()
		_ = os.Remove(prepared.Name())
	}

	bounded := source
	if maxBytes < math.MaxInt64 {
		bounded = io.LimitReader(source, maxBytes+1)
	}
	written, err := copyContext(ctx, prepared, bounded)
	if err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("prepare candidate content: %w", err)
	}
	if written > maxBytes {
		cleanup()
		return nil, 0, fmt.Errorf("candidate exceeds maximum of %d chunks", maxCandidateFileChunks)
	}
	if _, err := prepared.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("rewind prepared candidate content: %w", err)
	}
	return prepared, written, nil
}

func openCandidateRegularFile(workspace, relative string) (*os.File, os.FileInfo, error) {
	workspace, err := canonicalWorkspace(workspace)
	if err != nil {
		return nil, nil, err
	}
	path, err := candidateFilePath(workspace, relative)
	if err != nil {
		return nil, nil, err
	}
	cleanRelative, err := filepath.Rel(workspace, path)
	if err != nil {
		return nil, nil, fmt.Errorf("evidence: resolve candidate path %q: %w", relative, err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, nil, fmt.Errorf("evidence: open workspace root: %w", err)
	}
	defer root.Close()

	before, err := root.Lstat(cleanRelative)
	if err != nil {
		return nil, nil, fmt.Errorf("evidence: inspect untracked candidate %q: %w", relative, err)
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("evidence: untracked candidate %q is a symbolic link", relative)
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("evidence: untracked candidate %q is not a regular file", relative)
	}

	file, err := root.Open(cleanRelative)
	if err != nil {
		return nil, nil, fmt.Errorf("evidence: open untraced candidate %q: %w", relative, err)
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("evidence: inspect opened candidate %q: %w", relative, err)
	}
	after, err := root.Lstat(cleanRelative)
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("evidence: re-inspect untracked candidate %q: %w", relative, err)
	}
	if after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() || !os.SameFile(before, after) || !os.SameFile(after, opened) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("evidence: untracked candidate %q changed while opening", relative)
	}
	return file, opened, nil
}

func candidateChunkCount(size, limit int64) (int, error) {
	if size < 0 || limit <= 0 {
		return 0, fmt.Errorf("invalid candidate size or chunk limit")
	}
	count := size / limit
	if size%limit != 0 {
		count++
	}
	if count > maxCandidateFileChunks {
		return 0, fmt.Errorf("candidate requires %d chunks; maximum is %d", count, maxCandidateFileChunks)
	}
	return int(count), nil
}

func (s *CandidateSnapshotter) createArtifact(ctx context.Context, scope RunScope, name, kind, mediaType string, source io.Reader, metadata json.RawMessage) (store.Artifact, error) {
	return s.createArtifactWithBlobStore(ctx, scope, s.blobs, name, kind, mediaType, source, metadata)
}

func (s *CandidateSnapshotter) createArtifactWithBlobStore(ctx context.Context, scope RunScope, blobs BlobStore, name, kind, mediaType string, source io.Reader, metadata json.RawMessage) (store.Artifact, error) {
	blob, err := blobs.Put(ctx, scope.RunID, source)
	if err != nil {
		return store.Artifact{}, err
	}
	digest := blob.Digest
	return s.store.CreateArtifact(ctx, store.Artifact{ProjectID: scope.ProjectID, IssueID: scope.IssueID, RunID: scope.RunID, Name: name, Kind: kind, MediaType: &mediaType, SizeBytes: blob.SizeBytes, Digest: &digest, StorageRef: blob.Ref, SafeMetadata: metadata})
}

func candidateFilePath(workspace, relative string) (string, error) {
	workspace, err := canonicalWorkspace(workspace)
	if err != nil {
		return "", err
	}
	path := filepath.Clean(filepath.Join(workspace, filepath.FromSlash(relative)))
	root := filepath.Clean(workspace) + string(os.PathSeparator)
	if !strings.HasPrefix(path+string(os.PathSeparator), root) {
		return "", fmt.Errorf("evidence: candidate path escapes workspace")
	}
	return path, nil
}
