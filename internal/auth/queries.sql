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

-- name: CreateOAuthLoginState :exec
INSERT INTO oauth_login_states (
    state_hash,
    provider,
    code_verifier,
    redirect_url,
    expires_at,
    ip_address,
    user_agent,
    created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ConsumeOAuthLoginState :one
UPDATE oauth_login_states
SET used_at = $2
WHERE state_hash = $1
  AND used_at IS NULL
  AND expires_at > $2
RETURNING state_hash, provider, code_verifier, redirect_url, expires_at, used_at,
    ip_address, user_agent, created_at;

-- name: GetOAuthAccountByProviderUserID :one
SELECT id, user_id, provider, provider_user_id, provider_email, email_verified,
    last_login_at, created_at, updated_at
FROM oauth_accounts
WHERE provider = $1
  AND provider_user_id = $2;

-- name: CreateOAuthUser :one
INSERT INTO users (
    email,
    name,
    avatar_url,
    roles,
    last_login_at,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, ARRAY['STUDENT']::user_role[], $4, $4, $4)
RETURNING id, email, name, avatar_url, roles, last_login_at, disabled_at, created_at, updated_at;

-- name: CreateOAuthAccount :one
INSERT INTO oauth_accounts (
    user_id,
    provider,
    provider_user_id,
    provider_email,
    email_verified,
    last_login_at,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $6, $6)
RETURNING id, user_id, provider, provider_user_id, provider_email, email_verified,
    last_login_at, created_at, updated_at;

-- name: TouchOAuthAccount :exec
UPDATE oauth_accounts
SET provider_email = $3,
    email_verified = $4,
    last_login_at = $5,
    updated_at = $5
WHERE id = $1
  AND user_id = $2;

-- name: TouchUserLogin :exec
UPDATE users
SET last_login_at = $2,
    updated_at = $2
WHERE id = $1
  AND disabled_at IS NULL;

-- name: GetUserProfile :one
SELECT id, name, email
FROM users
WHERE id = $1
  AND disabled_at IS NULL;
