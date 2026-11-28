export default {
  videoTasks: {
    title: '视频任务',
    userId: '用户 ID',
    duration: '{seconds} 秒',
    riskFilter: '本站可能承担上游费用',
    providerCostBadge: '上游可能已扣本站费用',
    riskExplanation: '标有上游费用风险的订单已经退回冻结金额。上游如果扣了款，由本站承担。',
    empty: '没有符合条件的视频任务',
    loadFailed: '视频任务加载失败，请重试。',
    invalidUserId: '用户 ID 须为有效的正整数。',
    invalidTaskId: '请输入完整的本站视频任务号。',
    columnSettings: '列设置',
    deleted: '（已删除）',
    deletedAccount: '已删除账号',
    columns: {
      task_id: '任务号',
      user_id: '用户',
      group_id: '分组',
      account_id: '上游账号',
      model: '模型',
      upstream_model: '上游模型',
      seconds: '时长',
      resolution: '清晰度',
      hold_amount: '原冻结金额',
      status: '状态',
      billing_state: '结算',
      failure_reason: '原因',
      upstream_task_id: '上游任务号',
      created_at: '创建时间',
      settled_at: '结算时间'
    },
    statuses: {
      processing: '进行中',
      confirmation: '待确认',
      completed: '已完成',
      failed: '已失败'
    },
    billing: {
      held: '冻结中',
      charged: '已扣费',
      refunded: '已退回'
    },
    reasons: {
      not_sent: '未发出，已退回',
      upstream_rejected: '上游拒绝，已退回',
      upstream_failed: '视频生成失败，已退回',
      unconfirmed_unresolvable: '提交未确认，已退回',
      tracking_expired: '查询已到期，已退回',
      account_deleted: '上游账号已删除，已退回',
      api_key_deleted: '原 Key 已删除，已退回',
      user_deleted: '用户已删除，退回原账号'
    }
  }
}
