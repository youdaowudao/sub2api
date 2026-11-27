-- 余额不足提醒改为用户主动开启；保留阈值和通知邮箱。
ALTER TABLE users ALTER COLUMN balance_notify_enabled SET DEFAULT false;
UPDATE users SET balance_notify_enabled = false WHERE balance_notify_enabled = true;
