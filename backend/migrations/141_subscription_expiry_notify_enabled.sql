-- 订阅到期提醒邮件开关，新安装默认关闭。
INSERT INTO settings (key, value, updated_at)
VALUES ('subscription_expiry_notify_enabled', 'false', NOW())
ON CONFLICT (key) DO NOTHING;
