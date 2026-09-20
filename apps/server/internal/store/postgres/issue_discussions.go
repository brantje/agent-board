package postgres

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/brantje/agent-board/apps/server/internal/store"
)

const (
	issueDiscussionRootTraversalDepth      = 64
	issueDiscussionRootCandidateLimit      = 256
	issueDiscussionRootCandidateMultiplier = 8
	issueDiscussionRootMetadataCommentLimit = 256
)

func (s *Store) ListIssueDiscussionRoots(ctx context.Context, projectID, issueID string, limit int) ([]store.IssueDiscussionRoot, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || limit <= 0 {
		return nil, store.ErrInvalidArgument
	}

	candidateLimit := limit * issueDiscussionRootCandidateMultiplier
	if candidateLimit < limit || candidateLimit > issueDiscussionRootCandidateLimit {
		candidateLimit = issueDiscussionRootCandidateLimit
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id::text, c.created_at
		FROM issue_comments AS c
		JOIN issues AS i ON i.id = c.issue_id
		WHERE i.project_id = $1 AND c.issue_id = $2
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT $3
	`, projectID, issueID, candidateLimit)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		id        string
		createdAt time.Time
	}
	candidates := make([]candidate, 0, candidateLimit)
	for rows.Next() {
		var value candidate
		if err := rows.Scan(&value.id, &value.createdAt); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	type metadata struct {
		id             string
		latestID       string
		lastActivityAt time.Time
		chainIDs       []string
	}
	selected := make([]metadata, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, candidate := range candidates {
		chainIDs, err := s.issueCommentAncestorIDs(ctx, projectID, issueID, candidate.id, issueDiscussionRootTraversalDepth)
		if err != nil {
			if errors.Is(err, store.ErrInvalidArgument) {
				// A newer comment exists beyond the supported ancestor depth.
				// Stop before older candidates so we never return a root with a
				// stale last-activity timestamp.
				break
			}
			return nil, err
		}
		rootID := chainIDs[0]
		if _, exists := seen[rootID]; exists {
			continue
		}
		seen[rootID] = struct{}{}
		selected = append(selected, metadata{
			id: rootID, latestID: candidate.id, lastActivityAt: candidate.createdAt, chainIDs: chainIDs,
		})
		if len(selected) >= limit {
			break
		}
	}

	ids := make([]string, 0, len(selected))
	for _, item := range selected {
		ids = append(ids, item.id)
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
		treeIDs, truncated, err := s.boundedIssueCommentTreeIDs(
			ctx, projectID, issueID, item.id, issueDiscussionRootTraversalDepth, issueDiscussionRootMetadataCommentLimit,
		)
		if err != nil {
			return nil, err
		}
		projection := store.IssueDiscussionRoot{
			Root:           root,
			ReplyCount:     len(treeIDs) - 1,
			LastActivityAt: item.lastActivityAt,
			Truncated:      truncated,
		}
		if root.ResolvedAt != nil {
			chain, err := s.listIssueCommentsByIDs(ctx, projectID, issueID, item.chainIDs)
			if err != nil {
				return nil, err
			}
			chainByID := make(map[string]store.IssueComment, len(chain))
			for _, comment := range chain {
				chainByID[comment.ID] = comment
			}
			projection.CompactComments = make([]store.IssueComment, 0, len(item.chainIDs))
			for _, id := range item.chainIDs {
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
	ids, truncated, err := s.boundedIssueCommentTreeIDs(ctx, projectID, issueID, rootID, maxDepth, maxComments)
	if err != nil {
		return store.IssueDiscussionThread{}, err
	}
	if !containsString(ids, anchorID) {
		if len(chain) > maxComments {
			return store.IssueDiscussionThread{}, store.ErrInvalidArgument
		}
		selected := make([]string, 0, maxComments)
		selectedSet := make(map[string]struct{}, maxComments)
		for _, id := range chain {
			selected = append(selected, id)
			selectedSet[id] = struct{}{}
		}
		for _, id := range ids {
			if _, exists := selectedSet[id]; exists {
				continue
			}
			if len(selected) >= maxComments {
				break
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
	comments, err = orderIssueCommentsAncestorFirst(comments)
	if err != nil {
		return store.IssueDiscussionThread{}, err
	}
	return store.IssueDiscussionThread{RootID: rootID, AnchorID: anchorID, Comments: comments, Truncated: truncated}, nil
}

func (s *Store) boundedIssueCommentTreeIDs(ctx context.Context, projectID, issueID, rootID string, maxDepth, maxComments int) ([]string, bool, error) {
	if strings.TrimSpace(rootID) == "" || maxDepth < 1 || maxComments < 1 {
		return nil, false, store.ErrInvalidArgument
	}
	ids := []string{rootID}
	frontier := []string{rootID}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		remaining := maxComments - len(ids)
		if remaining <= 0 {
			more, err := s.issueCommentChildrenExist(ctx, projectID, issueID, frontier)
			return ids, more, err
		}
		children, hasMore, err := s.issueCommentChildIDs(ctx, projectID, issueID, frontier, remaining)
		if err != nil {
			return nil, false, err
		}
		ids = append(ids, children...)
		if hasMore {
			return ids, true, nil
		}
		frontier = children
	}
	if len(frontier) == 0 {
		return ids, false, nil
	}
	more, err := s.issueCommentChildrenExist(ctx, projectID, issueID, frontier)
	if err != nil {
		return nil, false, err
	}
	return ids, more, nil
}

func (s *Store) issueCommentChildIDs(ctx context.Context, projectID, issueID string, parentIDs []string, limit int) ([]string, bool, error) {
	if len(parentIDs) == 0 || limit < 1 {
		return []string{}, false, nil
	}
	queryLimit := limit + 1
	rows, err := s.pool.Query(ctx, `
		SELECT bounded.id::text
		FROM unnest($3::uuid[]) AS parent(parent_id)
		CROSS JOIN LATERAL (
			SELECT c.id, c.created_at
			FROM issue_comments AS c
			JOIN issues AS i ON i.id = c.issue_id
			WHERE i.project_id = $1
			  AND c.issue_id = $2
			  AND c.parent_comment_id = parent.parent_id
			ORDER BY c.created_at, c.id
			LIMIT $4
		) AS bounded
		ORDER BY bounded.created_at, bounded.id
		LIMIT $4
	`, projectID, issueID, parentIDs, queryLimit)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	ids := make([]string, 0, limit+1)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, false, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	return ids, hasMore, nil
}

func (s *Store) issueCommentChildrenExist(ctx context.Context, projectID, issueID string, parentIDs []string) (bool, error) {
	if len(parentIDs) == 0 {
		return false, nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM issue_comments AS c
			JOIN issues AS i ON i.id = c.issue_id
			WHERE i.project_id = $1
			  AND c.issue_id = $2
			  AND c.parent_comment_id = ANY($3::uuid[])
			LIMIT 1
		)
	`, projectID, issueID, parentIDs).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
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
		chainIDs, ancestorTruncated, err := s.issueCommentAncestorIDsBounded(ctx, projectID, issueID, id, maxDepth)
		if err != nil {
			return store.IssueDiscussionUpdates{}, err
		}
		if ancestorTruncated {
			// The complete ancestor chain is outside the bounded walk. Represent
			// the canonical new comment explicitly rather than failing the page or
			// advancing past an update the caller never received.
			if _, exists := included[id]; !exists {
				if len(included)+1 > maxContext {
					hasMore = true
					break
				}
				included[id] = comment
			}
			newIDs[id] = struct{}{}
			accepted = append(accepted, comment)
			continue
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

	contextTruncated, err := issueDiscussionContextTruncation(included)
	if err != nil {
		return store.IssueDiscussionUpdates{}, err
	}
	values := make([]store.IssueComment, 0, len(included))
	for _, comment := range included {
		values = append(values, comment)
	}
	values, err = orderIssueCommentsAncestorFirstWithPartialContext(values, contextTruncated)
	if err != nil {
		return store.IssueDiscussionUpdates{}, err
	}
	comments := make([]store.IssueDiscussionComment, 0, len(values))
	for _, comment := range values {
		_, isNew := newIDs[comment.ID]
		comments = append(comments, store.IssueDiscussionComment{
			Comment: comment, IsNew: isNew, ContextTruncated: contextTruncated[comment.ID],
		})
	}
	last := accepted[len(accepted)-1]
	next := store.IssueCommentCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	return store.IssueDiscussionUpdates{Comments: comments, NextCursor: &next, HasMore: hasMore || len(accepted) < len(candidateIDs)}, nil
}

func issueDiscussionContextTruncation(values map[string]store.IssueComment) (map[string]bool, error) {
	truncated := make(map[string]bool, len(values))
	state := make(map[string]uint8, len(values))
	var visit func(string) (bool, error)
	visit = func(id string) (bool, error) {
		switch state[id] {
		case 1:
			return false, store.ErrInvalidArgument
		case 2:
			return truncated[id], nil
		}
		value, ok := values[id]
		if !ok {
			return true, nil
		}
		state[id] = 1
		partial := false
		if value.ParentCommentID != nil {
			if _, ok := values[*value.ParentCommentID]; !ok {
				partial = true
			} else {
				var err error
				partial, err = visit(*value.ParentCommentID)
				if err != nil {
					return false, err
				}
			}
		}
		state[id] = 2
		truncated[id] = partial
		return partial, nil
	}
	for id := range values {
		if _, err := visit(id); err != nil {
			return nil, err
		}
	}
	return truncated, nil
}

func orderIssueCommentsAncestorFirst(values []store.IssueComment) ([]store.IssueComment, error) {
	return orderIssueCommentsAncestorFirstWithPartialContext(values, nil)
}

func orderIssueCommentsAncestorFirstWithPartialContext(values []store.IssueComment, contextTruncated map[string]bool) ([]store.IssueComment, error) {
	if len(values) == 0 {
		return []store.IssueComment{}, nil
	}
	byID := make(map[string]store.IssueComment, len(values))
	for _, value := range values {
		byID[value.ID] = value
	}
	roots := make([]store.IssueComment, 0)
	children := make(map[string][]store.IssueComment)
	for _, value := range values {
		if value.ParentCommentID == nil {
			roots = append(roots, value)
			continue
		}
		if _, ok := byID[*value.ParentCommentID]; !ok {
			if contextTruncated != nil && contextTruncated[value.ID] {
				roots = append(roots, value)
				continue
			}
			return nil, store.ErrInvalidArgument
		}
		children[*value.ParentCommentID] = append(children[*value.ParentCommentID], value)
	}
	less := func(left, right store.IssueComment) bool {
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ID < right.ID
	}
	sort.Slice(roots, func(i, j int) bool { return less(roots[i], roots[j]) })
	for parentID := range children {
		siblings := children[parentID]
		sort.Slice(siblings, func(i, j int) bool { return less(siblings[i], siblings[j]) })
		children[parentID] = siblings
	}
	ordered := make([]store.IssueComment, 0, len(values))
	visited := make(map[string]struct{}, len(values))
	var visit func(store.IssueComment) error
	visit = func(value store.IssueComment) error {
		if _, exists := visited[value.ID]; exists {
			return store.ErrInvalidArgument
		}
		visited[value.ID] = struct{}{}
		ordered = append(ordered, value)
		for _, child := range children[value.ID] {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range roots {
		if err := visit(root); err != nil {
			return nil, err
		}
	}
	if len(ordered) != len(values) {
		return nil, store.ErrInvalidArgument
	}
	return ordered, nil
}

func (s *Store) issueCommentAncestorIDs(ctx context.Context, projectID, issueID, anchorID string, maxDepth int) ([]string, error) {
	ids, truncated, err := s.issueCommentAncestorIDsBounded(ctx, projectID, issueID, anchorID, maxDepth)
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, store.ErrInvalidArgument
	}
	return ids, nil
}

func (s *Store) issueCommentAncestorIDsBounded(ctx context.Context, projectID, issueID, anchorID string, maxDepth int) ([]string, bool, error) {
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
		return nil, false, err
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
			return nil, false, err
		}
		chain = append(chain, value)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(chain) == 0 {
		return nil, false, store.ErrNotFound
	}
	last := chain[len(chain)-1]
	if last.parentID != nil {
		return nil, store.ErrInvalidArgument
	}
	ids := make([]string, 0, len(chain))
	for index := len(chain) - 1; index >= 0; index-- {
		ids = append(ids, chain[index].id)
	}
	return ids, truncated, nil
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
