import { FLOW_VERSION } from './steps'

export interface GuideUser { id: number; role: string }
export interface RegistrationRecord { version: string; userId: number; registrationId: string; expiresAt: number }
export const registrationKey = 's2o:registration:v1'
export const progressKey = 's2o:progress:v1'
export const suppressedKey = 's2o:visit-suppressed:v1'
export const historyKey = 's2o:history:v1'
const listeners = new Set<() => void>()

export function readSessionValue(key: string): string | null {
  try { return sessionStorage.getItem(key) } catch { return null }
}
export function writeSessionValue(key: string, value: string | null): void {
  try { value === null ? sessionStorage.removeItem(key) : sessionStorage.setItem(key, value) } catch { /* Business pages keep working without storage. */ }
}
export function clearRegistrationProgress(): void {
  writeSessionValue(registrationKey, null)
  writeSessionValue(progressKey, null)
}
export function isRegistrationRecord(record: unknown, userId: number, now = Date.now()): record is RegistrationRecord {
  const value = record as RegistrationRecord | null
  return !!value && value.version === FLOW_VERSION && value.userId === userId && /^[a-f0-9]{32}$/.test(value.registrationId) && Number.isFinite(value.expiresAt) && now < value.expiresAt && value.expiresAt <= now + 1_800_000
}
export function readGuideRegistration(userId: number): RegistrationRecord | null {
  let record: unknown
  try { record = JSON.parse(readSessionValue(registrationKey) || 'null') } catch { record = null }
  if (isRegistrationRecord(record, userId)) return record
  writeSessionValue(registrationKey, null)
  return null
}
export function recordGuideRegistration(user: GuideUser): void {
  try {
    if (user?.role !== 'user' || !Number.isSafeInteger(user.id) || user.id <= 0) return
    const registrationId = [...crypto.getRandomValues(new Uint8Array(16))].map(value => value.toString(16).padStart(2, '0')).join('')
    const record: RegistrationRecord = { version: FLOW_VERSION, userId: user.id, registrationId, expiresAt: Date.now() + 1_800_000 }
    sessionStorage.setItem(registrationKey, JSON.stringify(record))
    for (const listener of listeners) { try { listener() } catch { /* A guide callback cannot break signup. */ } }
  } catch { /* Registration success and navigation never depend on this optional guide. */ }
}
export function onGuideRegistration(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
