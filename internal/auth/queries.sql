-- name: CreateRefreshTokenFamily :one
INSERT INTO refresh_token_families (
    user_id,
    oauth_account_id,
    expires_at,
    ip_address,
    user_agent,
    created_at
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, user_id, oauth_account_id, expires_at, revoked_at, revoked_reason,
    reuse_detected_at, last_used_at, ip_address, user_agent, created_at;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (
    family_id,
    user_id,
    token_hash,
    rotated_from_token_id,
    is_current,
    issued_at,
    created_at
)
VALUES ($1, $2, $3, $4, true, $5, $5)
RETURNING id, family_id, user_id, token_hash, rotated_from_token_id, is_current,
    issued_at, used_at, revoked_at, created_at;

-- name: GetRefreshTokenByHash :one
SELECT
    rt.id,
    rt.family_id,
    rt.user_id,
    rt.token_hash,
    rt.rotated_from_token_id,
    rt.is_current,
    rt.issued_at,
    rt.used_at,
    rt.revoked_at,
    rt.created_at,
    rtf.expires_at AS family_expires_at,
    rtf.revoked_at AS family_revoked_at
FROM refresh_tokens rt
JOIN refresh_token_families rtf ON rtf.id = rt.family_id
WHERE rt.token_hash = $1;

-- name: MarkRefreshTokenUsed :exec
UPDATE refresh_tokens
SET used_at = $2,
    is_current = false
WHERE id = $1
  AND used_at IS NULL
  AND revoked_at IS NULL;

-- name: InsertRotatedRefreshToken :one
INSERT INTO refresh_tokens (
    family_id,
    user_id,
    token_hash,
    rotated_from_token_id,
    is_current,
    issued_at,
    created_at
)
VALUES ($1, $2, $3, $4, true, $5, $5)
RETURNING id, family_id, user_id, token_hash, rotated_from_token_id, is_current,
    issued_at, used_at, revoked_at, created_at;

-- name: TouchRefreshTokenFamily :exec
UPDATE refresh_token_families
SET last_used_at = $2
WHERE id = $1;

-- name: RevokeRefreshFamilyByTokenHash :exec
UPDATE refresh_token_families
SET revoked_at = $2,
    revoked_reason = $3
WHERE id = (
    SELECT family_id
    FROM refresh_tokens
    WHERE token_hash = $1
);

-- name: MarkRefreshFamilyReuseDetected :exec
UPDATE refresh_token_families
SET revoked_at = $2,
    revoked_reason = 'reuse_detected',
    reuse_detected_at = $2
WHERE id = $1
  AND revoked_at IS NULL;
