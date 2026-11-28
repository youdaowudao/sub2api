<template>
  <div class="group/dropdown relative">
    <button
      ref="triggerRef"
      :id="id"
      type="button"
      :disabled="disabled"
      :aria-label="ariaLabel"
      :aria-describedby="$attrs['aria-describedby'] as string | undefined"
      :aria-invalid="$attrs['aria-invalid'] as boolean | undefined"
      :aria-expanded="isOpen"
      @click="openGroupSelector"
      class="-mx-2 -my-1 flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1 transition-all duration-200 hover:bg-gray-100 max-md:max-w-full max-md:flex-wrap max-md:justify-end dark:hover:bg-dark-700"
      :title="t('keys.clickToChangeGroup')"
    >
      <GroupBadge
        v-if="selectedOption?.platform && !selectedSpecialOption"
        class="max-md:max-w-full max-md:flex-wrap"
        :name="selectedOption.label"
        :platform="selectedOption.platform"
        :subscription-type="selectedOption.subscriptionType"
        :rate-multiplier="selectedOption.rate"
        :user-rate-multiplier="selectedOption.userRate"
        :peak-rate-enabled="selectedOption.peakRateEnabled"
        :peak-start="selectedOption.peakStart"
        :peak-end="selectedOption.peakEnd"
        :peak-rate-multiplier="selectedOption.peakRateMultiplier"
      />
      <span v-else class="text-sm text-gray-400 dark:text-dark-500">{{
        selectedSpecialOption?.label ?? selectedOption?.label ?? t('keys.noGroup')
      }}</span>
      <svg
        class="h-3.5 w-3.5 text-gray-400 opacity-60 transition-opacity group-hover/dropdown:opacity-100"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M8.25 15L12 18.75 15.75 15m-7.5-6L12 5.25 15.75 9"
        />
      </svg>
    </button>
    <!-- Group Selector Dropdown (Teleported to body to avoid overflow clipping) -->
    <Teleport to="body">
      <div
        v-if="isOpen && dropdownPosition"
        ref="dropdownRef"
        class="animate-in fade-in slide-in-from-top-2 fixed z-[100000020] flex w-max min-w-[min(380px,calc(100vw-16px))] max-w-[calc(100vw-16px)] flex-col overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-black/5 duration-200 dark:bg-dark-800 dark:ring-white/10"
        style="pointer-events: auto !important;"
        :style="{
          top: dropdownPosition.top !== undefined ? dropdownPosition.top + 'px' : undefined,
          bottom: dropdownPosition.bottom !== undefined ? dropdownPosition.bottom + 'px' : undefined,
          left: dropdownPosition.left + 'px',
          maxHeight: `calc(100dvh - ${(dropdownPosition.top ?? dropdownPosition.bottom ?? 0) + 8}px)`
        }"
      >
        <!-- Search box -->
        <div class="shrink-0 border-b border-gray-100 p-2 [contain:inline-size] dark:border-dark-700">
          <div class="relative">
            <svg class="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              v-model="groupSearchQuery"
              type="text"
              class="w-full rounded-lg border border-gray-200 bg-gray-50 py-1.5 pl-8 pr-3 text-sm text-gray-900 placeholder-gray-400 outline-none focus:border-primary-300 focus:ring-1 focus:ring-primary-300 dark:border-dark-600 dark:bg-dark-700 dark:text-white dark:placeholder-gray-500 dark:focus:border-primary-600 dark:focus:ring-primary-600"
              :placeholder="t('keys.searchGroup')"
              @click.stop
            />
          </div>
        </div>
        <div
          v-if="groupPickerCategories.length"
          class="flex shrink-0 flex-wrap items-center gap-1 border-b border-gray-100 px-2 py-2 dark:border-dark-700"
        >
          <button
            v-for="section in groupPickerCategories"
            :key="section.category"
            type="button"
            class="inline-flex flex-none items-center gap-1 whitespace-nowrap rounded-md px-1.5 py-1.5 text-xs font-semibold ring-inset"
            :class="[
              platformBadgeLightClass(section.category === 'image' ? 'antigravity' : GROUP_DISPLAY_CATEGORY_ICONS[section.category][0]),
              activeGroupCategory === section.category && 'ring-1 ring-current',
              searchingGroupPicker && 'opacity-50',
            ]"
            :aria-pressed="activeGroupCategory === section.category"
            :disabled="searchingGroupPicker"
            @pointerenter="scheduleGroupCategory(section.category, $event)"
            @pointerleave="cancelGroupCategoryHover"
            @click="selectGroupCategory(section.category)"
          >
            <Icon v-if="section.category === 'image'" name="sparkles" size="xs" aria-hidden="true" />
            <PlatformIcon
              v-for="platform in GROUP_DISPLAY_CATEGORY_ICONS[section.category]"
              :key="platform"
              :platform="platform"
              size="xs"
              aria-hidden="true"
            />
            {{ groupCategoryLabel(section.category) }} {{ section.groups.length }}
          </button>
        </div>
        <!-- Group list -->
        <div
          ref="groupPickerListRef"
          class="min-h-0 max-h-80 overflow-y-auto p-1.5 [contain:inline-size]"
          :class="groupPickerCategories.length > 1 && 'h-80'"
        >
          <button
            v-for="option in visibleGroupPickerOptions"
            :key="`${typeof option.value}:${option.value}`"
            type="button"
            :disabled="option.disabled"
            @click="changeGroup(option)"
            :class="[
              'flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-sm transition-colors',
              'border-b border-gray-100 last:border-0 dark:border-dark-700',
              modelValue === option.value
                ? 'bg-primary-50 dark:bg-primary-900/20'
                : 'hover:bg-gray-100 dark:hover:bg-dark-700'
            ]"
            :title="option.description || option.label"
          >
            <span v-if="isSpecialOption(option) || !option.platform" class="text-sm text-gray-500 dark:text-gray-400">{{ option.label }}</span>
            <GroupOptionItem
              v-else
              :name="option.label"
              :platform="option.platform"
              :subscription-type="option.subscriptionType"
              :rate-multiplier="option.rate"
              :user-rate-multiplier="option.userRate"
              :peak-rate-enabled="option.peakRateEnabled"
              :peak-start="option.peakStart"
              :peak-end="option.peakEnd"
              :peak-rate-multiplier="option.peakRateMultiplier"
              :description="option.description"
              wrap-name
              :selected="
                modelValue === option.value
              "
            />
          </button>
          <!-- Empty state when search has no results -->
          <div v-if="visibleGroupOptions.length === 0 && (searchingGroupPicker || activeGroupCategory !== 'image')" class="py-4 text-center text-sm text-gray-400 dark:text-gray-500">
            {{ t('keys.noGroupFound') }}
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { GROUP_DISPLAY_CATEGORY_ICONS, groupGroupsByCategory, groupIconPlatform } from '@/utils/groupDisplay'
import { platformBadgeLightClass } from '@/utils/platformColors'
import type { Group, GroupPlatform, SubscriptionType } from '@/types'
import type { SelectOption } from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'

type PickerValue = number | string | null
type KeyGroupProvider = keyof typeof GROUP_DISPLAY_CATEGORY_ICONS
type PickerGroup = Pick<Group, 'id'> & { platform: string } & Partial<Pick<Group,
  'display_category' | 'display_icon' | 'sort_order' | 'description' | 'subscription_type' |
  'rate_multiplier' | 'peak_rate_enabled' | 'peak_start' | 'peak_end' | 'peak_rate_multiplier'>>
interface PickerOption extends SelectOption {
  value: PickerValue
  platform?: GroupPlatform
  subscriptionType?: SubscriptionType
  rate?: number
  userRate?: number | null
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  description?: string | null
}

const props = defineProps<{
  modelValue: PickerValue
  groups: PickerGroup[]
  specialOptions: { value: null | string; label: string }[]
  options: SelectOption[]
  disabled?: boolean
  id?: string
  ariaLabel?: string
}>()
const emit = defineEmits<{
  'update:modelValue': [value: PickerValue]
  change: [value: PickerValue, option: SelectOption]
}>()
const { t } = useI18n()
const groupCategoryLabel = (category: KeyGroupProvider) => t(`keys.providers.${category}`)
const isSpecialOption = (option: { value: PickerValue }) => props.specialOptions.some(special => special.value === option.value)
const groupOptions = computed<PickerOption[]>(() => {
  const groupsByID = new Map(props.groups.map(group => [String(group.id), group]))
  return props.options.filter((option): option is PickerOption =>
    typeof option.value !== 'boolean' && option.kind !== 'group' && !isSpecialOption(option as PickerOption)
  ).map(option => {
    const group = groupsByID.get(String(option.value))
    if (!group) return option
    return {
      ...option,
      platform: option.platform ?? groupIconPlatform(group) as GroupPlatform,
      subscriptionType: option.subscriptionType ?? group.subscription_type,
      rate: option.rate ?? group.rate_multiplier,
      description: option.description ?? group.description,
      peakRateEnabled: option.peakRateEnabled ?? group.peak_rate_enabled,
      peakStart: option.peakStart ?? group.peak_start,
      peakEnd: option.peakEnd ?? group.peak_end,
      peakRateMultiplier: option.peakRateMultiplier ?? group.peak_rate_multiplier,
    }
  })
})
const selectedOption = computed(() => groupOptions.value.find(option => option.value === props.modelValue))
const selectedSpecialOption = computed(() => props.specialOptions.find(option => option.value === props.modelValue))
const isOpen = ref(false)
const triggerRef = ref<HTMLElement | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const dropdownPosition = ref<{ top?: number; bottom?: number; left: number; anchorLeft: number } | null>(null)

// Group dropdown search
const groupSearchQuery = ref('')
const searchingGroupPicker = computed(() => groupSearchQuery.value.trim() !== '')
const groupPickerListRef = ref<HTMLElement | null>(null)
const activeGroupCategory = ref<KeyGroupProvider | null>(null)
const groupPickerCategories = computed(() => groupGroupsByCategory(props.groups, true))
let groupCategoryHoverTimer: ReturnType<typeof setTimeout> | undefined

function cancelGroupCategoryHover() {
  clearTimeout(groupCategoryHoverTimer)
  groupCategoryHoverTimer = undefined
}

function selectGroupCategory(category: KeyGroupProvider) {
  cancelGroupCategoryHover()
  if (!searchingGroupPicker.value) activeGroupCategory.value = category
}

function scheduleGroupCategory(category: KeyGroupProvider, event: PointerEvent) {
  cancelGroupCategoryHover()
  if (event.pointerType === 'touch' || searchingGroupPicker.value || category === activeGroupCategory.value) return
  groupCategoryHoverTimer = setTimeout(() => selectGroupCategory(category), 150)
}

watch([isOpen, groupPickerCategories], ([open, categories], [previousOpen]) => {
  cancelGroupCategoryHover()
  if (!open) {
    activeGroupCategory.value = null
  } else if (open !== previousOpen || !categories.some(section => section.category === activeGroupCategory.value)) {
    const current = categories.find(section => section.groups.some(group => String(group.id) === String(props.modelValue)))
    activeGroupCategory.value = current?.category ?? categories.find(section => section.groups.length > 0)?.category ?? null
  }
})

watch(searchingGroupPicker, cancelGroupCategoryHover)
watch([activeGroupCategory, searchingGroupPicker], () => {
  if (groupPickerListRef.value) groupPickerListRef.value.scrollTop = 0
})

watch([dropdownRef, groupPickerCategories], () => {
  const panel = dropdownRef.value
  const position = dropdownPosition.value
  if (!panel || !position) return
  position.left = Math.max(8, Math.min(position.anchorLeft, window.innerWidth - panel.offsetWidth - 8))
}, { flush: 'post' })

const filteredGroupOptions = computed(() => {
  const query = groupSearchQuery.value.trim().toLowerCase()
  if (!query) return groupOptions.value
  return groupOptions.value.filter((opt) => {
    return opt.label.toLowerCase().includes(query) ||
      (opt.description && opt.description.toLowerCase().includes(query))
  })
})

const visibleGroupOptions = computed(() => {
  const optionsByID = new Map(filteredGroupOptions.value.map(option => [String(option.value), option]))
  const sections = searchingGroupPicker.value
    ? groupPickerCategories.value
    : groupPickerCategories.value.filter(section => section.category === activeGroupCategory.value)
  const groupedOptions = sections.flatMap(section => section.groups.flatMap(group => optionsByID.get(String(group.id)) ?? []))
  const knownIDs = new Set(props.groups.map(group => String(group.id)))
  // Preserve the original Select fallback for a previously selected, unavailable group.
  return [...groupedOptions, ...filteredGroupOptions.value.filter(option => !knownIDs.has(String(option.value)))]
})

const visibleGroupPickerOptions = computed<PickerOption[]>(() => [...props.specialOptions, ...visibleGroupOptions.value])

const openGroupSelector = () => {
  if (props.disabled) return
  if (isOpen.value) {
    isOpen.value = false
    dropdownPosition.value = null
  } else {
    const buttonEl = triggerRef.value
    if (buttonEl) {
      const rect = buttonEl.getBoundingClientRect()
      const dropdownEstHeight = 400 // estimated max dropdown height
      const dropdownEstWidth = Math.min(380, window.innerWidth - 16)
      const spaceBelow = window.innerHeight - rect.bottom
      const spaceAbove = rect.top
      // 夹取 left，避免窄屏下浮层超出视口右缘
      const left = Math.max(8, Math.min(rect.left, window.innerWidth - dropdownEstWidth - 8))

      if (spaceBelow < dropdownEstHeight && spaceAbove > spaceBelow) {
        // Not enough space below, pop upward
        dropdownPosition.value = {
          bottom: window.innerHeight - rect.top + 4,
          left,
          anchorLeft: rect.left
        }
      } else {
        // Default: pop downward
        dropdownPosition.value = {
          top: rect.bottom + 4,
          left,
          anchorLeft: rect.left
        }
      }
    }
    isOpen.value = true
    groupSearchQuery.value = ''
  }
}

const closeGroupSelector = () => {
  isOpen.value = false
  dropdownPosition.value = null
  cancelGroupCategoryHover()
}

const changeGroup = (option: PickerOption) => {
  if (option.disabled) return
  emit('update:modelValue', option.value)
  emit('change', option.value, props.options.find(original => original.value === option.value) ?? option)
  closeGroupSelector()
  triggerRef.value?.focus()
}

const onDocumentClick = (event: MouseEvent) => {
  const target = event.target as Node
  if (!triggerRef.value?.contains(target) && !dropdownRef.value?.contains(target)) {
    closeGroupSelector()
  }
}

const onKeyDown = (event: KeyboardEvent) => {
  if (isOpen.value && event.key === 'Escape') {
    event.stopImmediatePropagation()
    closeGroupSelector()
    triggerRef.value?.focus()
  }
}

const onScroll = (event: Event) => {
  if (isOpen.value && !(event.target instanceof Node && dropdownRef.value?.contains(event.target))) closeGroupSelector()
}

watch(() => props.disabled, disabled => { if (disabled) closeGroupSelector() })

onMounted(() => {
  document.addEventListener('click', onDocumentClick, true)
  document.addEventListener('keydown', onKeyDown, true)
  window.addEventListener('scroll', onScroll, true)
  window.addEventListener('resize', closeGroupSelector)
})

onUnmounted(() => {
  document.removeEventListener('click', onDocumentClick, true)
  document.removeEventListener('keydown', onKeyDown, true)
  window.removeEventListener('scroll', onScroll, true)
  window.removeEventListener('resize', closeGroupSelector)
  cancelGroupCategoryHover()
})
</script>
