<template>
  <button type="button" class="ml-1 inline-flex items-center rounded p-0.5 align-middle text-gray-400 hover:bg-primary-50 hover:text-primary-600 dark:hover:bg-primary-900/30" :title="label" :aria-label="label" @click.stop="$emit('click')">
    <Icon name="search" size="xs" />
  </button>
</template>
<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
defineProps<{ label: string }>()
defineEmits<{ click: [] }>()
</script>
