SET LOCAL lock_timeout = '5s';
ALTER TABLE channels ADD COLUMN IF NOT EXISTS model_mapping_order JSONB NULL;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS model_routing_order JSONB NULL;
COMMENT ON COLUMN channels.model_mapping_order IS '渠道模型映射在管理界面的显示顺序，仅管理员显示用，NULL 表示无顺序';
COMMENT ON COLUMN groups.model_routing_order IS '分组模型路由在管理界面的显示顺序，仅管理员显示用，NULL 表示无顺序';
