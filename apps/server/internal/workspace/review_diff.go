package workspace

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type ReviewFileChange struct {
	Path       string
	OldPath    string
	ChangeType string
	Added      int
	Removed    int
}

func (g *GitCLI) ReviewFileChanges(ctx context.Context, repositoryPath, baseRevision, reviewRevision string) ([]ReviewFileChange, error) {
	baseRevision = strings.TrimSpace(baseRevision)
	reviewRevision = strings.TrimSpace(reviewRevision)
	if baseRevision == "" || reviewRevision == "" {
		return nil, fmt.Errorf("review git revisions are required")
	}
	if baseRevision == reviewRevision {
		return nil, nil
	}

	statusOutput, err := g.run(ctx, "-C", repositoryPath, "diff", "--name-status", "-M", baseRevision+".."+reviewRevision)
	if err != nil {
		return nil, err
	}
	numstatOutput, err := g.run(ctx, "-C", repositoryPath, "diff", "--numstat", "-M", baseRevision+".."+reviewRevision)
	if err != nil {
		return nil, err
	}

	statsByPath := parseDiffNumstat(numstatOutput)
	changes := parseDiffNameStatus(statusOutput, statsByPath)
	return changes, nil
}

func parseDiffNumstat(output string) map[string]ReviewFileChange {
	stats := make(map[string]ReviewFileChange)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}
		added, removed := parseDiffStatCount(parts[0]), parseDiffStatCount(parts[1])
		path := strings.TrimSpace(parts[2])
		if path == "" {
			continue
		}
		stats[path] = ReviewFileChange{Path: path, Added: added, Removed: removed}
	}
	return stats
}

func parseDiffStatCount(value string) int {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return 0
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0
	}
	return count
}

func parseDiffNameStatus(output string, statsByPath map[string]ReviewFileChange) []ReviewFileChange {
	changes := make([]ReviewFileChange, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		change := ReviewFileChange{}
		switch {
		case strings.HasPrefix(status, "R") && len(parts) >= 3:
			change.ChangeType = "renamed"
			change.OldPath = parts[1]
			change.Path = parts[2]
		case status == "A":
			change.ChangeType = "created"
			change.Path = parts[1]
		case status == "D":
			change.ChangeType = "deleted"
			change.Path = parts[1]
		case status == "M", status == "T":
			change.ChangeType = "modified"
			change.Path = parts[1]
		default:
			continue
		}
		if stat, ok := statsByPath[change.Path]; ok {
			change.Added = stat.Added
			change.Removed = stat.Removed
		}
		changes = append(changes, change)
	}
	return changes
}
