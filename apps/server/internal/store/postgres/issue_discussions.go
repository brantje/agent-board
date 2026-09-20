package postgres

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const issueDiscussionRootTraversalDepth = 64

func (s *Store) ListIssueDiscussionRoots(ctx context.Context, projectID, issueID string, limit int) ([]store.IssueDiscussionRoot, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || limit <= 0 {
		return nil, store.ErrInvalidArgument
	}
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE thread(root_id, id, created_at, depth) AS (
			SELECT c.id, c.id, c.created_at, 0
			FROM issue_comments AS c
			JOIN issues AS i ON i.id = c.issue_id
			WHERE i.project_id = $1 AND c.issue_id = $2 AND c.parent_comment_id IS NULL
			UNION ALL
			SELECT t.root_id, child.id, child.created_at, t.depth + 1
			FROM thread AS t
			JOIN issue_comments AS child ON child.issue_id = $2 AND child.parent_comment_id = t.id
			WHERE t.depth < $3
		), stats AS (
			SELECT t.root_id,
			       count(*) - 1 AS reply_count,
			       max(t.created_at) AS last_activity_at,
			       (array_agg(t.id ORDER BY t.created_at DESC, t.id DESC))[1] AS latest_id,
			       bool_or(t.depth = $3 AND EXISTS (
				   SELECT 1 FROM issue_comments AS child
				   WHERE child.issue_id = $2 AND child.parent_comment_id = t.id
			       )) AS truncated
			FROM thread AS t
			GROUP BY t.root_id
		)
		SELECT root_id::text, reply_count, last_activity_at, latest_id::text, truncated
		FROM stats
		ORDER BY last_activity_at DESC, root_id DESC
		LIMIT $4
	`, projectID, issueID, issueDiscussionRootTraversalDepth, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type metadata struct {
		id             string
		replyCount     int
		lastActivityAt time.Time
		latestID       string
		truncated      bool
	}
	selected := make([]metadata, 0, limit)
	ids := make([]string, 0, limit)
	for rows.Next() {
		var value metadata
		if err := rows.Scan(&value.id, &value.replyCount, &value.lastActivityAt, &value.latestID, &value.truncated); err != nil {
			return nil, err
		}
		selected = append(selected, value)
		ids = append(ids, value.id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	comments, err := s.listIssueCommentsByIDs(ctx, projectID, issueID, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]store.IssueComment, len(comments))
	for _, comment := range comments {
		byID[comment.ID] = comment
	}
	result := make([]store.IssueDiscussionRoot, 0, len(selected))
	for _, item := range selected {
		root, ok := byID[item.id]
		if !ok {
			return nil, store.ErrNotFound
		}
		projection := store.IssueDiscussionRoot{
			Root:           root,
			ReplyCount:     item.replyCount,
			LastActivityAt: item.lastActivityAt,
			Truncated:      item.truncated,
		}
		if root.ResolvedAt != nil {
			chainIDs, err := s.issueCommentAncestorIDs(ctx, projectID, issueID, item.latestID, issueDiscussionRootTraversalDepth)
			if err != nil {
				return nil, err
			}
			chain, err := s.listIssueCommentsByIDs(ctx, projectID, issueID, chainIDs)
			if err != nil {
				return nil, err
			}
			chainByID := make(map[string]store.IssueComment, len(chain))
			for _, comment := range chain {
				chainByID[comment.ID] = comment
			}
			projection.CompactComments = make([]store.IssueComment, 0, len(chainIDs))
			for _, id := range chainIDs {
				comment, ok := chainByID[id]
				if !ok {
					return nil, store.ErrNotFound
				}
				projection.CompactComments = append(projection.CompactComments, comment)
			}
		}
		result = append(result, projection)
	}
	return result, nil
}

func (s *Store) GetIssueDiscussionThread(ctx context.Context, projectID, issueID, anchorID string, maxDepth, maxComments int) (store.IssueDiscussionThread, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || strings.TrimSpace(anchorID) == "" || maxDepth < 1 || maxComments < 1 {
		return store.IssueDiscussionThread{}, store.ErrInvalidArgument
	}
	chain, err := s.issueCommentAncestorIDs(ctx, projectID, issueID, anchorID, maxDepth)
	if err != nil {
		return store.IssueDiscussionThread{}, err
	}
	rootID := chain[0]
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE descendants(id, depth) AS (
			SELECT c.id, 0
			FROM issue_comments AS c
			JOIN issues AS i ON i.id = c.issue_id
			WHERE i.project_id = $1 AND c.issue_id = $2 AND c.id = $3
			UNION ALL
			SELECT child.id, d.depth + 1
			FROM descendants AS d
			JOIN issue_comments AS child ON child.issue_id = $2 AND child.parent_comment_id = d.id
			WHERE d.depth < $4
		)
		SELECT d.id::text, d.depth,
		       (d.depth = $4 AND EXISTS (
			   SELECT 1 FROM issue_comments AS child
			   WHERE child.issue_id = $2 AND child.parent_comment_id = d.id
		       )) AS has_deeper
		FROM descendants AS d
		JOIN issue_comments AS c ON c.id = d.id
		ORDER BY c.created_at, c.id
		LIMIT $5
	`, projectID, issueID, rootID, maxDepth, maxComments+1)
	if err != nil {
		return store.IssueDiscussionThread{}, err
	}
	defer rows.Close()

	ids := make([]string, 0, maxComments+1)
	truncated := false
	for rows.Next() {
		var id string
		var depth int
		var hasDeeper bool
		if err := rows.Scan(&id, &depth, &hasDeeper); err != nil {
			return store.IssueDiscussionThread{}, err
		}
		if hasDeeper {
			truncated = true
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return store.IssueDiscussionThread{}, err
	}
	if len(ids) > maxComments {
		ids = ids[:maxComments]
		truncated = true
	}
	if !containsString(ids, anchorID) {
		if len(chain) > maxComments {
			return store.IssueDiscussionThread{}, store.ErrInvalidArgument
		}
		chainSet := make(map[string]struct{}, len(chain))
		for _, id := range chain {
			chainSet[id] = struct{}{}
		}
		selected := make([]string, 0, maxComments)
		selectedSet := make(map[string]struct{}, maxComments)
		for _, id := range ids {
			if _, mandatory := chainSet[id]; mandatory {
				continue
			}
			if len(selected)+len(chain) >= maxComments {
				break
			}
			selected = append(selected, id)
			selectedSet[id] = struct{}{}
		}
		for _, id := range chain {
			if _, exists := selectedSet[id]; exists {
				continue
			}
			selected = append(selected, id)
			selectedSet[id] = struct{}{}
		}
		ids = selected
		truncated = true
	}
	comments, err := s.listIssueCommentsByIDs(ctx, projectID, issueID, ids)
	if err != nil {
		return store.IssueDiscussionThread{}, err
	}
	return store.IssueDiscussionThread{RootID: rootID, AnchorID: anchorID, Comments: comments, Truncated: truncated}, nil
}

func (s *Store) ListIssueDiscussionUpdates(ctx context.Context, projectID, issueID string, cursor *store.IssueCommentCursor, newLimit, maxContext, maxDepth int) (store.IssueDiscussionUpdates, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || newLimit < 1 || maxDepth < 1 || maxContext < maxDepth+1 {
		return store.IssueDiscussionUpdates{}, store.ErrInvalidArgument
	}
	var createdAt any
	var cursorID any
	if cursor != nil {
		if cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
			return store.IssueDiscussionUpdates{}, store.ErrInvalidArgument
		}
		createdAt = cursor.CreatedAt
		cursorID = cursor.ID
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id::text
		FROM issue_comments AS c
		JOIN issues AS i ON i.id = c.issue_id
		WHERE i.project_id = $1 AND c.issue_id = $2
		  AND ($3::timestamptz IS NULL OR (c.created_at, c.id) > ($3::timestamptz, $4::uuid))
		ORDER BY c.created_at, c.id
		LIMIT $5
	`, projectID, issueID, createdAt, cursorID, newLimit+1)
	if err != nil {
		return store.IssueDiscussionUpdates{}, err
	}
	defer rows.Close()

	candidateIDs := make([]string, 0, newLimit+1)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return store.IssueDiscussionUpdates{}, err
		}
		candidateIDs = append(candidateIDs, id)
	}
	if err := rows.Err(); err != nil {
		return store.IssueDiscussionUpdates{}, err
	}
	hasMore := len(candidateIDs) > newLimit
	if hasMore {
		candidateIDs = candidateIDs[:newLimit]
	}
	if len(candidateIDs) == 0 {
		return store.IssueDiscussionUpdates{HasMore: false}, nil
	}
	candidates, err := s.listIssueCommentsByIDs(ctx, projectID, issueID, candidateIDs)
	if err != nil {
		return store.IssueDiscussionUpdates{}, err
	}
	candidateByID := make(map[string]store.IssueComment, len(candidates))
	for _, comment := range candidates {
		candidateByID[comment.ID] = comment
	}

	included := make(map[string]store.IssueComment)
	newIDs := make(map[string]struct{})
	accepted := make([]store.IssueComment, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		comment, ok := candidateByID[id]
		if !ok {
			return store.IssueDiscussionUpdates{}, store.ErrNotFound
		}
		chainIDs, err := s.issueCommentAncestorIDs(ctx, projectID, issueID, id, maxDepth)
		if err != nil {
			return store.IssueDiscussionUpdates{}, err
		}
		missingIDs := make([]string, 0, len(chainIDs))
		for _, chainID := range chainIDs {
			if _, exists := included[chainID]; !exists {
				missingIDs = append(missingIDs, chainID)
			}
		}
		if len(included)+len(missingIDs) > maxContext {
			hasMore = true
			break
		}
		missing, err := s.listIssueCommentsByIDs(ctx, projectID, issueID, missingIDs)
		if err != nil {
			return store.IssueDiscussionUpdates{}, err
		}
		for _, value := range missing {
			included[value.ID] = value
		}
		newIDs[id] = struct{}{}
		accepted = append(accepted, comment)
	}
	if len(accepted) == 0 {
		return store.IssueDiscussionUpdates{}, store.ErrInvalidArgument
	}

	comments := make([]store.IssueDiscussionComment, 0, len(included))
	for _, comment := range included {
		_, isNew := newIDs[comment.ID]
		comments = append(comments, store.IssueDiscussionComment{Comment: comment, IsNew: isNew})
	}
	sort.Slice(comments, func(i, j int) bool {
		left, right := comments[i].Comment, comments[j].Comment
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ID < right.ID
	})
	last := accepted[len(accepted)-1]
	next := store.IssueCommentCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	return store.IssueDiscussionUpdates{Comments: comments, NextCursor: &next, HasMore: hasMore || len(accepted) < len(candidateIDs)}, nil
}

func (s *Store) issueCommentAncestorIDs(ctx context.Context, projectID, issueID, anchorID string, maxDepth int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE ancestors(id, parent_comment_id, depth) AS (
			SELECT c.id, c.parent_comment_id, 0
			FROM issue_comments AS c
			JOIN issues AS i ON i.id = c.issue_id
			WHERE i.project_id = $1 AND c.issue_id = $2 AND c.id = $3
			UNION ALL
			SELECT parent.id, parent.parent_comment_id, a.depth + 1
			FROM ancestors AS a
			JOIN issue_comments AS parent ON parent.issue_id = $2 AND parent.id = a.parent_comment_id
			WHERE a.depth < $4
		)
		SELECT id::text, parent_comment_id::text, depth
		FROM ancestors
		ORDER BY depth
	`, projectID, issueID, anchorID, maxDepth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type item struct {
		id       string
		parentID *string
		depth    int
	}
	chain := make([]item, 0, maxDepth+1)
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.id, &value.parentID, &value.depth); err != nil {
			return nil, err
		}
		chain = append(chain, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, store.ErrNotFound
	}
	last := chain[len(chain)-1]
	if last.parentID != nil {
		return nil, store.ErrInvalidArgument
	}
	ids := make([]string, 0, len(chain))
	for index := len(chain) - 1; index >= 0; index-- {
		ids = append(ids, chain[index].id)
	}
	return ids, nil
}

func (s *Store) listIssueCommentsByIDs(ctx context.Context, projectID, issueID string, ids []string) ([]store.IssueComment, error) {
	if len(ids) == 0 {
		return []store.IssueComment{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id::text, c.issue_id::text, c.parent_comment_id::text, c.author_type, c.author_id::text,
		       COALESCE(
		           CASE
		               WHEN c.author_type = 'HUMAN' THEN (SELECT u.display_name FROM users AS u WHERE u.id = c.author_id)
		               WHEN c.author_type = 'AGENT' THEN (SELECT a.name FROM agents AS a WHERE a.id = c.author_id AND (a.project_id IS NULL OR a.project_id = i.project_id))
		           END,
		           ''
		       ),
		       c.source_run_id::text, c.source_action_key, COALESCE(c.body, ''), c.deleted_at, c.resolved_at, c.resolved_by_user_id::text,
		       COALESCE((SELECT u.display_name FROM users AS u WHERE u.id = c.resolved_by_user_id), ''),
		       c.created_at, c.updated_at
		FROM issue_comments AS c
		JOIN issues AS i ON i.id = c.issue_id
		WHERE i.project_id = $1 AND c.issue_id = $2 AND c.id::text = ANY($3::text[])
		ORDER BY c.created_at, c.id
	`, projectID, issueID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]store.IssueComment, 0, len(ids))
	for rows.Next() {
		value, err := scanIssueComment(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.loadIssueCommentReactions(ctx, projectID, issueID, values); err != nil {
		return nil, err
	}
	return values, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
