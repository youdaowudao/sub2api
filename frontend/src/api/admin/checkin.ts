import { apiClient } from '../client'
import type { BasePaginationResponse } from '@/types'

export interface CheckinConfig {
  enabled: boolean
  veteran_threshold: number
  veteran_min: number
  veteran_max: number
  newcomer_min: number
  newcomer_max: number
  newcomer_cap: number
  newcomer_after_cap: number
  newcomer_captcha: boolean
  min_account_age_days: number
  ip_daily_limit: number
  email_verify_enabled: boolean
  email_verify_first: number
  email_verify_every: number
  email_verify_skip_reserved: boolean
}
export type CheckinTier = 'veteran' | 'new_funded' | 'new_unfunded'
export interface CheckinConfigResponse { config: CheckinConfig; config_valid: boolean; captcha_provider: string; captcha_available: boolean }
export interface CheckinPeriod { amount: number; users: number; tiers: Partial<Record<CheckinTier, { amount: number; users: number }>> }
export interface CheckinOverview {
  today: CheckinPeriod; yesterday: CheckinPeriod; last_7_days: CheckinPeriod; last_30_days: CheckinPeriod; all_time: CheckinPeriod
  email_verifications: number; trend: Array<{ date: string; users: number; amount: number }>
}
export interface CheckinAdminUser {
  user_id: number; email: string; username: string; status: string; r: number; tier: CheckinTier
  total_days: number; total_amount: number; first_checkin: string; last_checkin: string; last_ip: string
  shared_ip_users: number; email_verifications: number; pending_email: boolean
}
export interface CheckinAdminRecord {
  id: number; user_id: number; email: string; amount: number; date: string; created_at: string; tier: CheckinTier; ip: string; verification: string
}
export interface CheckinAdminIP { ip: string; users: number; total_days: number; total_amount: number; first_date: string; last_date: string; emails: string[] }
export interface PageParams { page: number; page_size: number }
export interface CheckinUserParams extends PageParams { search: string; tier: string; sort: string; order: 'asc' | 'desc' }
export interface CheckinRecordParams extends PageParams { date_from: string; date_to: string; search: string; ip: string; tier: string; verification: string }
export interface CheckinIPParams extends PageParams { date_from: string; date_to: string; min_users: number }

async function get<T>(path: string, params?: object): Promise<T> {
  const { data } = await apiClient.get<T>('/admin/checkin'+path, { params })
  return data
}
export const adminCheckinAPI = {
  config: () => get<CheckinConfigResponse>('/config'),
  async save(config: CheckinConfig): Promise<CheckinConfig> {
    const { data } = await apiClient.put<CheckinConfig>('/admin/checkin/config',config)
    return data
  },
  overview: (days: number) => get<CheckinOverview>('/overview',{days}),
  users: (params: CheckinUserParams) => get<BasePaginationResponse<CheckinAdminUser>>('/users',params),
  records: (params: CheckinRecordParams) => get<BasePaginationResponse<CheckinAdminRecord>>('/records',params),
  ips: (params: CheckinIPParams) => get<BasePaginationResponse<CheckinAdminIP>>('/ips',params),
}
