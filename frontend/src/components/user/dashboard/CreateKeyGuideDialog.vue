<template>
  <BaseDialog :show="show" :title="t('dashboard.quickStart.step1GuideTitle')" width="wide" @close="emit('close')">
    <div class="space-y-5" data-testid="create-key-guide">
      <figure v-for="(item, i) in shots" :key="item.src" class="space-y-2">
        <figcaption class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-white">
          <span class="flex h-6 w-6 flex-none items-center justify-center rounded-full bg-primary-50 text-xs font-semibold text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">{{ i + 1 }}</span>
          {{ item.caption }}
        </figcaption>
        <!-- 点图片在新标签页看原图 -->
        <a :href="item.src" target="_blank" rel="noopener noreferrer" class="block overflow-hidden rounded-xl border border-gray-200 dark:border-dark-600">
          <img :src="item.src" :alt="item.caption" class="block h-auto w-full" loading="lazy" data-testid="create-key-guide-image" />
        </a>
      </figure>
      <p class="text-xs text-gray-400 dark:text-dark-500">{{ t('dashboard.quickStart.step1GuideZoom') }}</p>
    </div>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button>
        <router-link to="/keys" class="btn btn-primary" data-testid="create-key-guide-go" @click="emit('close')">{{ t('dashboard.quickStart.step1Action') }}</router-link>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
// 与新手引导“快速开始”文档共用同一组截图
import step1Image from '@/features/guide/content/quick-start/images/create-api-key-1.jpg?url'
import step2Image from '@/features/guide/content/quick-start/images/create-api-key-2.jpg?url'

defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

const shots = computed(() => [
  { src: step1Image, caption: t('dashboard.quickStart.step1GuideShot1') },
  { src: step2Image, caption: t('dashboard.quickStart.step1GuideShot2') }
])
</script>
