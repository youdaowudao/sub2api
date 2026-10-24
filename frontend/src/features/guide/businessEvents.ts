export type GuideActionKind = 'key-create' | 'key-copy'
export interface GuideActionTicket { readonly sessionId: object; readonly kind: GuideActionKind; readonly userId: number; readonly keyId?: number }
export interface BusinessSession {
  sessionId: object
  userId: number
  accepts: (kind: GuideActionKind, keyId?: number) => boolean
  complete: (kind: GuideActionKind, keyId: number) => void
}
let current: BusinessSession | null = null
let issued = new WeakSet<GuideActionTicket>()

export function bindGuideBusinessSession(session: BusinessSession): () => void {
  current = session
  issued = new WeakSet()
  return () => { if (current === session) { current = null; issued = new WeakSet() } }
}
export function beginGuideAction(kind: GuideActionKind, keyId?: number): GuideActionTicket | null {
  try {
    if (!current) return null
    const ticket = Object.freeze({ sessionId: current.sessionId, kind, userId: current.userId, ...(keyId !== undefined ? { keyId } : {}) })
    issued.add(ticket)
    return ticket
  } catch { return null }
}
export function completeGuideAction(ticket: GuideActionTicket | null, result: { keyId: number }): void {
  try {
    if (!ticket || !issued.has(ticket)) return
    issued.delete(ticket)
    if (!current || current.sessionId !== ticket.sessionId || current.userId !== ticket.userId || !Number.isSafeInteger(result.keyId) || result.keyId <= 0) return
    if (ticket.kind === 'key-copy' && ticket.keyId !== result.keyId) return
    if (!current.accepts(ticket.kind, ticket.keyId)) return
    // The successful result is registered synchronously, before KeysView closes its dialog.
    current.complete(ticket.kind, result.keyId)
  } catch { /* Optional guide reporting never changes the business result. */ }
}
