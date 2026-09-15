package postgres

const effectiveProjectRoleExpression = `
CASE
	WHEN u.deployment_role='admin' THEN 'admin'
	ELSE (
		SELECT role
		FROM (
			SELECT pua.role
			FROM project_user_access AS pua
			WHERE pua.project_id=$1 AND pua.user_id=u.id
			UNION ALL
			SELECT pga.role
			FROM project_group_access AS pga
			JOIN group_members AS gm ON gm.group_id=pga.group_id
			WHERE pga.project_id=$1 AND gm.user_id=u.id
		) grants
		ORDER BY CASE role WHEN 'admin' THEN 3 WHEN 'member' THEN 2 WHEN 'viewer' THEN 1 ELSE 0 END DESC
		LIMIT 1
	)
END`
