<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <!-- Background Decoration -->
    <div class="pointer-events-none fixed inset-0 bg-mesh-gradient"></div>

    <!-- Sidebar -->
    <AppSidebar ref="sidebarRef" />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main ref="contentRef" class="p-4 md:p-6 lg:p-8">
        <slot />
      </main>
    </div>
    <PluginShell v-if="pluginsEnabled" :anchors="anchors" />
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onBeforeUnmount, onMounted, ref, watch, type ComponentPublicInstance } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import PluginShell from '@/components/plugins/PluginShell.vue'
import { usePluginEntriesStore } from '@/stores/pluginEntries'

const appStore = useAppStore()
const authStore = useAuthStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.user?.role === 'admin')
const route = useRoute()
const pluginEntries = usePluginEntriesStore()
const sidebarRef = ref<ComponentPublicInstance | null>(null)
const contentRef = ref<HTMLElement | null>(null)
const anchors = computed(() => ({ navigation: (sidebarRef.value?.$el as HTMLElement) || null, content: contentRef.value }))
const pluginsEnabled = computed(() => authStore.isAuthenticated && authStore.user?.status === 'active' && route.meta.requiresAuth === true)
let releasePluginEntries: (() => void) | undefined
watch(pluginsEnabled, enabled => {
  releasePluginEntries?.()
  releasePluginEntries = enabled ? pluginEntries.acquire() : undefined
}, { immediate: true })
onBeforeUnmount(() => { releasePluginEntries?.() })

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

defineExpose({ replayTour })
</script>
