-- name: ListUsers :many
SELECT id, email, name, avatar_url, roles::text[] AS roles, created_at, updated_at
FROM users
WHERE disabled_at IS NULL
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search') || '%'
    OR email ILIKE '%' || sqlc.narg('search') || '%'
  )
  AND (
    sqlc.narg('role')::text IS NULL
    OR sqlc.narg('role')::user_role = ANY (roles)
  )
ORDER BY name, id
LIMIT sqlc.arg('limit')::int OFFSET sqlc.arg('offset')::int;

-- name: CountUsers :one
SELECT count(*)
FROM users
WHERE disabled_at IS NULL
  AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search') || '%'
    OR email ILIKE '%' || sqlc.narg('search') || '%'
  )
  AND (
    sqlc.narg('role')::text IS NULL
    OR sqlc.narg('role')::user_role = ANY (roles)
  );

-- name: GetActiveUserByID :one
SELECT id, email, name, avatar_url, roles::text[] AS roles, created_at, updated_at
FROM users
WHERE id = $1
  AND disabled_at IS NULL;

-- name: SoftDeleteUser :execrows
UPDATE users
SET disabled_at = now(),
    updated_at = now()
WHERE id = $1
  AND disabled_at IS NULL;

-- name: RevokeUserRefreshFamilies :exec
UPDATE refresh_token_families
SET revoked_at = now(),
    revoked_reason = 'user_disabled'
WHERE user_id = $1
  AND revoked_at IS NULL;

-- name: ReplaceUserRoles :one
UPDATE users
SET roles = sqlc.arg('roles')::text[]::user_role[],
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND disabled_at IS NULL
RETURNING id, email, name, avatar_url, roles::text[] AS roles, created_at, updated_at;
