interface APIErrorLike {
  message?: string
  response?: {
    data?: {
      detail?: string
      message?: string
    }
  }
}

function extractErrorMessage(error: unknown): string {
  const err = (error || {}) as APIErrorLike
  return err.response?.data?.detail || err.response?.data?.message || err.message || ''
}

export function buildAuthErrorMessage(
  error: unknown,
  options: {
    fallback: string
  }
): string {
  const { fallback } = options
  const message = extractErrorMessage(error)
  return message || fallback
}

// Only new-account rejection uses this predicate; other auth errors keep their existing presentation.
export function isRegistrationIPBlocked(error: unknown): boolean {
  const e = error as { reason?: string; code?: string; response?: { data?: { reason?: string; code?: string } } } | null
  return [e?.reason, e?.code, e?.response?.data?.reason, e?.response?.data?.code].includes('REGISTRATION_IP_BLOCKED')
}

export function isRegistrationIPBlockedRedirect(params: URLSearchParams): boolean {
  return ['error', 'error_message', 'error_description'].some(key => params.get(key) === 'REGISTRATION_IP_BLOCKED')
}
