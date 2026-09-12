package postgres

import (
	"context"
	"github.com/brantje/agent-board/apps/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateProjectWithAdmin(ctx context.Context, input store.Project, userID string) (store.Project, error) {
	if userID == "" {
		return store.Project{}, store.ErrInvalidArgument
	}
	prefix := store.NormalizeIssuePrefix(input.IssuePrefix)
	if !store.ValidIssuePrefix(prefix) {
		return store.Project{}, store.ErrInvalidArgument
	}
	sourceType := input.SourceType
	if sourceType == "" {
		sourceType = store.ProjectSourceLocal
	}
	branch := input.DefaultBranch
	if sourceType == store.ProjectSourceLocal && branch == "" {
		branch = "main"
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.Project{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Keep the same lock order as user status/direct-access mutations. This
	// makes Project creation + creator grant race safely with disabling the
	// creator or concurrently changing direct Project admins.
	if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return store.Project{}, err
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE project_user_access IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return store.Project{}, err
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1`, userID).Scan(&status); err != nil {
		return store.Project{}, notFound(err)
	}
	if status != store.UserStatusActive {
		return store.Project{}, store.ErrInvalidArgument
	}

	project, err := scanProject(tx.QueryRow(ctx, `
		INSERT INTO projects (name, issue_prefix, source_type, clone_url, source_ref, repository_path, default_branch, workflow_settings, allow_internal_runner)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9,true))
		RETURNING `+projectSelectColumns,
		input.Name, prefix, sourceType, input.CloneURL, input.SourceRef, input.RepositoryPath, branch, objectJSON(input.WorkflowSettings), input.AllowInternalRunner,
	))
	if err != nil {
		return store.Project{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_user_access (project_id,user_id,role)
		VALUES ($1,$2,$3)
	`, project.ID, userID, store.ProjectRoleAdmin); err != nil {
		return store.Project{}, notFound(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Project{}, err
	}
	return project, nil
}

func (s *Store) ListProjectsForUser(ctx context.Context, userID string) ([]store.Project, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+projectSelectColumns+`
		FROM projects p
		WHERE EXISTS (
			SELECT 1 FROM project_user_access pua
			WHERE pua.project_id=p.id AND pua.user_id=$1
		) OR EXISTS (
			SELECT 1
			FROM project_group_access pga
			JOIN group_members gm ON gm.group_id=pga.group_id
			WHERE pga.project_id=p.id AND gm.user_id=$1
		)
		ORDER BY p.created_at,p.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := make([]store.Project, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projects, nil
}

func (s *Store) EffectiveProjectRole(ctx context.Context, projectID, userID string) (string, error) {
	var role string
	if err := s.pool.QueryRow(ctx, `
		SELECT role
		FROM (
			SELECT pua.role
			FROM project_user_access pua
			WHERE pua.project_id=$1 AND pua.user_id=$2
			UNION ALL
			SELECT pga.role
			FROM project_group_access pga
			JOIN group_members gm ON gm.group_id=pga.group_id
			WHERE pga.project_id=$1 AND gm.user_id=$2
		) grants
		ORDER BY CASE role WHEN 'admin' THEN 3 WHEN 'member' THEN 2 WHEN 'viewer' THEN 1 ELSE 0 END DESC
		LIMIT 1
	`, projectID, userID).Scan(&role); err != nil {
		return "", notFound(err)
	}
	if !store.ValidProjectRole(role) {
		return "", store.ErrInvalidArgument
	}
	return role, nil
}

func (s *Store) ListProjectUserAccess(ctx context.Context, projectID string) ([]store.ProjectUserAccessView, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM projects WHERE id=$1)`, projectID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, store.ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT pua.project_id::text,pua.user_id::text,pua.role,
		       u.id::text,u.username,u.email,u.display_name,COALESCE(u.password_hash,''),u.deployment_role,u.status,u.force_password_change,u.auth_version,u.created_at,u.updated_at
		FROM project_user_access pua
		JOIN users u ON u.id=pua.user_id
		WHERE pua.project_id=$1
		ORDER BY u.display_name,u.username,u.id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]store.ProjectUserAccessView, 0)
	for rows.Next() {
		var value store.ProjectUserAccessView
		if err := rows.Scan(
			&value.Access.ProjectID, &value.Access.UserID, &value.Access.Role,
			&value.User.ID, &value.User.Username, &value.User.Email, &value.User.DisplayName, &value.User.PasswordHash,
			&value.User.DeploymentRole, &value.User.Status, &value.User.ForcePasswordChange, &value.User.AuthVersion,
			&value.User.CreatedAt, &value.User.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) UpsertProjectUserAccess(ctx context.Context, input store.ProjectUserAccess) (store.ProjectUserAccess, error) {
	if !store.ValidProjectRole(input.Role) {
		return store.ProjectUserAccess{}, store.ErrInvalidArgument
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.ProjectUserAccess{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockDirectProjectAdminState(ctx, tx); err != nil {
		return store.ProjectUserAccess{}, err
	}
	var value store.ProjectUserAccess
	if err := tx.QueryRow(ctx, `
		INSERT INTO project_user_access (project_id,user_id,role)
		VALUES ($1,$2,$3)
		ON CONFLICT (project_id,user_id) DO UPDATE SET role=EXCLUDED.role
		RETURNING project_id::text,user_id::text,role
	`, input.ProjectID, input.UserID, input.Role).Scan(&value.ProjectID, &value.UserID, &value.Role); err != nil {
		return store.ProjectUserAccess{}, notFound(err)
	}
	if err := ensureActiveDirectProjectAdmin(ctx, tx, input.ProjectID); err != nil {
		return store.ProjectUserAccess{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.ProjectUserAccess{}, err
	}
	return value, nil
}

func (s *Store) DeleteProjectUserAccess(ctx context.Context, projectID, userID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockDirectProjectAdminState(ctx, tx); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM project_user_access WHERE project_id=$1 AND user_id=$2`, projectID, userID)
	if err != nil {
		return notFound(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	if err := ensureActiveDirectProjectAdmin(ctx, tx, projectID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListProjectGroupAccess(ctx context.Context, projectID string) ([]store.ProjectGroupAccessView, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM projects WHERE id=$1)`, projectID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, store.ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT pga.project_id::text,pga.group_id::text,pga.role,g.id::text,g.name,g.created_at,g.updated_at
		FROM project_group_access pga
		JOIN groups g ON g.id=pga.group_id
		WHERE pga.project_id=$1
		ORDER BY g.name,g.id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]store.ProjectGroupAccessView, 0)
	for rows.Next() {
		var value store.ProjectGroupAccessView
		if err := rows.Scan(
			&value.Access.ProjectID, &value.Access.GroupID, &value.Access.Role,
			&value.Group.ID, &value.Group.Name, &value.Group.CreatedAt, &value.Group.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) UpsertProjectGroupAccess(ctx context.Context, input store.ProjectGroupAccess) (store.ProjectGroupAccess, error) {
	if !store.ValidProjectRole(input.Role) {
		return store.ProjectGroupAccess{}, store.ErrInvalidArgument
	}
	var value store.ProjectGroupAccess
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO project_group_access (project_id,group_id,role)
		VALUES ($1,$2,$3)
		ON CONFLICT (project_id,group_id) DO UPDATE SET role=EXCLUDED.role
		RETURNING project_id::text,group_id::text,role
	`, input.ProjectID, input.GroupID, input.Role).Scan(&value.ProjectID, &value.GroupID, &value.Role); err != nil {
		return store.ProjectGroupAccess{}, notFound(err)
	}
	return value, nil
}

func (s *Store) DeleteProjectGroupAccess(ctx context.Context, projectID, groupID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM project_group_access WHERE project_id=$1 AND group_id=$2`, projectID, groupID)
	if err != nil {
		return notFound(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func lockDirectProjectAdminState(ctx context.Context, tx pgx.Tx) error {
	// User status changes and direct grant mutations must take these locks in
	// this order. Serializing these relatively rare administration operations
	// avoids a race that could otherwise leave a Project without an active
	// direct individual admin.
	if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE project_user_access IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	return nil
}

func ensureActiveDirectProjectAdmin(ctx context.Context, tx pgx.Tx, projectID string) error {
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM project_user_access pua
		JOIN users u ON u.id=pua.user_id
		WHERE pua.project_id=$1 AND pua.role=$2 AND u.status=$3
	`, projectID, store.ProjectRoleAdmin, store.UserStatusActive).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return store.ErrLastProjectAdmin
	}
	return nil
}

func (s *Store) userIsSoleActiveDirectAdmin(ctx context.Context, tx pgx.Tx, userID string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM project_user_access mine
			WHERE mine.user_id=$1 AND mine.role=$2
			AND NOT EXISTS (
				SELECT 1
				FROM project_user_access other
				JOIN users u ON u.id=other.user_id
				WHERE other.project_id=mine.project_id
				  AND other.role=$2
				  AND other.user_id<>$1
				  AND u.status=$3
			)
		)
	`, userID, store.ProjectRoleAdmin, store.UserStatusActive).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

var _ store.ProjectAccessStore = (*Store)(nil)
