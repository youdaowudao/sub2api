export default {
  "title": "Check-in management",
  "description": "Review rewards, users, records and settings",
  "overview": "Overview",
  "users": "Users",
  "records": "Records",
  "ips": "IP summary",
  "settings": "Settings",
  "loadFailed": "Loading failed. Try again.",
  "empty": "No data",
  "retry": "Retry",
  "save": "Save",
  "saved": "Settings saved",
  "saveFailed": "Save failed",
  "invalidConfig": "Invalid check-in configuration. Save corrected settings.",
  "captchaUnavailable": "No usable human verification configuration; new users cannot check in.",
  "captchaProvider": "Current verification provider",
  "none": "None",
  "today": "Today",
  "yesterday": "Yesterday",
  "last_7_days": "Last 7 days",
  "last_30_days": "Last 30 days",
  "all_time": "All time",
  "amount": "Rewards issued",
  "usersCount": "Users checked in",
  "email_verifications": "Email-verified check-ins",
  "trend": "Daily check-in trend",
  "days": "Last {count} days",
  "search": "Email / username / ID",
  "all": "All",
  "date_from": "Start date",
  "date_to": "End date",
  "min_users": "Minimum accounts",
  "viewRecords": "View records",
  "manageUser": "Manage user",
  "user_id": "User ID",
  "email": "Email",
  "username": "Username",
  "status": "Status",
  "r": "Check-in recharge total",
  "tier": "User tier",
  "total_days": "Check-ins",
  "total_amount": "Total rewards",
  "first_checkin": "First check-in",
  "last_checkin": "Last check-in",
  "last_ip": "Last check-in IP",
  "shared_ip_users": "Other accounts on shared IP",
  "pending_email": "Email verification due",
  "actions": "Actions",
  "date": "Check-in date",
  "created_at": "Check-in time",
  "ip": "IP",
  "verification": "Verification",
  "first_date": "First date",
  "last_date": "Last date",
  "emails": "Account emails (up to 5)",
  "yes": "Yes",
  "no": "No",
  "filter": "Filter",
  "groups": {
    "basic": "General",
    "veteran": "Veteran users",
    "reward": "New user rewards",
    "protection": "New user protection",
    "email": "Email verification"
  },
  "tiers": {
    "veteran": "Veteran",
    "new_funded": "New · funded",
    "new_unfunded": "New · unfunded"
  },
  "verifications": {
    "none": "None",
    "captcha": "Human verification",
    "email": "Email verification"
  },
  "fields": {
    "enabled": {
      "label": "Enable check-in",
      "help": "When disabled, check-in disappears and the original dashboard layout returns."
    },
    "veteran_threshold": {
      "label": "Veteran threshold (¥)",
      "help": "Uses the check-in recharge total. At 0, R ≥ 0 qualifies."
    },
    "veteran_min": {
      "label": "Veteran minimum reward (¥)",
      "help": "Minimum random reward; 0 allows zero."
    },
    "veteran_max": {
      "label": "Veteran maximum reward (¥)",
      "help": "Maximum random reward, at least the minimum."
    },
    "newcomer_min": {
      "label": "New user minimum (¥)",
      "help": "Minimum random reward before reaching the cap."
    },
    "newcomer_max": {
      "label": "New user maximum (¥)",
      "help": "Maximum random reward before reaching the cap; at least the minimum."
    },
    "newcomer_cap": {
      "label": "New user reward cap (¥)",
      "help": "Use the fixed reward after this total; 0 applies it from the first check-in."
    },
    "newcomer_after_cap": {
      "label": "Fixed reward after cap (¥)",
      "help": "Fixed amount per check-in; 0 issues no balance."
    },
    "newcomer_captcha": {
      "label": "Human verification for new users",
      "help": "When off, new users do not need human verification."
    },
    "min_account_age_days": {
      "label": "Minimum account age (days)",
      "help": "Wait this many days; 0 disables the restriction."
    },
    "ip_daily_limit": {
      "label": "Daily new users per IP",
      "help": "Maximum new accounts per IP per day; 0 disables the limit."
    },
    "email_verify_enabled": {
      "label": "Enable email verification",
      "help": "When off, no user needs email verification for check-in."
    },
    "email_verify_first": {
      "label": "Check-ins before first email verification",
      "help": "Unfunded new users must verify on the next check-in after this many."
    },
    "email_verify_every": {
      "label": "Email re-verification interval",
      "help": "Check-ins allowed between email verifications; 0 means verify once."
    },
    "email_verify_skip_reserved": {
      "label": "Skip reserved email addresses",
      "help": "If off, bind a real email address before proceeding."
    }
  }
}
