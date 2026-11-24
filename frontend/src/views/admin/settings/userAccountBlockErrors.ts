import { extractApiErrorCode } from '@/utils/apiError'

export function blockWriteErrorMessage(
  error: unknown,
  t: (key: string, params: Record<string, unknown>) => string,
  failureKey: string,
): string | undefined {
  const errors = 'admin.settings.userAccountBlock.errors'
  switch (extractApiErrorCode(error)) {
    case 'ADMIN_COMPLIANCE_ACK_REQUIRED':
      return undefined
    case 'RATE_LIMITED':
      return t(`${errors}.writeRateLimit`, {})
    case 'USER_ACCOUNT_BLOCK_DURATION_READ_FAILED':
      return t(`${errors}.durationReadFailed`, {})
    case 'USER_ACCOUNT_BLOCK_INVALID_ID':
      return t(failureKey, { reason: t(`${errors}.invalidID`, {}) })
    default:
      return t(`${errors}.unconfirmedWrite`, {})
  }
}
