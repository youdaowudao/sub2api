ALTER TABLE groups ADD COLUMN IF NOT EXISTS display_icon VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN IF NOT EXISTS display_category VARCHAR(32) NOT NULL DEFAULT '';
COMMENT ON COLUMN groups.display_icon IS 'Frontend display icon: empty = follow platform, otherwise one of the platform ids that have a dedicated logo';
COMMENT ON COLUMN groups.display_category IS 'Frontend display category: empty = default by platform, otherwise openai/anthropic/gemini/domestic';
