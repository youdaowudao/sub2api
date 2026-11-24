import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export type VideoTaskStatus = 'processing' | 'confirmation' | 'completed' | 'failed'
export type VideoTaskBillingState = 'held' | 'charged' | 'refunded'
export type VideoTaskFailureReason = '' | 'not_sent' | 'upstream_rejected' | 'upstream_failed' |
  'unconfirmed_unresolvable' | 'tracking_expired' | 'account_deleted' | 'api_key_deleted' | 'user_deleted'

export interface VideoTask {
  task_id: string
  user_id: number
  user_email: string
  group_id: number
  account_id: number
  model: string
  upstream_model: string
  seconds: number
  resolution: string
  hold_amount: number
  status: VideoTaskStatus
  billing_state: VideoTaskBillingState
  failure_reason: VideoTaskFailureReason
  upstream_task_id: string
  created_at: string
  settled_at: string | null
  risk: boolean
}

export interface VideoTasksQuery {
  page: number
  page_size: number
  status?: VideoTaskStatus
  user_id?: string
  task_id?: string
  risk_only?: true
}

async function list(params: VideoTasksQuery): Promise<PaginatedResponse<VideoTask>> {
  const { data } = await apiClient.get<PaginatedResponse<VideoTask>>('/admin/video-tasks', { params })
  return data
}

export default { list }
