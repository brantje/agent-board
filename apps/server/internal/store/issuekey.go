package store

import (
	"fmt"
	"strconv"
	"strings"
)

const issueWorkingBranchPrefix = "agent-board/"

// FormatIssueKey builds the public Issue identifier from a Project prefix and number.
func FormatIssueKey(prefix string, number int) string {
	return fmt.Sprintf("%s-%d", prefix, number)
}

// WorkingBranchForIssue returns the deterministic Issue Workspace working branch name.
func WorkingBranchForIssue(issueKey string) string {
	return issueWorkingBranchPrefix + strings.TrimSpace(issueKey)
}

// NormalizeIssuePrefix uppercases and trims a Project issue prefix.
func NormalizeIssuePrefix(prefix string) string {
	return strings.ToUpper(strings.TrimSpace(prefix))
}

// ValidIssuePrefix reports whether prefix matches the canonical pattern.
func ValidIssuePrefix(prefix string) bool {
	if len(prefix) < 2 || len(prefix) > 10 {
		return false
	}
	for i, r := range prefix {
		if i == 0 {
			if r < 'A' || r > 'Z' {
				return false
			}
			continue
		}
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// ValidIssueKey reports whether key matches PREFIX-NUMBER.
func ValidIssueKey(key string) bool {
	_, _, err := ParseIssueKey(key)
	return err == nil
}

// ParseIssueKey splits a public Issue key into prefix and number.
func ParseIssueKey(key string) (prefix string, number int, err error) {
	key = strings.TrimSpace(key)
	dash := strings.IndexByte(key, '-')
	if dash <= 0 || dash == len(key)-1 {
		return "", 0, fmt.Errorf("invalid issue key")
	}
	prefix = key[:dash]
	numberText := key[dash+1:]
	if !ValidIssuePrefix(prefix) {
		return "", 0, fmt.Errorf("invalid issue key prefix")
	}
	number, err = strconv.Atoi(numberText)
	if err != nil || number < 1 {
		return "", 0, fmt.Errorf("invalid issue key number")
	}
	return prefix, number, nil
}
