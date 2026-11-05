<template>
  <!-- 指引气泡：绝对定位在目标旁边，不挡点击；放在 relative 的包裹元素里使用 -->
  <span
    role="tooltip"
    class="guide-bubble"
    :class="[`gb-${placement}`, `gb-align-${align}`, xl ? `gb-xl-${xl}` : '', wrap ? 'gb-wrap' : '']"
    data-testid="guide-bubble"
    :data-placement="placement"
  >
    {{ text }}
  </span>
</template>

<script setup lang="ts">
type Placement = 'top' | 'right' | 'bottom'

withDefaults(
  defineProps<{
    text: string
    /** 默认位置 */
    placement?: Placement
    /** 屏幕宽度 ≥1280px 时改用的位置（可选） */
    xl?: Placement
    /** 上/下方时与目标的对齐方式 */
    align?: 'start' | 'center' | 'end'
    /** 允许换行：最宽不超过 --gb-max-width（由使用方按可用空间给出），放不下就折成多行 */
    wrap?: boolean
  }>(),
  { placement: 'top', align: 'start', xl: undefined, wrap: false }
)
</script>

<style scoped>
/* 品牌青蓝渐变小气泡：小字号、柔和阴影，轻轻上下浮动，不刺眼 */
.guide-bubble {
  --gb-gap: 10px;
  --gb-shift: 0px;
  position: absolute;
  z-index: 30;
  pointer-events: none;
  white-space: nowrap;
  padding: 6px 12px;
  border-radius: 10px;
  font-size: 13px;
  font-weight: 600;
  line-height: 18px;
  color: #fff;
  background: linear-gradient(135deg, #0d9488 0%, #0891b2 100%);
  box-shadow: 0 8px 20px -8px rgba(8, 145, 178, 0.75);
  animation:
    gb-in 0.35s ease-out both,
    gb-float 2.4s ease-in-out 0.35s infinite;
}

/* 可换行：宽度随文字，最宽不超过使用方给的可用空间 */
.gb-wrap {
  white-space: normal;
  width: max-content;
  max-width: var(--gb-max-width, none);
}

/* 小三角 */
.guide-bubble::after {
  content: '';
  position: absolute;
  width: 8px;
  height: 8px;
  background: inherit;
  border-radius: 1px;
}

/* 上方 */
.gb-top {
  bottom: calc(100% + var(--gb-gap));
}
.gb-top::after {
  top: 100%;
  margin-top: -4px;
  transform: rotate(45deg);
  background: #0a8fa5;
}
/* 下方 */
.gb-bottom {
  top: calc(100% + var(--gb-gap));
}
.gb-bottom::after {
  bottom: 100%;
  margin-bottom: -4px;
  transform: rotate(45deg);
  background: #0c90a0;
}
.gb-top.gb-align-start,
.gb-bottom.gb-align-start {
  left: 0;
}
.gb-top.gb-align-start::after,
.gb-bottom.gb-align-start::after {
  left: 16px;
}
.gb-top.gb-align-center,
.gb-bottom.gb-align-center {
  left: 50%;
  translate: -50% 0;
}
.gb-top.gb-align-center::after,
.gb-bottom.gb-align-center::after {
  left: calc(50% - 4px);
}
.gb-top.gb-align-end,
.gb-bottom.gb-align-end {
  right: 0;
}
.gb-top.gb-align-end::after,
.gb-bottom.gb-align-end::after {
  right: 16px;
}
/* 右侧 */
.gb-right {
  left: calc(100% + var(--gb-gap));
  top: 50%;
  translate: 0 -50%;
}
.gb-right::after {
  right: 100%;
  top: calc(50% - 4px);
  margin-right: -4px;
  transform: rotate(45deg);
  background: #0d9488;
}

/* 两个类名提高优先级，盖过上面「上/下方 + 对齐」的定位 */
@media (min-width: 1280px) {
  .guide-bubble.gb-xl-right {
    left: calc(100% + var(--gb-gap));
    right: auto;
    top: 50%;
    bottom: auto;
    translate: 0 -50%;
  }
  .guide-bubble.gb-xl-right::after {
    left: auto;
    right: 100%;
    top: calc(50% - 4px);
    bottom: auto;
    margin: 0 -4px 0 0;
    background: #0d9488;
  }
}

/* 浮动方向朝着目标：上方的往下点、右侧的往左点 */
.gb-top {
  --gb-dx: 0px;
  --gb-dy: 3px;
}
.gb-bottom {
  --gb-dx: 0px;
  --gb-dy: -3px;
}
.gb-right {
  --gb-dx: -3px;
  --gb-dy: 0px;
}
@media (min-width: 1280px) {
  .guide-bubble.gb-xl-right {
    --gb-dx: -3px;
    --gb-dy: 0px;
  }
}

@keyframes gb-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

@keyframes gb-float {
  0%,
  100% {
    transform: translate(0, 0);
  }
  50% {
    transform: translate(var(--gb-dx), var(--gb-dy));
  }
}

@media (prefers-reduced-motion: reduce) {
  .guide-bubble {
    animation: none;
  }
}
</style>
