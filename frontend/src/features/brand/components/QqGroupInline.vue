<template>
  <div class="qq-inline" data-testid="qq-inline">
    <span class="qq-tile">
      <img :src="qqGroup.penguinImage" alt="" />
    </span>
    <div class="qq-body">
      <div class="qq-line1">
        <span class="qq-join">{{ t('community.joinGroup') }}</span>
        <button
          type="button"
          class="qq-num"
          :title="t('community.copyNumber')"
          data-testid="qq-copy"
          @click="copyNumber"
        >
          {{ qqGroup.number }}
        </button>
      </div>
      <div class="qq-line2">{{ t('community.newcomerReward') }}</div>
    </div>
    <button type="button" class="qq-scan" data-testid="qq-scan" @click="showDialog = true">
      {{ t('community.scanToJoin') }}
    </button>
    <QqGroupDialog :show="showDialog" @close="showDialog = false" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
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
.qq-inline {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
  padding: 10px 10px 10px 12px;
  border-radius: 14px;
  text-align: left;
  background: rgba(13, 148, 136, 0.09);
}

.qq-tile {
  display: flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border-radius: 11px;
  background: #fff;
  box-shadow: 0 4px 10px -4px rgba(15, 23, 42, 0.22);
}

.qq-tile img {
  width: 25px;
  height: auto;
}

.qq-body {
  flex: 1;
  min-width: 0;
}

.qq-line1 {
  font-size: 14px;
  color: #334155;
  white-space: nowrap;
}

.qq-join {
  margin-right: 4px;
}

.qq-num {
  padding: 0;
  border: 0;
  border-bottom: 1px dashed #8a94a6;
  background: none;
  font: inherit;
  font-weight: 800;
  font-variant-numeric: tabular-nums;
  color: #0b1220;
  cursor: pointer;
}

.qq-num:hover {
  color: #0f766e;
  border-bottom-color: currentColor;
}

.qq-line2 {
  margin-top: 2px;
  font-size: 13px;
  font-weight: 650;
  color: #0f766e;
}

.qq-scan {
  flex: none;
  height: 34px;
  padding: 0 12px;
  border: 0;
  border-radius: 10px;
  font-size: 13px;
  font-weight: 650;
  white-space: nowrap;
  cursor: pointer;
  color: #fff;
  background: #111827;
}

.qq-scan:hover {
  background: #1f2937;
}

.dark .qq-inline {
  background: rgba(45, 212, 191, 0.12);
}

.dark .qq-line1 {
  color: #cbd5e1;
}

.dark .qq-num {
  color: #eef2f7;
  border-bottom-color: #5f6b7f;
}

.dark .qq-num:hover,
.dark .qq-line2 {
  color: #5eead4;
}

.dark .qq-scan {
  color: #111827;
  background: #fff;
}

.dark .qq-scan:hover {
  background: #e5e7eb;
}

@media (max-width: 420px) {
  .qq-inline {
    gap: 10px;
  }

  .qq-join {
    display: none;
  }
}
</style>
