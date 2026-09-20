package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	issueDiscussionDefaultRootLimit   = 10
	issueDiscussionMaxRootLimit       = 20
	issueDiscussionDefaultThreadLimit = 100
	issueDiscussionMaxThreadLimit     = 100
	issueDiscussionDefaultUpdateLimit = 20
	issueDiscussionMaxUpdateLimit     = 50
	issueDiscussionMaxDepth           = 64
	issueDiscussionContextLimit       = 200
)

type IssueDiscussionUpdatePage struct {
	Comments   []store.IssueDiscussionComment
	NextCursor string
	HasMore    bool
}

type issueDiscussionCursorPayload struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

func (s *Service) ListRecentIssueDiscussions(ctx context.Context, projectID, issueID string, limit int) ([]store.IssueDiscussionRoot, error) {
	if _, err := s.GetIssue(ctx, projectID, issueID); err != nil {
		return nil, err
	}
	if s.issueDiscussions == nil {
		return nil, errors.New("issue discussions are unavailable")
	}
	bounded, err := normalizeIssueDiscussionLimit(limit, issueDiscussionDefaultRootLimit, issueDiscussionMaxRootLimit)
	if err != nil {
		return nil, err
	}
	values, err := s.issueDiscussions.ListIssueDiscussionRoots(ctx, projectID, issueID, bounded)
	if err != nil {
		return nil, translateStoreError(err, "issue_comment")
	}
	return values, nil
}

func (s *Service) GetIssueDiscussionThread(ctx context.Context, projectID, issueID, anchorCommentID string, limit int) (store.IssueDiscussionThread, error) {
	if _, err := s.GetIssue(ctx, projectID, issueID); err != nil {
		return store.IssueDiscussionThread{}, err
	}
	if s.issueDiscussions == nil {
		return store.IssueDiscussionThread{}, errors.New("issue discussions are unavailable")
	}
	if !validIssueDiscussionCursorID(anchorCommentID) {
		return store.IssueDiscussionThread{}, invalid("comment anchor must be a UUID")
	}
	bounded, err := normalizeIssueDiscussionLimit(limit, issueDiscussionDefaultThreadLimit, issueDiscussionMaxThreadLimit)
	if err != nil {
		return store.IssueDiscussionThread{}, err
	}
	value, err := s.issueDiscussions.GetIssueDiscussionThread(ctx, projectID, issueID, anchorCommentID, issueDiscussionMaxDepth, bounded)
	return value, translateStoreError(err, "issue_comment")
}

func (s *Service) ListIssueDiscussionUpdates(ctx context.Context, projectID, issueID, cursor string, limit int) (IssueDiscussionUpdatePage, error) {
	if _, err := s.GetIssue(ctx, projectID, issueID); err != nil {
		return IssueDiscussionUpdatePage{}, err
	}
	if s.issueDiscussions == nil {
		return IssueDiscussionUpdatePage{}, errors.New("issue discussions are unavailable")
	}
	bounded, err := normalizeIssueDiscussionLimit(limit, issueDiscussionDefaultUpdateLimit, issueDiscussionMaxUpdateLimit)
	if err != nil {
		return IssueDiscussionUpdatePage{}, err
	}
	decoded, err := decodeIssueDiscussionCursor(cursor)
	if err != nil {
		return IssueDiscussionUpdatePage{}, err
	}
	value, err := s.issueDiscussions.ListIssueDiscussionUpdates(ctx, projectID, issueID, decoded, bounded, issueDiscussionContextLimit, issueDiscussionMaxDepth)
	if err != nil {
		return IssueDiscussionUpdatePage{}, translateStoreError(err, "issue_comment")
	}
	nextCursor := strings.TrimSpace(cursor)
	if value.NextCursor != nil {
		nextCursor, err = encodeIssueDiscussionCursor(*value.NextCursor)
		if err != nil {
			return IssueDiscussionUpdatePage{}, err
		}
	}
	return IssueDiscussionUpdatePage{Comments: value.Comments, NextCursor: nextCursor, HasMore: value.HasMore}, nil
}

func normalizeIssueDiscussionLimit(value, defaultValue, maxValue int) (int, error) {
	if value < 0 {
		return 0, invalid("discussion limit must not be negative")
	}
	if value == 0 {
		return defaultValue, nil
	}
	if value > maxValue {
		return maxValue, nil
	}
	return value, nil
}

func encodeIssueDiscussionCursor(cursor store.IssueCommentCursor) (string, error) {
	if cursor.CreatedAt.IsZero() || !validIssueDiscussionCursorID(cursor.ID) {
		return "", invalid("discussion cursor is invalid")
	}
	encoded, err := json.Marshal(issueDiscussionCursorPayload{CreatedAt: cursor.CreatedAt.UTC(), ID: cursor.ID})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeIssueDiscussionCursor(value string) (*store.IssueCommentCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, invalid("discussion cursor is invalid")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var payload issueDiscussionCursorPayload
	if err := decoder.Decode(&payload); err != nil || payload.CreatedAt.IsZero() || !validIssueDiscussionCursorID(payload.ID) {
		return nil, invalid("discussion cursor is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, invalid("discussion cursor is invalid")
	}
	return &store.IssueCommentCursor{CreatedAt: payload.CreatedAt.UTC(), ID: payload.ID}, nil
}

func validIssueDiscussionCursorID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}
