<template>
  <router-link v-if="compact" to="/affiliate" class="promo-compact" :aria-label="t('nav.affiliate')" :title="t('nav.affiliate')" @click="emit('navigate')">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><path d="m12 3 1.8 6.2L20 11l-6.2 1.8L12 19l-1.8-6.2L4 11l6.2-1.8L12 3Z M20 2v4m-2-2h4 M5 17v4m-2-2h4" stroke-linecap="round" stroke-linejoin="round" /></svg>
  </router-link>
  <section v-else class="affiliate-promo" :class="{ 'affiliate-promo-collapsed': collapsed }" data-testid="affiliate-promo-card">
    <div class="promo-heading">
      <router-link to="/affiliate" class="promo-heading-link" @click="emit('navigate')">
        <span class="promo-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="m12 3 1.8 6.2L20 11l-6.2 1.8L12 19l-1.8-6.2L4 11l6.2-1.8L12 3Z M20 2v4m-2-2h4 M5 17v4m-2-2h4" stroke-linecap="round" stroke-linejoin="round" /></svg></span>
        <span>{{ t('nav.affiliatePromo.title') }}</span>
      </router-link>
      <button type="button" class="promo-toggle" :aria-label="t(collapsed ? 'nav.affiliatePromo.expand' : 'nav.affiliatePromo.collapse')" :title="t(collapsed ? 'nav.affiliatePromo.expand' : 'nav.affiliatePromo.collapse')" :aria-expanded="!collapsed" @click="emit('toggle')">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><path v-if="collapsed" d="M14 4h6v6m0-6-6 6M10 20H4v-6m0 6 6-6" /><path v-else d="M20 10h-6V4m0 6 6-6M4 14h6v6m0-6-6 6" /></svg>
      </button>
    </div>
    <div v-if="!collapsed" class="promo-body">
      <template v-if="validRate">
        <span class="promo-rate"><span>{{ rateText }}%</span><svg viewBox="0 0 130 48" fill="none" preserveAspectRatio="none" aria-hidden="true"><ellipse cx="65" cy="24" rx="62" ry="21" stroke="currentColor" stroke-width="1.3" transform="rotate(-4 65 24)" /></svg></span>
        <p>{{ t('nav.affiliatePromo.description', { rate: rateText }) }}</p>
      </template>
      <p v-else>{{ t('nav.affiliatePromo.fallback') }}</p>
    </div>
    <router-link v-if="!collapsed" to="/affiliate" class="promo-action" @click="emit('navigate')">
      <span>{{ t('nav.affiliatePromo.action') }}</span><span class="promo-arrow" aria-hidden="true">↗</span>
    </router-link>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ collapsed: boolean; compact: boolean; rebateRate: number | null }>()
const emit = defineEmits<{ toggle: []; navigate: [] }>()
const { t } = useI18n()
const validRate = computed(() => props.rebateRate !== null && Number.isFinite(props.rebateRate) && props.rebateRate > 0 && props.rebateRate <= 100)
const rateText = computed(() => validRate.value ? String(Math.round(props.rebateRate! * 100) / 100) : '')
</script>

<style scoped>
.affiliate-promo { display:flex; flex-direction:column; gap:12px; max-height:min(280px, calc(100dvh - 200px)); padding:16px; border:1px solid #b3e2ff; border-radius:28px; background:linear-gradient(125deg,#f1faff 0%,#fff 54%,#f6ffe6 100%); color:#111827; box-shadow:0 2px 3px #12243918; }
.affiliate-promo-collapsed { padding:12px 10px; border-radius:999px; }
.promo-heading { display:flex; flex-shrink:0; align-items:center; gap:6px; }
.promo-heading-link { display:flex; flex:1; min-width:0; align-items:center; gap:10px; font-size:15px; font-weight:750; line-height:1.3; }
.promo-icon,.promo-compact { display:flex; flex-shrink:0; align-items:center; justify-content:center; width:36px; height:36px; border-radius:50%; background:#cffafe; color:#0073b6; }
.promo-icon svg,.promo-compact svg { width:25px; height:25px; }
.promo-toggle { display:flex; flex-shrink:0; align-items:center; justify-content:center; width:32px; height:32px; border:1px solid #b3e2ff; border-radius:50%; background:#ffffffed; color:#0073b6; box-shadow:0 2px 3px #12243915; }
.promo-toggle svg { width:19px; height:19px; }
.promo-body { min-height:0; overflow-y:auto; font-size:14px; line-height:1.9; color:#64748b; }
.promo-rate { position:relative; display:inline-block; margin:3px 0 7px; padding:3px 16px; color:#ff346d; font-size:20px; font-weight:700; }
.promo-rate svg { position:absolute; inset:0; width:100%; height:100%; pointer-events:none; }
.promo-body p { text-decoration:underline; text-decoration-color:#10b981; text-underline-offset:5px; text-decoration-thickness:1px; }
.promo-action { display:flex; flex-shrink:0; align-items:center; justify-content:space-between; margin-top:4px; font-size:16px; font-weight:700; color:#0073b6; }
.promo-arrow { display:flex; align-items:center; justify-content:center; width:38px; height:38px; border:1px solid #e5e7eb; border-radius:50%; background:#ffffffb3; font-size:23px; transition:transform .2s; }
.promo-action:hover .promo-arrow { transform:translate(2px,-2px); }
.promo-heading-link:focus-visible,.promo-toggle:focus-visible,.promo-action:focus-visible,.promo-compact:focus-visible { outline:2px solid #0073b6; outline-offset:3px; border-radius:8px; }
@media (prefers-reduced-motion:reduce) { .promo-arrow { transition:none; } .promo-action:hover .promo-arrow { transform:none; } }
</style>
