import { describe, expect, it, vi } from 'vitest'
import { beginGuideAction, bindGuideBusinessSession, completeGuideAction } from '../businessEvents'

describe('guide business result tickets', () => {
  it('does nothing without a session and accepts an issued success synchronously once', () => {
    expect(beginGuideAction('key-create')).toBeNull()
    const complete = vi.fn()
    const release = bindGuideBusinessSession({ sessionId: {}, userId: 7, accepts: () => true, complete })
    const ticket = beginGuideAction('key-create')
    completeGuideAction(ticket, { keyId: 10 })
    expect(complete).toHaveBeenCalledWith('key-create', 10)
    completeGuideAction(ticket, { keyId: 10 }); expect(complete).toHaveBeenCalledOnce()
    release(); expect(beginGuideAction('key-copy', 10)).toBeNull()
  })
  it('rejects late results from an exited session even after a new session has started', () => {
    const complete = vi.fn()
    const oldRelease = bindGuideBusinessSession({ sessionId: {}, userId: 7, accepts: () => true, complete })
    const oldTicket = beginGuideAction('key-create')
    oldRelease()
    const release = bindGuideBusinessSession({ sessionId: {}, userId: 7, accepts: () => true, complete })
    oldRelease() // The old owner cannot release a new session.
    completeGuideAction(oldTicket, { keyId: 10 })
    expect(complete).not.toHaveBeenCalled()
    completeGuideAction(beginGuideAction('key-create'), { keyId: 11 })
    expect(complete).toHaveBeenCalledWith('key-create', 11)
    release()
  })
  it('checks the account, current step and selected key ID rather than clipboard or DOM success hints', () => {
    let userId = 7, selected = 10, accepting = true
    const complete = vi.fn()
    const session = { sessionId: {}, get userId() { return userId }, accepts: (_kind: string, id?: number) => accepting && id === selected, complete }
    const release = bindGuideBusinessSession(session)
    const switched = beginGuideAction('key-copy', 10); userId = 8
    completeGuideAction(switched, { keyId: 10 }); expect(complete).not.toHaveBeenCalled()
    userId = 7
    const anotherRow = beginGuideAction('key-copy', 11)
    completeGuideAction(anotherRow, { keyId: 11 }); expect(complete).not.toHaveBeenCalled()
    const delayed = beginGuideAction('key-copy', 10); selected = 11
    completeGuideAction(delayed, { keyId: 10 }); expect(complete).not.toHaveBeenCalled()
    const wrongStep = beginGuideAction('key-copy', 11); accepting = false
    completeGuideAction(wrongStep, { keyId: 11 }); expect(complete).not.toHaveBeenCalled()
    accepting = true
    const mismatched = beginGuideAction('key-copy', 11)
    completeGuideAction(mismatched, { keyId: 12 }); expect(complete).not.toHaveBeenCalled()
    completeGuideAction(beginGuideAction('key-copy', 11), { keyId: 11 })
    expect(complete).toHaveBeenCalledOnce()
    release()
  })
})
