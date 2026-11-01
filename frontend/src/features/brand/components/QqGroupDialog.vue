<template>
  <BaseDialog :show="show" :title="t('community.dialogTitle')" width="narrow" @close="emit('close')">
    <div class="text-center" data-testid="qq-dialog">
      <p class="text-sm text-gray-500 dark:text-dark-400">
        {{ qqGroup.name }} · {{ t('community.groupNumber', { number: qqGroup.number }) }}
      </p>
      <div class="qr-box" :style="cropStyle">
        <img :src="qqGroup.cardImage" :alt="t('community.qrAlt')" data-testid="qq-qr-image" />
      </div>
      <p class="text-sm text-gray-400 dark:text-dark-500">{{ t('community.saveHint') }}</p>
      <a
        :href="qqGroup.link"
        target="_blank"
        rel="noopener noreferrer"
        class="brand-gradient-btn mt-4 flex h-11 w-full items-center justify-center rounded-xl text-[15px] font-semibold text-white"
        data-testid="qq-join-link"
      >
        {{ t('community.openLink') }}
      </a>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { qqCardCrop, qqGroup } from '../config'

defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

const cropStyle = {
  '--crop-w': String(qqCardCrop.width / qqCardCrop.size),
  '--crop-x': String(qqCardCrop.x / qqCardCrop.size),
  '--crop-y': String(qqCardCrop.y / qqCardCrop.size)
}
</script>

<style scoped>
.qr-box {
  --s: 280px;
  position: relative;
  width: var(--s);
  height: var(--s);
  max-width: 100%;
  margin: 18px auto 10px;
  overflow: hidden;
  border-radius: 16px;
  background: #fff;
  box-shadow: inset 0 0 0 1px rgba(15, 23, 42, 0.06);
}

.qr-box img {
  position: absolute;
  max-width: none;
  width: calc(var(--s) * var(--crop-w));
  left: calc(var(--s) * var(--crop-x) * -1);
  top: calc(var(--s) * var(--crop-y) * -1);
}

.brand-gradient-btn {
  background: linear-gradient(135deg, #0d9488 0%, #0891b2 100%);
  transition: filter 0.2s;
}

.brand-gradient-btn:hover {
  filter: brightness(1.05);
}

@media (max-width: 640px) {
  .qr-box {
    --s: 250px;
  }
}
</style>
