-- 默认 0 保持升级前令牌指纹，显式撤销通过原子递增使旧凭据失效。
ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version BIGINT NOT NULL DEFAULT 0;
