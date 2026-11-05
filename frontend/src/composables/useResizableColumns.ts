import { computed, ref, type Ref } from 'vue'

/** 拖动时列宽的下限，避免拖成看不见 */
export const MIN_COLUMN_WIDTH = 48
/** 从没拖过、也量不到宽度的列（比如后来才勾选显示的列）的默认宽度 */
export const DEFAULT_COLUMN_WIDTH = 140
const MAX_COLUMN_WIDTH = 2000

type Widths = Record<string, number>

function readWidths(storageKey: string): Widths {
  try {
    const raw = localStorage.getItem(storageKey)
    if (!raw) return {}
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    const out: Widths = {}
    for (const [key, value] of Object.entries(parsed)) {
      if (typeof value === 'number' && Number.isFinite(value) && value >= MIN_COLUMN_WIDTH && value <= MAX_COLUMN_WIDTH) {
        out[key] = Math.round(value)
      }
    }
    return out
  } catch {
    return {}
  }
}

function writeWidths(storageKey: string, widths: Widths) {
  try {
    if (Object.keys(widths).length) localStorage.setItem(storageKey, JSON.stringify(widths))
    else localStorage.removeItem(storageKey)
  } catch {
    // 隐私模式等存不了时，本次页面里照样能拖，只是不记住
  }
}

/**
 * 表头拖动调整列宽，并记在本浏览器（localStorage）。
 * 一旦拖过，表格切到固定布局：每列按记住的宽度显示，内容超出就截断；
 * 双击拖动条恢复自动列宽。storageKey 为空时整个功能关闭。
 */
export function useResizableColumns(storageKey: Ref<string | undefined>, columnKeys: Ref<string[]>) {
  const widths = ref<Widths>(storageKey.value ? readWidths(storageKey.value) : {})
  const enabled = computed(() => !!storageKey.value)
  /** 有记住的宽度就用固定布局 */
  const isFixed = computed(() => enabled.value && Object.keys(widths.value).length > 0)
  const resizingKey = ref<string | null>(null)

  const widthOf = (key: string) => widths.value[key] ?? DEFAULT_COLUMN_WIDTH

  /** 固定布局下表格总宽 = 各列宽之和（extra 是勾选列等不可拖的列） */
  const tableWidth = (extra = 0) => columnKeys.value.reduce((sum, key) => sum + widthOf(key), extra)

  function save() {
    if (storageKey.value) writeWidths(storageKey.value, widths.value)
  }

  /**
   * 开始拖动。headerCells 用来在第一次拖动时把当前自动布局下的列宽全部量下来，
   * 这样切到固定布局的瞬间各列宽度不跳。
   */
  function startResize(event: PointerEvent, key: string, headerCells: () => Record<string, HTMLElement | undefined>) {
    if (!enabled.value || event.button !== 0) return
    event.preventDefault()
    event.stopPropagation()
    const cells = headerCells()
    if (!isFixed.value) {
      const snapshot: Widths = {}
      for (const k of columnKeys.value) {
        const w = cells[k]?.getBoundingClientRect().width
        if (w) snapshot[k] = Math.max(MIN_COLUMN_WIDTH, Math.ceil(w))
      }
      widths.value = snapshot
    }
    const startX = event.clientX
    const startWidth = widths.value[key] ?? cells[key]?.getBoundingClientRect().width ?? DEFAULT_COLUMN_WIDTH
    resizingKey.value = key

    const onMove = (e: PointerEvent) => {
      const next = Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, Math.round(startWidth + e.clientX - startX)))
      widths.value = { ...widths.value, [key]: next }
    }
    const onUp = () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      window.removeEventListener('pointercancel', onUp)
      document.body.style.removeProperty('cursor')
      document.body.style.removeProperty('user-select')
      resizingKey.value = null
      save()
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onUp)
    // 拖动过程中鼠标离开拖动条也保持左右箭头，并且不选中文字
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
  }

  /** 恢复自动列宽，并清掉本浏览器记住的宽度 */
  function resetWidths() {
    widths.value = {}
    save()
  }

  return { enabled, isFixed, widths, widthOf, tableWidth, resizingKey, startResize, resetWidths }
}
