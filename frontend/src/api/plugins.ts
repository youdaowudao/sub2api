import { apiClient } from './client'

export type PluginRole = 'admin' | 'user'
export type PluginMount = 'admin' | 'user' | 'shell'

export interface PluginMethod {
  name: string
  role: PluginRole
  access: 'read' | 'write'
}

export interface PluginEntry {
  plugin_id: number
  plugin_key: string
  revision: string
  entry_id: string
  label: string
  mount: PluginMount
  roles: PluginRole[]
  methods: PluginMethod[]
}

export interface PluginBusinessSession {
  url: string
  bridge_token: string
  ui_bridge_version: number
  expires_at: string
  entry: PluginEntry
}

export interface PluginCallResult {
  request_id: string
  payload: Record<string, unknown>
}

export function pluginEntryKey(entry: PluginEntry): string {
  return `${entry.plugin_id}:${entry.entry_id}`
}

export function pluginEntryPath(entry: PluginEntry): string {
  const prefix = entry.mount === 'admin' ? '/admin/extensions' : '/extensions'
  return `${prefix}/${entry.plugin_id}/${encodeURIComponent(entry.entry_id)}`
}

export async function listEntries(signal?: AbortSignal): Promise<PluginEntry[]> {
  const { data } = await apiClient.get<PluginEntry[]>('/plugins/entries', { signal })
  return data
}

export async function createSession(pluginID: number, entryID: string, signal?: AbortSignal): Promise<PluginBusinessSession> {
  const { data } = await apiClient.post<PluginBusinessSession>(
    `/plugins/${pluginID}/entries/${encodeURIComponent(entryID)}/session`, {}, { signal }
  )
  return data
}

export async function call(pluginID: number, method: string, payload: Record<string, unknown>, signal?: AbortSignal): Promise<PluginCallResult> {
  const { data } = await apiClient.post<PluginCallResult>(
    `/plugins/${pluginID}/calls/${encodeURIComponent(method)}`, { payload }, { signal }
  )
  return data
}

export async function action(pluginID: number, method: string, payload: Record<string, unknown>, signal?: AbortSignal): Promise<PluginCallResult> {
  const { data } = await apiClient.post<PluginCallResult>(
    `/plugins/${pluginID}/actions/${encodeURIComponent(method)}`, { payload }, { signal }
  )
  return data
}

export default { listEntries, createSession, call, action }
