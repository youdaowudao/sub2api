export const OPENAI_VIDEO_RESOLUTIONS = ['480p', '720p', '1080p'] as const
export const OPENAI_VIDEO_MAX_SECONDS = 2_147_483_647

type VideoResolution = (typeof OPENAI_VIDEO_RESOLUTIONS)[number]

export interface OpenAIVideoSettings {
  resolutions: VideoResolution[]
  min_seconds: number
  max_seconds: number
  default_seconds: number
}

export function defaultOpenAIVideoSettings(): OpenAIVideoSettings {
  return { resolutions: ['480p'], min_seconds: 5, max_seconds: 30, default_seconds: 5 }
}

export function readOpenAIVideoSettings(raw: unknown): OpenAIVideoSettings {
  if (!raw || typeof raw !== 'object') return defaultOpenAIVideoSettings()
  const value = raw as Partial<OpenAIVideoSettings>
  const resolutions = Array.isArray(value.resolutions)
    ? OPENAI_VIDEO_RESOLUTIONS.filter((item) => value.resolutions?.includes(item))
    : []
  const min = value.min_seconds
  const max = value.max_seconds
  const initial = value.default_seconds
  if (
    resolutions.length === 0 || typeof min !== 'number' || typeof max !== 'number' ||
    typeof initial !== 'number' || ![min, max, initial].every(Number.isInteger) ||
    min < 1 || min > initial || initial > max || max > OPENAI_VIDEO_MAX_SECONDS
  ) {
    return defaultOpenAIVideoSettings()
  }
  return { resolutions, min_seconds: min, max_seconds: max, default_seconds: initial }
}
