<template>
  <span v-if="parts" class="brand-name"
    >{{ parts.before }}<span class="brand-good" data-testid="brand-highlight">{{ parts.word }}</span
    >{{ parts.after }}</span
  >
  <span v-else class="brand-name">{{ name }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { brandHighlight } from '../config'

const props = defineProps<{ name: string }>()

/** 站点名里第一次出现的高亮词（区分大小写）拆出来单独上色；没有就原样显示 */
const parts = computed(() => {
  const index = props.name.indexOf(brandHighlight)
  if (index < 0) return null
  return {
    before: props.name.slice(0, index),
    word: brandHighlight,
    after: props.name.slice(index + brandHighlight.length)
  }
})
</script>

<style scoped>
.brand-good {
  background-image: linear-gradient(120deg, #0d9488 10%, #0891b2 55%, #2563eb 100%);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.dark .brand-good {
  background-image: linear-gradient(120deg, #79f4bd 0%, #39d9e7 50%, #3875f6 100%);
}
</style>
