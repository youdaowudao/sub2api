export default {
  "title": "签到管理",
  "description": "查看签到发放、用户、明细和配置",
  "overview": "概览",
  "users": "用户统计",
  "records": "签到明细",
  "ips": "IP 聚合",
  "settings": "签到设置",
  "loadFailed": "加载失败，请重试",
  "empty": "暂无数据",
  "retry": "重试",
  "save": "保存",
  "saved": "签到配置已保存",
  "saveFailed": "保存失败",
  "invalidConfig": "签到配置异常，请重新保存",
  "captchaUnavailable": "未找到可用的人机验证配置，新用户将无法签到",
  "captchaProvider": "当前验证服务商",
  "none": "无",
  "today": "今日",
  "yesterday": "昨日",
  "last_7_days": "近 7 天",
  "last_30_days": "近 30 天",
  "all_time": "累计",
  "amount": "发放金额",
  "usersCount": "签到人数",
  "email_verifications": "邮箱验证签到次数",
  "trend": "每日签到趋势",
  "days": "近 {count} 天",
  "search": "邮箱 / 用户名 / ID",
  "all": "全部",
  "date_from": "起始日期",
  "date_to": "结束日期",
  "min_users": "最少账号数",
  "viewRecords": "查看明细",
  "manageUser": "用户管理",
  "user_id": "用户 ID",
  "email": "邮箱",
  "username": "用户名",
  "status": "状态",
  "r": "签到口径累计充值",
  "tier": "用户类型",
  "total_days": "签到天数 / 次数",
  "total_amount": "累计领取",
  "first_checkin": "首次签到",
  "last_checkin": "最近签到",
  "last_ip": "最近签到 IP",
  "shared_ip_users": "同 IP 其他账号数",
  "pending_email": "下一次需邮箱验证",
  "actions": "操作",
  "date": "签到日期",
  "created_at": "签到时间",
  "ip": "IP",
  "verification": "验证方式",
  "first_date": "首次日期",
  "last_date": "末次日期",
  "emails": "账号邮箱（最多 5 个）",
  "yes": "是",
  "no": "否",
  "filter": "筛选",
  "groups": {
    "basic": "基础",
    "veteran": "老用户",
    "reward": "新用户奖励",
    "protection": "新用户防刷",
    "email": "邮箱验证"
  },
  "tiers": {
    "veteran": "老用户",
    "new_funded": "新用户 · 有入账",
    "new_unfunded": "新用户 · 无入账"
  },
  "verifications": {
    "none": "无",
    "captcha": "人机验证",
    "email": "邮箱验证"
  },
  "fields": {
    "enabled": {
      "label": "签到总开关",
      "help": "关闭后首页不显示签到，原仪表盘布局恢复。"
    },
    "veteran_threshold": {
      "label": "老用户门槛（元）",
      "help": "按签到口径累计充值判定，0 表示 R ≥ 0 即归为老用户。"
    },
    "veteran_min": {
      "label": "老用户最小奖励（元）",
      "help": "每次随机奖励下限，0 允许零奖励。"
    },
    "veteran_max": {
      "label": "老用户最大奖励（元）",
      "help": "每次随机奖励上限，不得小于下限。"
    },
    "newcomer_min": {
      "label": "新用户最小奖励（元）",
      "help": "累计金额未达指定金额前随机奖励下限。"
    },
    "newcomer_max": {
      "label": "新用户最大奖励（元）",
      "help": "累计金额未达指定金额前随机奖励上限，不得小于下限。"
    },
    "newcomer_cap": {
      "label": "新用户累计奖励界线（元）",
      "help": "累计达到此金额后改发固定小额；0 表示首次即固定。"
    },
    "newcomer_after_cap": {
      "label": "达到界线后的固定奖励（元）",
      "help": "每次固定发放该金额；0 表示不发余额。"
    },
    "newcomer_captcha": {
      "label": "新用户人机验证",
      "help": "关闭后新用户签到不需要人机验证。"
    },
    "min_account_age_days": {
      "label": "最少注册天数",
      "help": "满指定天数才可签到；0 表示不限。"
    },
    "ip_daily_limit": {
      "label": "同 IP 每日新用户上限",
      "help": "每天同 IP 最多签到账号数；0 表示不限。"
    },
    "email_verify_enabled": {
      "label": "邮箱验证总开关",
      "help": "关闭后任何用户都无需签到邮箱验证。"
    },
    "email_verify_first": {
      "label": "首次邮箱验证前签到次数",
      "help": "无入账新用户签满指定次数后，下次需要验证。"
    },
    "email_verify_every": {
      "label": "复验间隔",
      "help": "首次验证后允许签到的次数；0 表示只验一次。"
    },
    "email_verify_skip_reserved": {
      "label": "占位邮箱跳过验证",
      "help": "关闭后需先绑定真实邮箱，再继续签到。"
    }
  }
}
