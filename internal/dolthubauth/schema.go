package dolthubauth

const bootstrapSchema = `
CREATE TABLE IF NOT EXISTS connections (
    connection_id CHAR(26) PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    rig_handle TEXT NOT NULL,
    wastelands JSONB NOT NULL DEFAULT '[]'::jsonb,
    status TEXT NOT NULL CHECK (status IN ('active', 'invalid', 'degraded')),
    credential_ciphertext BYTEA NOT NULL,
    credential_key_version TEXT NOT NULL,
    credential_encryption_backend TEXT NOT NULL,
    credential_version INTEGER NOT NULL DEFAULT 1,
    record_version INTEGER NOT NULL DEFAULT 0,
    last_validated_at TIMESTAMPTZ,
    last_validation_error_code TEXT CHECK (
        last_validation_error_code IN (
            'invalid_key',
            'expired_key',
            'revoked_key',
            'upstream_unreachable',
            'rate_limited',
            'kms_unavailable',
            'proxy_unauthorized'
        )
    ),
    last_proxy_error_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, environment, subject_id),
    CHECK (jsonb_typeof(wastelands) = 'array')
);

CREATE INDEX IF NOT EXISTS connections_subject_lookup_idx
    ON connections (tenant_id, environment, subject_id);

CREATE TABLE IF NOT EXISTS connect_tokens (
    connect_token_mac BYTEA PRIMARY KEY,
    redeem_secret_mac BYTEA NOT NULL,
    tenant_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    approved_metadata JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS connect_tokens_reap_idx
    ON connect_tokens (expires_at)
    WHERE used_at IS NULL;

CREATE TABLE IF NOT EXISTS service_request_nonces (
    key_id TEXT NOT NULL,
    nonce TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (key_id, nonce)
);

CREATE INDEX IF NOT EXISTS service_request_nonces_reap_idx
    ON service_request_nonces (expires_at);

CREATE TABLE IF NOT EXISTS audit_log (
    audit_id BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    connection_id CHAR(26),
    actor_type TEXT NOT NULL,
    action TEXT NOT NULL,
    outcome TEXT NOT NULL,
    error_code TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS audit_log_scope_idx
    ON audit_log (tenant_id, environment, occurred_at DESC);
`
