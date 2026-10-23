<template>
  <slot />
  <PluginShell v-if="pluginsEnabled" :anchors="anchors" />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, provide, shallowRef, watch, type ComputedRef } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { usePluginEntriesStore } from '@/stores/pluginEntries'
import PluginShell from './PluginShell.vue'
import { pluginLayoutKey, type PluginLayoutAnchors } from './layoutContext'

const auth = useAuthStore()
const route = useRoute()
const entries = usePluginEntriesStore()
const layout = shallowRef<ComputedRef<PluginLayoutAnchors> | null>(null)
provide(pluginLayoutKey, layout)
const anchors = computed(() => layout.value?.value ?? { navigation: null, content: null })
const pluginsEnabled = computed(() => auth.isAuthenticated && auth.user?.status === 'active' && route.meta.requiresAuth === true && layout.value !== null)
let releaseEntries: (() => void) | undefined

// Observe the completed page replacement, not the old layout's unmount in the
// middle of the router patch. The shell and its one subscription stay alive.
watch(pluginsEnabled, enabled => {
  releaseEntries?.()
  releaseEntries = enabled ? entries.acquire() : undefined
}, { immediate: true, flush: 'post' })
onBeforeUnmount(() => { releaseEntries?.() })
</script>
