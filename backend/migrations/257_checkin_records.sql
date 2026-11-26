CREATE TABLE IF NOT EXISTS checkin_records (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    checkin_date DATE NOT NULL,
    amount DECIMAL(20,8) NOT NULL,
    user_tier VARCHAR(16) NOT NULL,
    verification VARCHAR(16) NOT NULL,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_checkin_records_user_date ON checkin_records (user_id, checkin_date);
CREATE INDEX IF NOT EXISTS idx_checkin_records_date_ip ON checkin_records (checkin_date, ip);
