<template>
  <section class="brand-hero" data-testid="brand-hero">
    <div class="portal" data-streams-focal>
      <span class="ring ring-outer"></span>
      <span class="ring ring-inner"></span>
      <img :src="siteLogo || '/logo.svg'" alt="Logo" />
    </div>

    <h1 class="hero-title"><BrandName :name="siteName" /></h1>
    <p class="hero-subtitle">{{ siteSubtitle }}</p>

    <div v-if="isAuthenticated" class="hero-cta">
      <router-link :to="dashboardPath" class="hero-btn hero-btn-primary" data-testid="hero-dashboard">
        {{ t('home.goToDashboard') }}
        <Icon name="arrowRight" size="sm" />
      </router-link>
    </div>
    <template v-else>
      <div class="hero-cta">
        <router-link v-if="showRegister" to="/register" class="hero-btn hero-btn-primary" data-testid="hero-register">
          {{ t('auth.signUp') }}
          <Icon name="arrowRight" size="sm" />
        </router-link>
        <router-link
          to="/login"
          class="hero-btn"
          :class="showRegister ? 'hero-btn-ghost' : 'hero-btn-primary'"
          data-testid="hero-login"
        >
          {{ t('home.login') }}
        </router-link>
      </div>
      <p v-if="showRegister" class="hero-gift" data-testid="hero-signup-bonus">
        <Icon name="gift" size="sm" />
        {{ t('community.signupBonus') }}
      </p>
    </template>

    <QqGroupCard />
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import BrandName from './BrandName.vue'
import QqGroupCard from './QqGroupCard.vue'

defineProps<{
  siteName: string
  siteLogo: string
  siteSubtitle: string
  isAuthenticated: boolean
  dashboardPath: string
  showRegister: boolean
}>()

const { t } = useI18n()
</script>

<style scoped>
.brand-hero {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  width: 100%;
  max-width: 780px;
  text-align: center;
}

/* 文字后方的淡化底，保证标题和按钮在特效上方清晰 */
.brand-hero::before {
  content: '';
  position: absolute;
  left: 50%;
  top: 62%;
  z-index: -1;
  width: min(900px, 150vw);
  height: 600px;
  transform: translate(-50%, -50%);
  background: radial-gradient(closest-side, rgba(244, 247, 250, 0.9) 0%, rgba(244, 247, 250, 0.9) 48%, transparent 100%);
  pointer-events: none;
}

.portal {
  position: relative;
  width: 88px;
  height: 88px;
  margin-bottom: 36px;
}

.portal img {
  position: relative;
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
  border-radius: 22px;
  box-shadow: 0 18px 50px -12px rgba(15, 40, 90, 0.55);
}

.ring {
  position: absolute;
  border-radius: 50%;
  pointer-events: none;
}

.ring-inner {
  inset: -22px;
  background: conic-gradient(from 0deg, transparent 0 58%, rgba(13, 148, 136, 0.75) 86%, transparent 100%);
  -webkit-mask: radial-gradient(farthest-side, transparent calc(100% - 1.6px), #000 calc(100% - 1.4px));
  mask: radial-gradient(farthest-side, transparent calc(100% - 1.6px), #000 calc(100% - 1.4px));
  animation: brand-spin 7s linear infinite;
}

.ring-outer {
  inset: -40px;
  border: 1px dashed rgba(8, 145, 178, 0.32);
  animation: brand-spin 48s linear infinite reverse;
}

@keyframes brand-spin {
  to {
    transform: rotate(360deg);
  }
}

.hero-title {
  font-size: clamp(44px, 7.4vw, 76px);
  line-height: 1.04;
  font-weight: 800;
  letter-spacing: -0.035em;
  overflow-wrap: anywhere;
  color: #0b1220;
}

.hero-subtitle {
  margin-top: 16px;
  font-size: clamp(14px, 2.1vw, 19px);
  line-height: 1.7;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  text-wrap: balance;
  color: #566275;
}

.hero-cta {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 12px;
  margin-top: 36px;
}

.hero-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-width: 140px;
  height: 50px;
  padding: 0 28px;
  border: 1px solid transparent;
  border-radius: 12px;
  font-size: 16px;
  font-weight: 650;
  transition: transform 0.15s, box-shadow 0.2s, background 0.2s;
}

.hero-btn-primary {
  color: #fff;
  background: linear-gradient(135deg, #0d9488 0%, #0891b2 100%);
  box-shadow: 0 12px 30px -12px rgba(13, 148, 136, 0.75);
}

.hero-btn-primary:hover {
  transform: translateY(-1px);
  box-shadow: 0 16px 36px -12px rgba(13, 148, 136, 0.9);
}

.hero-btn-ghost {
  color: #0b1220;
  background: rgba(255, 255, 255, 0.66);
  border-color: rgba(15, 23, 42, 0.09);
  -webkit-backdrop-filter: blur(12px);
  backdrop-filter: blur(12px);
}

.hero-btn-ghost:hover {
  background: rgba(255, 255, 255, 0.92);
}

.hero-gift {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-top: 16px;
  font-size: 14px;
  font-weight: 550;
  color: #0f766e;
}

.dark .brand-hero::before {
  background: radial-gradient(closest-side, rgba(3, 7, 18, 0.84) 0%, rgba(3, 7, 18, 0.84) 48%, transparent 100%);
}

.dark .portal img {
  box-shadow: 0 18px 60px -10px rgba(57, 217, 231, 0.4);
}

.dark .ring-inner {
  background: conic-gradient(from 0deg, transparent 0 58%, rgba(121, 244, 189, 0.8) 86%, transparent 100%);
}

.dark .ring-outer {
  border-color: rgba(57, 217, 231, 0.3);
}

.dark .hero-title {
  color: #eef2f7;
}

.dark .hero-subtitle {
  color: #9aa6b8;
}

.dark .hero-btn-ghost {
  color: #eef2f7;
  background: rgba(15, 23, 42, 0.55);
  border-color: rgba(148, 163, 184, 0.15);
}

.dark .hero-btn-ghost:hover {
  background: rgba(12, 19, 36, 0.92);
}

.dark .hero-gift {
  color: #5eead4;
}

@media (max-width: 640px) {
  .portal {
    width: 76px;
    height: 76px;
    margin-bottom: 30px;
  }

  .portal img {
    border-radius: 19px;
  }

  .hero-cta {
    width: 100%;
    max-width: 360px;
  }

  .hero-btn {
    flex: 1;
    min-width: 0;
    padding: 0 16px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .ring {
    animation: none;
  }
}
</style>
