import { apiClient } from '../client'

export interface ForkEmailSignupFilterConfig {
  enabled: boolean
  trusted_domains: string[]
  time_window_enabled: boolean
  window_start: string
  window_end: string
}

export interface ForkEmailSignupFilterResponse extends ForkEmailSignupFilterConfig {
  server_timezone: string
  config_invalid: boolean
}

export async function getForkEmailSignupFilter(): Promise<ForkEmailSignupFilterResponse> {
  const { data } = await apiClient.get<ForkEmailSignupFilterResponse>('/admin/fork/email-signup-filter')
  return data
}

export async function updateForkEmailSignupFilter(
  payload: ForkEmailSignupFilterConfig,
): Promise<ForkEmailSignupFilterResponse> {
  const { data } = await apiClient.put<ForkEmailSignupFilterResponse>('/admin/fork/email-signup-filter', payload)
  return data
}
