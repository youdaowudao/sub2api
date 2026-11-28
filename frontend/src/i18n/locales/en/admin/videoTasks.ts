export default {
  videoTasks: {
    title: 'Video Tasks',
    userId: 'User ID',
    duration: '{seconds} sec',
    riskFilter: 'Possible provider cost',
    providerCostBadge: 'The provider may have charged this site',
    riskExplanation: 'Frozen funds have been returned for orders marked with a possible provider cost. This site covers any provider charge.',
    empty: 'No video tasks match these filters',
    loadFailed: 'Video tasks could not be loaded. Please try again.',
    invalidUserId: 'Enter a valid positive user ID.',
    invalidTaskId: 'Enter the full video task ID from this site.',
    columnSettings: 'Column settings',
    deleted: '(Deleted)',
    deletedAccount: 'Deleted account',
    columns: {
      task_id: 'Task ID',
      user_id: 'User',
      group_id: 'Group',
      account_id: 'Provider account',
      model: 'Model',
      upstream_model: 'Provider model',
      seconds: 'Duration',
      resolution: 'Resolution',
      hold_amount: 'Originally frozen',
      status: 'Status',
      billing_state: 'Billing',
      failure_reason: 'Reason',
      upstream_task_id: 'Provider task ID',
      created_at: 'Created',
      settled_at: 'Settled'
    },
    statuses: {
      processing: 'Processing',
      confirmation: 'Awaiting confirmation',
      completed: 'Completed',
      failed: 'Failed'
    },
    billing: {
      held: 'Frozen',
      charged: 'Charged',
      refunded: 'Returned'
    },
    reasons: {
      not_sent: 'Not sent. Funds returned.',
      upstream_rejected: 'Rejected by provider. Funds returned.',
      upstream_failed: 'Video generation failed. Funds returned.',
      unconfirmed_unresolvable: 'Submission unconfirmed. Funds returned.',
      tracking_expired: 'Tracking expired. Funds returned.',
      account_deleted: 'Provider account deleted. Funds returned.',
      api_key_deleted: 'Original key deleted. Funds returned.',
      user_deleted: 'User deleted. Funds returned to the original account.'
    }
  }
}
