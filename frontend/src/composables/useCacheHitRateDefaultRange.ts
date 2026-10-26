import { watch } from 'vue'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { getCacheHitRateRangeDates } from '@/utils/dateRange'

export function useCacheHitRateDefaultRange(onChange: () => void) {
  const settings = useAdminSettingsStore()
  const dates = { get value() { return getCacheHitRateRangeDates(settings.cacheHitRateDefaultRange) } }
  watch(() => settings.cacheHitRateDefaultRange, onChange)
  void settings.fetch()
  return { dates }
}
