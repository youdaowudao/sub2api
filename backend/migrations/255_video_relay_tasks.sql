CREATE TABLE video_relay_tasks (
    task_id VARCHAR(38) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    model VARCHAR(100) NOT NULL,
    upstream_model VARCHAR(100) NOT NULL,
    seconds INTEGER NOT NULL CHECK (seconds > 0),
    resolution TEXT NOT NULL,
    total_cost DECIMAL(20, 8) NOT NULL CHECK (total_cost >= 0),
    hold_amount DECIMAL(20, 8) NOT NULL CHECK (hold_amount >= 0),
    rate_multiplier DECIMAL(20, 8) NOT NULL CHECK (rate_multiplier >= 0),
    upstream_site_fingerprint VARCHAR(64) NOT NULL,
    upstream_credential_fingerprint VARCHAR(64) NOT NULL,
    upstream_route_fingerprint VARCHAR(64) NOT NULL,
    upstream_task_id VARCHAR(128) NOT NULL DEFAULT '',
    upstream_request_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'submitting'
        CHECK (status IN ('submitting', 'unconfirmed', 'queued', 'in_progress', 'completed', 'failed')),
    billing_state TEXT NOT NULL DEFAULT 'held'
        CHECK (billing_state IN ('held', 'charged', 'refunded')),
    progress INTEGER NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
    error_message TEXT NOT NULL DEFAULT '',
    failure_reason TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    last_lookup_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX video_relay_tasks_site_task_uidx
    ON video_relay_tasks (upstream_site_fingerprint, upstream_task_id)
    WHERE upstream_task_id <> '';

CREATE INDEX video_relay_tasks_site_credential_sent_idx
    ON video_relay_tasks (upstream_site_fingerprint, upstream_credential_fingerprint, sent_at)
    WHERE sent_at IS NOT NULL;
