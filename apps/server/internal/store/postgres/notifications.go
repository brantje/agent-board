package postgres

import (
	"context"
	"strings"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const notificationProjectAccessPredicate = `(
	u.deployment_role = 'admin'
	OR EXISTS (
		SELECT 1 FROM project_user_access pua
		WHERE pua.project_id = p.id AND pua.user_id = u.id
	)
	OR EXISTS (
		SELECT 1
		FROM project_group_access pga
		JOIN group_members gm ON gm.group_id = pga.group_id
		WHERE pga.project_id = p.id AND gm.user_id = u.id
	)
)`

func (s *Store) CreateIssueCommentNotifications(ctx context.Context, projectID, commentID string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(commentID) == "" {
		return store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		WITH source AS (
			SELECT c.id, c.issue_id, c.parent_comment_id, c.author_type, c.author_id, i.project_id, c.created_at
			FROM issue_comments c
			JOIN issues i ON i.id = c.issue_id AND i.project_id = $1
			WHERE c.id = $2
		), candidates AS (
			SELECT parent.author_id AS recipient_user_id, 2 AS priority
			FROM source s
			JOIN issue_comments parent ON parent.issue_id = s.issue_id AND parent.id = s.parent_comment_id
			WHERE parent.author_type = 'HUMAN'
			UNION ALL
			SELECT sub.user_id AS recipient_user_id, 1 AS priority
			FROM source s
			JOIN issue_subscriptions sub ON sub.project_id = s.project_id AND sub.issue_id = s.issue_id
		), recipients AS (
			SELECT recipient_user_id, MAX(priority) AS priority
			FROM candidates
			GROUP BY recipient_user_id
		)
		INSERT INTO user_notifications (
			id, recipient_user_id, project_id, issue_id, source_comment_id, kind, created_at
		)
		SELECT gen_random_uuid(), r.recipient_user_id, s.project_id, s.issue_id, s.id,
			CASE WHEN r.priority >= 2 THEN $3 ELSE $4 END,
			s.created_at
		FROM source s
		JOIN recipients r ON TRUE
		JOIN users u ON u.id = r.recipient_user_id
		JOIN projects p ON p.id = s.project_id
		WHERE u.status = $5
		  AND NOT (s.author_type = 'HUMAN' AND r.recipient_user_id = s.author_id)
		  AND `+notificationProjectAccessPredicate+`
		ON CONFLICT (recipient_user_id, source_comment_id) DO UPDATE
		SET kind = CASE
			WHEN EXCLUDED.kind = $3 THEN EXCLUDED.kind
			ELSE user_notifications.kind
		END
	`, projectID, commentID, store.NotificationKindCommentReply, store.NotificationKindIssueComment, store.UserStatusActive)
	if err != nil {
		return notFound(err)
	}
	return tx.Commit(ctx)
}

func (s *Store) ListUserNotifications(ctx context.Context, userID string) (store.UserNotificationPage, error) {
	if strings.TrimSpace(userID) == "" {
		return store.UserNotificationPage{}, store.ErrInvalidArgument
	}
	rows, err := s.pool.Query(ctx, `
		SELECT n.id::text, n.recipient_user_id::text, n.project_id::text, p.name,
		       n.issue_id::text, p.issue_prefix || '-' || i.number::text, i.title,
		       n.source_comment_id::text, c.author_type, c.author_id::text,
		       COALESCE(CASE
		           WHEN c.author_type = 'HUMAN' THEN hu.display_name
		           WHEN c.author_type = 'AGENT' THEN a.name
		       END, ''),
		       CASE
		           WHEN c.deleted_at IS NOT NULL THEN 'Comment deleted'
		           ELSE left(regexp_replace(COALESCE(c.body, ''), E'[[:space:]]+', ' ', 'g'), 240)
		       END,
		       n.kind, n.created_at, n.read_at,
		       COUNT(*) FILTER (WHERE n.read_at IS NULL) OVER ()
		FROM user_notifications n
		JOIN users u ON u.id = n.recipient_user_id AND u.status = $1
		JOIN projects p ON p.id = n.project_id
		JOIN issues i ON i.project_id = n.project_id AND i.id = n.issue_id
		LEFT JOIN issue_comments c ON c.issue_id = n.issue_id AND c.id = n.source_comment_id
		LEFT JOIN users hu ON hu.id = c.author_id AND c.author_type = 'HUMAN'
		LEFT JOIN agents a ON a.id = c.author_id AND c.author_type = 'AGENT'
		WHERE n.recipient_user_id = $2
		  AND `+notificationProjectAccessPredicate+`
		ORDER BY n.created_at DESC, n.id DESC
	`, store.UserStatusActive, userID)
	if err != nil {
		return store.UserNotificationPage{}, err
	}
	defer rows.Close()

	page := store.UserNotificationPage{Notifications: make([]store.UserNotification, 0)}
	for rows.Next() {
		var value store.UserNotification
		if err := rows.Scan(
			&value.ID, &value.RecipientUserID, &value.ProjectID, &value.ProjectName,
			&value.IssueID, &value.IssueKey, &value.IssueTitle,
			&value.SourceCommentID, &value.CommentAuthorType, &value.CommentAuthorID,
			&value.CommentAuthorName, &value.Preview, &value.Kind, &value.CreatedAt,
			&value.ReadAt, &page.UnreadCount,
		); err != nil {
			return store.UserNotificationPage{}, err
		}
		page.Notifications = append(page.Notifications, value)
	}
	if err := rows.Err(); err != nil {
		return store.UserNotificationPage{}, err
	}
	return page, nil
}

func (s *Store) SetNotificationRead(ctx context.Context, userID, notificationID string, read bool) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(notificationID) == "" {
		return store.ErrInvalidArgument
	}
	command, err := s.pool.Exec(ctx, `
		UPDATE user_notifications n
		SET read_at = CASE WHEN $3 THEN COALESCE(n.read_at, now()) ELSE NULL END
		FROM users u, projects p
		WHERE n.id = $1 AND n.recipient_user_id = $2
		  AND u.id = n.recipient_user_id AND u.status = $4
		  AND p.id = n.project_id
		  AND `+notificationProjectAccessPredicate,
		notificationID, userID, read, store.UserStatusActive)
	if err != nil {
		return notFound(err)
	}
	if command.RowsAffected() != 1 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID string) (int, error) {
	if strings.TrimSpace(userID) == "" {
		return 0, store.ErrInvalidArgument
	}
	command, err := s.pool.Exec(ctx, `
		UPDATE user_notifications n
		SET read_at = now()
		FROM users u, projects p
		WHERE n.recipient_user_id = $1 AND n.read_at IS NULL
		  AND u.id = n.recipient_user_id AND u.status = $2
		  AND p.id = n.project_id
		  AND `+notificationProjectAccessPredicate,
		userID, store.UserStatusActive)
	if err != nil {
		return 0, err
	}
	return int(command.RowsAffected()), nil
}

func (s *Store) GetIssueSubscription(ctx context.Context, projectID, issueID, userID string) (bool, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || strings.TrimSpace(userID) == "" {
		return false, store.ErrInvalidArgument
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM issues i
			JOIN projects p ON p.id = i.project_id
			JOIN users u ON u.id = $3 AND u.status = $4
			WHERE i.project_id = $1 AND i.id = $2 AND `+notificationProjectAccessPredicate+`
		)
	`, projectID, issueID, userID, store.UserStatusActive).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		return false, store.ErrNotFound
	}
	var subscribed bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue_subscriptions WHERE project_id=$1 AND issue_id=$2 AND user_id=$3)`, projectID, issueID, userID).Scan(&subscribed); err != nil {
		return false, err
	}
	return subscribed, nil
}

func (s *Store) SetIssueSubscription(ctx context.Context, projectID, issueID, userID string, subscribed bool) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" || strings.TrimSpace(userID) == "" {
		return store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var allowed bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM issues i
			JOIN projects p ON p.id = i.project_id
			JOIN users u ON u.id = $3 AND u.status = $4
			WHERE i.project_id = $1 AND i.id = $2 AND `+notificationProjectAccessPredicate+`
		)
	`, projectID, issueID, userID, store.UserStatusActive).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return store.ErrNotFound
	}
	if subscribed {
		_, err = tx.Exec(ctx, `
			INSERT INTO issue_subscriptions (project_id, issue_id, user_id)
			VALUES ($1,$2,$3)
			ON CONFLICT (issue_id,user_id) DO NOTHING
		`, projectID, issueID, userID)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM issue_subscriptions WHERE issue_id=$1 AND user_id=$2`, issueID, userID)
	}
	if err != nil {
		return notFound(err)
	}
	return tx.Commit(ctx)
}

var _ store.NotificationStore = (*Store)(nil)
