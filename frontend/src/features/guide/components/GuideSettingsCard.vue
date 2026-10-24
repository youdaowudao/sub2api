<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useAppStore } from '@/stores'
import { buildConfiguration, moduleCatalog, normalizeConfig, previewConfigKey, type GuideConfig } from '../config'

const appStore = useAppStore()
function readBuildConfiguration() {
  try { return { value: buildConfiguration(), error: '' } }
  catch (cause) { return { value: null, error: cause instanceof Error && cause.message ? cause.message : '配置格式不正确。' } }
}
const { value: current, error: configurationError } = readBuildConfiguration()
const currentModules = current ? [...current.modules, ...moduleCatalog.filter(module => !current.modules.some(item => item.id === module.id)).map(module => ({ id: module.id, enabled: false }))] : []
const draft = reactive(JSON.parse(JSON.stringify(current ? { ...current, modules: currentModules } : { schemaVersion: 1, modules: [] })) as GuideConfig)
const state = ref<'loading' | 'ready' | 'cached' | 'failed'>('loading')
const message = ref('')
const error = ref(false)
const menus = computed(() => (appStore.cachedPublicSettings?.custom_menu_items || []).filter(item => item.id && item.visibility !== 'admin'))
const bound = current?.modules.find(item => item.id === 'quick-start')?.menuId
const boundMenu = computed(() => menus.value.find(item => item.id === bound))
const canJudgeBinding = computed(() => state.value === 'ready' || state.value === 'cached')
const draftQuickStart = computed(() => draft.modules.find(item => item.id === 'quick-start'))
const selectedMenu = computed({ get: () => draftQuickStart.value?.menuId || '', set: value => {
  if (!draftQuickStart.value) return
  if (value) draftQuickStart.value.menuId = value
  else delete draftQuickStart.value.menuId
} })
const keepBoundOption = computed(() => !!selectedMenu.value && (!canJudgeBinding.value || !menus.value.some(item => item.id === selectedMenu.value)))

async function readMenus(force = false) {
  state.value = 'loading'
  try {
    let result = await appStore.fetchPublicSettings()
    if (force) result = await appStore.fetchPublicSettings(true)
    state.value = result ? 'ready' : appStore.cachedPublicSettings ? 'cached' : 'failed'
  } catch { state.value = appStore.cachedPublicSettings ? 'cached' : 'failed' }
}
// Saving the custom-menu card refreshes this shared cache. Unsaved form edits never enter it.
watch(() => appStore.cachedPublicSettings, value => { if (value && state.value !== 'loading') state.value = 'ready' })
onMounted(() => { void readMenus() })
function notify(text: string, failed = false) { message.value = text; error.value = failed }
function validated() {
  if (!current) { notify(configurationError, true); return null }
  try { return normalizeConfig(draft) }
  catch (cause) { notify(cause instanceof Error ? cause.message : '配置格式不正确。', true); return null }
}
function preview() {
  const value = validated(); if (!value) return
  try {
    localStorage.setItem(previewConfigKey, JSON.stringify(value))
    notify('已保存试用配置，只对当前浏览器生效。在这个浏览器里用测试普通账号登录，在地址后加 ?onboarding-preview=1 打开用户页面，再点右下角“重播使用引导”。')
  } catch { notify('当前浏览器无法保存试用配置，可以直接导出文件。', true) }
}
function clearPreview() {
  try { localStorage.removeItem(previewConfigKey); notify('已清除试用配置。之后打开或刷新的页面使用全站配置；已经打开的试用页面需要刷新一次。') }
  catch { notify('当前浏览器无法清除试用配置，试用配置仍然保留。', true) }
}
function exportConfig() {
  const value = validated(); if (!value) return
  const url = URL.createObjectURL(new Blob([JSON.stringify(value, null, 2) + '\n'], { type: 'application/json' }))
  try {
    const link = document.createElement('a'); link.href = url; link.download = 'onboarding.json'; link.click()
    notify('配置已导出。放回源码并重新构建发布后，才会对所有用户生效。')
  } finally { URL.revokeObjectURL(url) }
}
</script>

<template>
  <section class="card" data-guide-settings>
    <div class="card-header"><h3 class="card-title">新手引导配置</h3></div>
    <div v-if="!current" class="card-body space-y-4">
      <p role="alert" class="text-sm text-red-600">当前全站引导配置无效，普通用户引导已关闭。{{ configurationError }}</p>
      <p class="text-sm text-gray-500 dark:text-dark-400">请检查配置文件并重新构建，修正后刷新页面重试。其他设置可以继续使用。</p>
      <button type="button" class="btn btn-secondary" @click="clearPreview">清除试用</button>
      <p v-if="message" :role="error ? 'alert' : 'status'" class="text-sm" :class="error ? 'text-red-600' : 'text-emerald-700'">{{ message }}</p>
    </div>
    <div v-else class="card-body space-y-4">
      <p class="text-sm text-gray-500 dark:text-dark-400">选择普通用户可以重播的章节。新用户注册后只连续播放“创建密钥 → 快速开始”；未勾选的章节不会播放。</p>
      <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700" data-guide-current>
        <h4 class="mb-2 font-medium">当前全站配置</h4>
        <ul><li v-for="item in currentModules" :key="item.id">{{ moduleCatalog.find(module => module.id === item.id)?.title }}：{{ item.enabled ? '已启用' : '已关闭' }}</li></ul>
        <p v-if="!bound" class="mt-2 text-sm">当前配置没有绑定快速开始菜单。</p>
        <p v-else-if="canJudgeBinding && !boundMenu" class="mt-2 text-sm">当前配置绑定的菜单（ID：{{ bound }}）在已保存的自定义菜单中找不到，用户打开不到快速开始正文。</p>
        <p v-else-if="boundMenu" class="mt-2 text-sm">打开“{{ boundMenu.label }}”时显示网站内置的快速开始正文，这个菜单里填写的 URL 不生效。<span>（ID：{{ bound }}）</span></p>
        <p v-else class="mt-2 text-sm">当前绑定（ID：{{ bound }}）</p>
      </div>
      <div class="flex flex-wrap items-center gap-3 text-sm" role="status">
        <span v-if="state === 'loading'">正在读取已保存的自定义菜单…</span>
        <span v-else-if="state === 'failed'">已保存的自定义菜单暂时没有读取成功，现在无法判断绑定的菜单是否存在。</span>
        <span v-else-if="state === 'cached'">菜单这次没有刷新成功，下面显示的是上次读到的结果。</span>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="state === 'loading'" @click="readMenus(true)">重新读取</button>
      </div>
      <fieldset class="flex flex-wrap gap-4">
        <legend class="mb-2 font-medium">试用草稿</legend>
        <label v-for="item in draft.modules" :key="item.id" class="flex items-center gap-2 text-sm"><input v-model="item.enabled" type="checkbox" :data-guide-module="item.id" />{{ moduleCatalog.find(module => module.id === item.id)?.title }}</label>
      </fieldset>
      <label class="block text-sm">快速开始对应的自定义菜单
        <select v-model="selectedMenu" class="input mt-2" data-guide-menu>
          <option value="">请选择已保存的用户菜单</option>
          <option v-if="keepBoundOption" :value="selectedMenu">当前绑定（ID：{{ selectedMenu }}）</option>
          <option v-for="item in canJudgeBinding ? menus : []" :key="item.id" :value="item.id">{{ item.label }}（{{ item.id }}）</option>
        </select>
      </label>
      <div class="flex flex-wrap gap-3">
        <button type="button" class="btn btn-primary" @click="preview">仅当前浏览器试用</button>
        <button type="button" class="btn btn-secondary" @click="exportConfig">导出配置文件</button>
        <button type="button" class="btn btn-secondary" @click="clearPreview">清除试用</button>
      </div>
      <p v-if="message" :role="error ? 'alert' : 'status'" class="text-sm" :class="error ? 'text-red-600' : 'text-emerald-700'">{{ message }}</p>
      <p class="break-words text-sm text-gray-500 dark:text-dark-400">试用只影响当前这个浏览器。要对全站所有用户生效，需要把导出的文件放回源码 frontend/src/features/guide/config/onboarding.json，重新构建并发布。</p>
    </div>
  </section>
</template>
