<template>
  <div class="qq-card" data-testid="qq-card">
    <div class="qq-icon">
      <img :src="qqGroup.penguinImage" alt="" />
    </div>
    <div class="qq-text">
      <div class="qq-label">{{ t('community.qqGroup') }}</div>
      <div class="qq-number">{{ qqGroup.number }}</div>
    </div>
    <div class="qq-actions">
      <button type="button" class="qq-pill" data-testid="qq-copy" @click="copyNumber">
        <Icon name="copy" size="sm" />
        {{ t('community.copyNumber') }}
      </button>
      <button type="button" class="qq-pill qq-pill-solid" data-testid="qq-scan" @click="showDialog = true">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <rect width="5" height="5" x="3" y="3" rx="1" />
          <rect width="5" height="5" x="16" y="3" rx="1" />
          <rect width="5" height="5" x="3" y="16" rx="1" />
          <path d="M21 16h-3a2 2 0 0 0-2 2v3M21 21v.01M12 7v3a2 2 0 0 1-2 2H7M3 12h.01M12 3h.01M12 16v.01M16 12h1M21 12v.01M12 21v-1" />
        </svg>
        {{ t('community.scanToJoin') }}
      </button>
    </div>
    <QqGroupDialog :show="showDialog" @close="showDialog = false" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { qqGroup } from '../config'
import QqGroupDialog from './QqGroupDialog.vue'

const { t } = useI18n()
const showDialog = ref(false)

// 点击时才取复制工具（它依赖全局状态），组件本身挂载不依赖全局状态
function copyNumber() {
  void useClipboard().copyToClipboard(qqGroup.number, t('community.copied'))
}
</script>

<style scoped>
.qq-card {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px 14px;
  width: min(520px, 100%);
  margin-top: 34px;
  padding: 14px 14px 14px 16px;
  border-radius: 18px;
  text-align: left;
  background: rgba(255, 255, 255, 0.66);
  border: 1px solid rgba(15, 23, 42, 0.09);
  -webkit-backdrop-filter: blur(16px);
  backdrop-filter: blur(16px);
  box-shadow: 0 14px 36px -16px rgba(15, 23, 42, 0.24);
}

.qq-icon {
  display: flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: 48px;
  height: 48px;
  border-radius: 14px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  box-shadow: 0 6px 14px -6px rgba(15, 23, 42, 0.25);
}

.qq-icon img {
  width: 30px;
  height: auto;
}

.qq-text {
  flex: 1 1 auto;
}

.qq-label {
  font-size: 13px;
  white-space: nowrap;
  color: #566275;
}

.qq-number {
  margin-top: 2px;
  font-size: 22px;
  font-weight: 800;
  line-height: 1.2;
  letter-spacing: 0.02em;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  color: #0b1220;
}

.qq-actions {
  display: flex;
  gap: 8px;
  margin-left: auto;
}

.qq-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 38px;
  padding: 0 14px;
  border-radius: 11px;
  font-size: 14px;
  font-weight: 650;
  white-space: nowrap;
  cursor: pointer;
  color: #0b1220;
  background: rgba(255, 255, 255, 0.92);
  border: 1px solid rgba(15, 23, 42, 0.09);
  transition: border-color 0.2s, opacity 0.2s;
}

.qq-pill:hover {
  border-color: #0d9488;
}

.qq-pill-solid {
  color: #f4f7fa;
  background: #0b1220;
  border-color: transparent;
}

.qq-pill-solid:hover {
  opacity: 0.88;
  border-color: transparent;
}

.dark .qq-card {
  background: rgba(15, 23, 42, 0.55);
  border-color: rgba(148, 163, 184, 0.15);
  box-shadow: 0 16px 40px -16px rgba(0, 0, 0, 0.75);
}

.dark .qq-label {
  color: #9aa6b8;
}

.dark .qq-number {
  color: #eef2f7;
}

.dark .qq-pill {
  color: #eef2f7;
  background: rgba(12, 19, 36, 0.92);
  border-color: rgba(148, 163, 184, 0.15);
}

.dark .qq-pill:hover {
  border-color: #2dd4bf;
}

.dark .qq-pill-solid {
  color: #030712;
  background: #eef2f7;
  border-color: transparent;
}

@media (max-width: 640px) {
  .qq-card {
    padding: 14px;
  }

  .qq-actions {
    width: 100%;
  }

  .qq-actions .qq-pill {
    flex: 1;
    justify-content: center;
  }
}
</style>
