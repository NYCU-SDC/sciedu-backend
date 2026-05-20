CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

DO $$
BEGIN
    CREATE TYPE user_role AS ENUM ('STUDENT', 'EXPERIMENTER', 'ADMIN');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- Internal users returned by the Users API and referenced by auth flows.
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_roles_check,
    ALTER COLUMN email TYPE CITEXT,
    ALTER COLUMN name TYPE TEXT,
    ALTER COLUMN roles TYPE user_role[] USING roles::TEXT[]::user_role[],
    ADD COLUMN last_login_at TIMESTAMPTZ,
    ADD COLUMN disabled_at TIMESTAMPTZ,
    ADD CONSTRAINT users_name_not_empty CHECK (btrim(name) <> ''),
    ADD CONSTRAINT users_roles_not_empty CHECK (cardinality(roles) > 0);

-- OAuth provider identities linked to internal users.
CREATE TABLE oauth_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Internal user that owns this provider identity.
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    provider TEXT NOT NULL,

    -- Stable provider subject identifier, such as the OIDC sub claim.
    provider_user_id TEXT NOT NULL,

    provider_email CITEXT NOT NULL,

    email_verified BOOLEAN NOT NULL DEFAULT false,

    last_login_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT oauth_accounts_provider_not_empty CHECK (btrim(provider) <> ''),
    CONSTRAINT oauth_accounts_provider_user_id_not_empty CHECK (btrim(provider_user_id) <> ''),
    CONSTRAINT oauth_accounts_provider_identity_unique UNIQUE (provider, provider_user_id)
);

-- One login session and its non-sliding refresh-token rotation chain.
CREATE TABLE refresh_token_families (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Internal user that owns this refresh token family.
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Provider account used to create this family. Nullable for dev/non-OAuth login methods.
    oauth_account_id UUID REFERENCES oauth_accounts(id) ON DELETE SET NULL,

    -- Absolute expiry for the whole family. Refresh does not extend this value.
    expires_at TIMESTAMPTZ NOT NULL,

    revoked_at TIMESTAMPTZ,

    revoked_reason TEXT,

    reuse_detected_at TIMESTAMPTZ,

    last_used_at TIMESTAMPTZ,

    ip_address INET,

    user_agent TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT refresh_token_families_identity_unique UNIQUE (id, user_id),
    CONSTRAINT refresh_token_families_expire_after_created CHECK (expires_at > created_at),
    CONSTRAINT refresh_token_families_revoked_after_created CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CONSTRAINT refresh_token_families_reuse_after_created CHECK (reuse_detected_at IS NULL OR reuse_detected_at >= created_at),
    CONSTRAINT refresh_token_families_last_used_after_created CHECK (last_used_at IS NULL OR last_used_at >= created_at),
    CONSTRAINT refresh_token_families_revoked_reason_known CHECK (
        revoked_reason IS NULL
        OR revoked_reason IN ('logout', 'reuse_detected', 'admin_revoked', 'user_disabled')
    )
);

-- Individual opaque refresh tokens stored as hashes for rotation and reuse detection.
CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    family_id UUID NOT NULL,

    user_id UUID NOT NULL,

    token_hash BYTEA NOT NULL UNIQUE,

    rotated_from_token_id UUID REFERENCES refresh_tokens(id) ON DELETE SET NULL,

    -- Current usable token marker. Only one token per family may be current.
    is_current BOOLEAN NOT NULL DEFAULT true,

    issued_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    used_at TIMESTAMPTZ,

    revoked_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT refresh_tokens_family_user_fk
        FOREIGN KEY (family_id, user_id)
        REFERENCES refresh_token_families(id, user_id)
        ON DELETE CASCADE,
    CONSTRAINT refresh_tokens_hash_sha256_length CHECK (length(token_hash) = 32),
    CONSTRAINT refresh_tokens_used_after_issued CHECK (used_at IS NULL OR used_at >= issued_at),
    CONSTRAINT refresh_tokens_revoked_after_issued CHECK (revoked_at IS NULL OR revoked_at >= issued_at)
);

CREATE UNIQUE INDEX idx_refresh_tokens_one_current_per_family
ON refresh_tokens(family_id)
WHERE is_current = true;

-- Short-lived OAuth state records for callback validation, redirect preservation, and PKCE.
CREATE TABLE oauth_login_states (
    -- SHA-256 hash of the OAuth state value used as the callback lookup key.
    state_hash BYTEA PRIMARY KEY,

    provider TEXT NOT NULL,

    -- Short-lived one-shot PKCE verifier. Must not be logged.
    code_verifier TEXT NOT NULL,

    redirect_url TEXT NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    used_at TIMESTAMPTZ,

    -- Client IP address observed when this state was created.
    ip_address INET,

    -- Client user-agent observed when this state was created.
    user_agent TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT oauth_login_states_hash_sha256_length CHECK (length(state_hash) = 32),
    CONSTRAINT oauth_login_states_provider_not_empty CHECK (btrim(provider) <> ''),
    CONSTRAINT oauth_login_states_code_verifier_length CHECK (char_length(code_verifier) BETWEEN 43 AND 128),
    CONSTRAINT oauth_login_states_expire_after_created CHECK (expires_at > created_at),
    CONSTRAINT oauth_login_states_used_after_created CHECK (used_at IS NULL OR used_at >= created_at)
);

CREATE INDEX idx_oauth_accounts_user_id
ON oauth_accounts(user_id);

CREATE INDEX idx_refresh_token_families_user_id
ON refresh_token_families(user_id);

CREATE INDEX idx_refresh_token_families_active
ON refresh_token_families(user_id)
WHERE revoked_at IS NULL;

CREATE INDEX idx_refresh_tokens_family_id
ON refresh_tokens(family_id);

CREATE INDEX idx_oauth_login_states_expires_at
ON oauth_login_states(expires_at);
