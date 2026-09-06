package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = workspace
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
	collector *CandidateCollector
	store     ArtifactStore
	blobs     BlobStore
}

func NewCandidateSnapshotter(collector *CandidateCollector, store ArtifactStore, blobs BlobStore) (*CandidateSnapshotter, error) {
	if collector == nil || store == nil || blobs == nil {
		return nil, fmt.Errorf("evidence: candidate collector, artifact store and blob store are required")
	}
	return &CandidateSnapshotter{collector: collector, store: store, blobs: blobs}, nil
}

func (s *CandidateSnapshotter) Snapshot(ctx context.Context, scope RunScope, workspace string) (CandidateSnapshot, error) {
	candidate, err := s.collector.Collect(ctx, workspace)
	if err != nil {
		return CandidateSnapshot{}, err
	}
	manifestData, err := json.Marshal(candidate)
	if err != nil {
		return CandidateSnapshot{}, fmt.Errorf("evidence: encode candidate manifest: %w", err)
	}
	manifest, err := s.createArtifact(ctx, scope, "candidate-manifest.json", "candidate_manifest", "application/json", bytes.NewReader(manifestData), store.EmptyObject)
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
		path, err := candidateFilePath(workspace, change.Path)
		if err != nil {
			return CandidateSnapshot{}, err
		}
		file, err := os.Open(path)
		if err != nil {
			return CandidateSnapshot{}, fmt.Errorf("evidence: open untracked candidate %q: %w", change.Path, err)
		}
		metadata, _ := json.Marshal(map[string]string{"path": change.Path})
		artifact, createErr := s.createArtifact(ctx, scope, change.Path, "candidate_file", "application/octet-stream", file, metadata)
		_ = file.Close()
		if createErr != nil {
			return CandidateSnapshot{}, createErr
		}
		snapshot.Artifacts = append(snapshot.Artifacts, artifact)
	}
	return snapshot, nil
}

func (s *CandidateSnapshotter) createArtifact(ctx context.Context, scope RunScope, name, kind, mediaType string, source io.Reader, metadata json.RawMessage) (store.Artifact, error) {
	blob, err := s.blobs.Put(ctx, scope.RunID, source)
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
