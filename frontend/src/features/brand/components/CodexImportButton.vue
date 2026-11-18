<template>
  <button
    type="button"
    class="codex-import-btn"
    :class="stacked ? 'codex-import-btn-stacked' : compact ? 'codex-import-btn-compact' : ''"
    :title="t('keys.importAssistantHint', { app: appLabel })"
    :aria-label="buttonLabel"
    :data-app="app"
    data-testid="codex-import"
    @click="launch"
  >
    <!-- stacked：表格操作列里用，两行字，和旁边的图标按钮一样高 -->
    <template v-if="stacked">
      <span>{{ t('keys.importToCodexLine1') }}</span>
      <span>{{ appLabel }}</span>
    </template>
    <template v-else>
      <Icon name="bolt" size="sm" />
      <span>{{ buttonLabel }}</span>
    </template>
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import type { ApiKey } from '@/types'
import { buildQuickConnectLink, type ImportApp } from '../config'
import { getCurrentQuickImport } from '../quickKey'

const props = withDefaults(defineProps<{ apiKey?: ApiKey; app?: ImportApp; compact?: boolean; stacked?: boolean }>(), { app: 'codex' })
const emit = defineEmits<{ 'missing-key': [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const appLabel = computed(() => props.app === 'image' ? t('keys.importImageApp') : props.app === 'claude' ? 'Claude' : 'Codex')
const buttonLabel = computed(() => {
  if (props.app === 'image') return t('keys.importToImage')
  return t(props.app === 'claude' ? 'keys.importToClaude' : 'keys.importToCodex')
})

async function launch() {
  const selected = props.apiKey
  if (!selected?.key) {
    emit('missing-key')
    return
  }
  const target = props.app
  try {
    const { key, app } = await getCurrentQuickImport(selected.id)
    if (props.apiKey?.id !== selected.id || app !== target || key.group_id !== selected.group_id || key.key !== selected.key) {
      appStore.showError(t('keys.importChanged'))
      return
    }
    window.location.href = buildQuickConnectLink(key.key, key.name, app)
  } catch {
    appStore.showError(t('keys.importCheckFailed'))
  }
}
</script>

<style scoped>
.codex-import-btn {
  display: inline-flex;
  justify-content: center;
  align-items: center;
  gap: 6px;
  height: 36px;
  padding: 0 16px;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 600;
  white-space: nowrap;
  color: #fff;
  background: linear-gradient(135deg, #0d9488 0%, #0891b2 55%, #2563eb 100%);
  box-shadow: 0 8px 18px -10px rgba(8, 145, 178, 0.9);
  transition: filter 0.2s, transform 0.2s;
}

.codex-import-btn:hover {
  filter: brightness(1.07);
  transform: translateY(-1px);
}

.codex-import-btn-compact {
  height: 30px;
  padding: 0 11px;
  font-size: 12px;
  border-radius: 8px;
}
.codex-import-btn-stacked {
  flex-direction: column;
  max-width: 100%;
  white-space: normal;
  gap: 0;
  height: auto;
  padding: 4px 6px;
  border-radius: 8px;
  font-size: 11px;
  line-height: 1.25;
}
</style>
