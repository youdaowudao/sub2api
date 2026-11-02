<template>
  <button
    type="button"
    class="codex-import-btn"
    :class="stacked ? 'codex-import-btn-stacked' : compact ? 'codex-import-btn-compact' : ''"
    :title="t('keys.importToCodex')"
    data-testid="codex-import"
    @click="launch"
  >
    <!-- stacked：表格操作列里用，两行字，和旁边的图标按钮一样高 -->
    <template v-if="stacked">
      <span>{{ t('keys.importToCodexLine1') }}</span>
      <span>Codex</span>
    </template>
    <template v-else>
      <Icon name="bolt" size="sm" />
      <span>{{ t('keys.importToCodex') }}</span>
    </template>
  </button>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { buildQuickConnectLink } from '../config'

const props = defineProps<{ apiKey?: string; name?: string; compact?: boolean; stacked?: boolean }>()
const emit = defineEmits<{ 'missing-key': [] }>()
const { t } = useI18n()

/** 打开接入助手的导入链接；没装助手时浏览器不会有反应，由页面上的「没反应？」兜底。
 *  还没有可用密钥时不跳链接，交给页面处理（例如去创建密钥） */
function launch() {
  if (!props.apiKey) {
    emit('missing-key')
    return
  }
  window.location.href = buildQuickConnectLink(props.apiKey, props.name)
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
  gap: 0;
  height: auto;
  padding: 4px 6px;
  border-radius: 8px;
  font-size: 11px;
  line-height: 1.25;
}
</style>
