-- 用户创建流程在明确成功出口独立保存完成事实；不回填旧用户或保存邮箱快照。
CREATE TABLE IF NOT EXISTS sub2api_plugin_user_creations (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    signup_source VARCHAR(20) NOT NULL
);
