import { apiClient } from './client'

export type CheckinStatus = 'available' | 'done' | 'too_new' | 'bind_email' | 'unavailable'
export type CheckinProvider = 'turnstile' | 'tencent' | 'aliyun'
export interface CheckinPublic {
  site_key?: string
  app_id?: string
  region?: string
  scene_id?: string
  prefix?: string
}
export interface CheckinRecord { date: string; amount: number; time: string }
export interface CheckinMonth { month: string; month_amount: number; records: CheckinRecord[] }
export interface CheckinStats { total_days: number; total_amount: number; streak: number }
export interface CheckinDetails extends CheckinMonth {
  enabled: true
  status: CheckinStatus
  today: string
  seconds_until_reset: number
  registered_month: string
  needs_captcha: boolean
  needs_email: boolean
  captcha_provider?: CheckinProvider
  captcha_public?: CheckinPublic
  masked_email?: string
  available_at?: string
  today_amount: number | null
  stats: CheckinStats
}
export type CheckinState = { enabled: false } | { enabled: true; status: 'unavailable' } | CheckinDetails
export interface CheckinProof { captcha_token?: string; captcha_randstr?: string; email_code?: string }
export interface CheckinResult extends CheckinStats {
  amount: number; balance: number; checkin_date: string; time: string; month: string; month_amount: number
}
export const checkinAPI = {
  async get(month?: string): Promise<CheckinState> {
    const { data } = await apiClient.get<CheckinState>('/user/checkin', { params: month ? { month } : {} })
    return data
  },
  async records(month: string): Promise<CheckinMonth> {
    const { data } = await apiClient.get<CheckinMonth>('/user/checkin/records', { params: { month } })
    return data
  },
  async submit(proof: CheckinProof): Promise<CheckinResult> {
    const { data } = await apiClient.post<CheckinResult>('/user/checkin', proof)
    return data
  },
  async sendCode(): Promise<{ masked_email: string; cooldown_seconds: number }> {
    const { data } = await apiClient.post<{ masked_email: string; cooldown_seconds: number }>('/user/checkin/email-code')
    return data
  },
}
