ALTER TABLE video_relay_tasks ADD COLUMN next_check_at TIMESTAMPTZ DEFAULT NOW();

UPDATE video_relay_tasks
SET next_check_at = NULL
WHERE NOT (
    billing_state = 'held' OR (
        billing_state = 'refunded' AND sent_at IS NOT NULL AND settled_at IS NOT NULL
        AND failure_reason IN ('unconfirmed_unresolvable', 'api_key_deleted', 'user_deleted')
        AND NOT (upstream_task_id COLLATE "C" ~ '^[A-Za-z0-9_.-]{1,128}$' AND upstream_task_id ~ '[^.]')
    )
);

CREATE INDEX video_relay_tasks_next_check_idx
    ON video_relay_tasks (next_check_at, task_id)
    WHERE next_check_at IS NOT NULL AND (
        billing_state = 'held' OR (
            billing_state = 'refunded' AND sent_at IS NOT NULL AND settled_at IS NOT NULL
            AND failure_reason IN ('unconfirmed_unresolvable', 'api_key_deleted', 'user_deleted')
            AND NOT (upstream_task_id COLLATE "C" ~ '^[A-Za-z0-9_.-]{1,128}$' AND upstream_task_id ~ '[^.]')
        )
    );
