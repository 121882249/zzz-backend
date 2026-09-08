<template>
  <div v-if="variant === 'cosmic'" class="auth-cosmic">
    <div class="auth-cosmic-backdrop" aria-hidden="true"></div>

    <section class="auth-cosmic-story" aria-label="TokenPro">
      <router-link to="/home" class="auth-cosmic-brand" aria-label="TokenPro home">
        <img src="/brand/tokenpro-orbit.webp" alt="" />
        <span>TokenPro</span>
      </router-link>

      <div class="auth-cosmic-copy">
        <p>{{ t('auth.cosmic.kicker') }}</p>
        <h1>
          {{ t('auth.cosmic.titleLineOne') }}<br />
          <span>{{ t('auth.cosmic.titleHighlight') }}</span>
        </h1>
        <div class="auth-cosmic-line" aria-hidden="true"><i></i><i></i><i></i></div>
        <p class="auth-cosmic-description">{{ t('auth.cosmic.description') }}</p>
        <div class="auth-cosmic-models" aria-label="GPT, Claude, Gemini, Grok">
          <span v-for="model in authModels" :key="model.name">
            <img :src="model.icon" alt="" />{{ model.name }}
          </span>
        </div>
      </div>

      <p class="auth-cosmic-caption">ONE API · MORE INTELLIGENCE</p>
    </section>

    <main class="auth-cosmic-form-pane">
      <router-link to="/home" class="auth-cosmic-mobile-brand" aria-label="TokenPro home">
        <img src="/brand/tokenpro-orbit.webp" alt="" />
        <span>TokenPro</span>
      </router-link>
      <div class="auth-cosmic-card">
        <slot />
      </div>
      <div class="auth-cosmic-footer">
        <slot name="footer" />
      </div>
      <p class="auth-cosmic-copyright">&copy; {{ currentYear }} TokenPro. All rights reserved.</p>
    </main>
  </div>

  <div v-else class="relative flex min-h-screen items-center justify-center overflow-hidden p-4">
    <div
      class="absolute inset-0 bg-gradient-to-br from-gray-50 via-primary-50/30 to-gray-100 dark:from-dark-950 dark:via-dark-900 dark:to-dark-950"
    ></div>

    <div class="pointer-events-none absolute inset-0 overflow-hidden">
      <div
        class="absolute -right-40 -top-40 h-80 w-80 rounded-full bg-primary-400/20 blur-3xl"
      ></div>
      <div
        class="absolute -bottom-40 -left-40 h-80 w-80 rounded-full bg-primary-500/15 blur-3xl"
      ></div>
      <div
        class="absolute left-1/2 top-1/2 h-96 w-96 -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary-300/10 blur-3xl"
      ></div>
      <div
        class="absolute inset-0 bg-[linear-gradient(rgba(20,184,166,0.03)_1px,transparent_1px),linear-gradient(90deg,rgba(20,184,166,0.03)_1px,transparent_1px)] bg-[size:64px_64px]"
      ></div>
    </div>

    <div class="relative z-10 w-full max-w-md">
      <div class="mb-8 text-center">
        <template v-if="settingsLoaded">
          <div
            class="mb-4 inline-flex h-16 w-16 items-center justify-center overflow-hidden rounded-2xl shadow-lg shadow-primary-500/30"
          >
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <h1 class="text-gradient mb-2 text-3xl font-bold">{{ siteName }}</h1>
          <p class="text-sm text-gray-500 dark:text-dark-400">{{ siteSubtitle }}</p>
        </template>
      </div>

      <div class="card-glass rounded-2xl p-8 shadow-glass"><slot /></div>
      <div class="mt-6 text-center text-sm"><slot name="footer" /></div>
      <div class="mt-8 text-center text-xs text-gray-400 dark:text-dark-500">
        &copy; {{ currentYear }} {{ siteName }}. All rights reserved.
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

withDefaults(defineProps<{ variant?: 'default' | 'cosmic' }>(), { variant: 'default' })

const { t } = useI18n()
const appStore = useAppStore()
const siteName = computed(() => appStore.siteName || 'Sub2API')
const siteLogo = computed(() =>
  sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true })
)
const siteSubtitle = computed(
  () => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform'
)
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)
const currentYear = computed(() => new Date().getFullYear())
const authModels = [
  { name: 'GPT', icon: '/brand/model-openai.svg' },
  { name: 'Claude', icon: '/brand/model-claude.svg' },
  { name: 'Gemini', icon: '/brand/model-gemini.svg' },
  { name: 'Grok', icon: '/brand/model-grok.svg' }
]

onMounted(() => appStore.fetchPublicSettings())
</script>

<style scoped>
.text-gradient {
  @apply bg-gradient-to-r from-primary-600 to-primary-500 bg-clip-text text-transparent;
}

.auth-cosmic {
  position: relative;
  display: grid;
  min-height: 100vh;
  grid-template-columns: minmax(0, 1.08fr) minmax(480px, 0.92fr);
  overflow: hidden;
  color: #f7f9ff;
  background: #020612;
  color-scheme: dark;
}

.auth-cosmic-backdrop {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(90deg, rgba(1, 5, 17, 0.08), rgba(1, 5, 17, 0.38) 58%, #020612 78%),
    linear-gradient(180deg, rgba(1, 6, 20, 0.05), rgba(1, 6, 20, 0.42)),
    url('/brand/orbital-gateway-bg.webp') center / cover;
}

.auth-cosmic-story,
.auth-cosmic-form-pane {
  position: relative;
  z-index: 1;
}

.auth-cosmic-story {
  display: flex;
  min-height: 100vh;
  flex-direction: column;
  justify-content: space-between;
  padding: 44px clamp(42px, 5vw, 88px) 40px;
}

.auth-cosmic-brand,
.auth-cosmic-mobile-brand {
  display: inline-flex;
  width: fit-content;
  align-items: center;
  gap: 13px;
  color: #fff;
  font-size: 25px;
  font-weight: 760;
  letter-spacing: -0.035em;
}

.auth-cosmic-brand img,
.auth-cosmic-mobile-brand img {
  width: 48px;
  height: 48px;
  border: 1px solid rgba(111, 185, 255, 0.5);
  border-radius: 14px;
  box-shadow: 0 0 28px rgba(89, 88, 255, 0.32);
}

.auth-cosmic-copy {
  max-width: 690px;
  margin: 70px 0;
}

.auth-cosmic-copy > p:first-child,
.auth-cosmic-caption {
  color: #9abfff;
  font-size: 11px;
  font-weight: 650;
  letter-spacing: 0.34em;
}

.auth-cosmic-copy h1 {
  margin: 22px 0 0;
  color: #fff;
  font-size: clamp(50px, 5vw, 76px);
  font-weight: 810;
  letter-spacing: -0.055em;
  line-height: 1.08;
}

.auth-cosmic-copy h1 span {
  background: linear-gradient(100deg, #effbff, #5be7ff 32%, #7584ff 66%, #b06eff);
  background-clip: text;
  color: transparent;
}

.auth-cosmic-line {
  display: flex;
  align-items: center;
  width: min(440px, 78%);
  height: 1px;
  margin: 34px 0;
  background: linear-gradient(90deg, rgba(82, 226, 255, 0.86), rgba(115, 73, 255, 0));
}

.auth-cosmic-line i {
  width: 7px;
  height: 7px;
  margin-right: 96px;
  border-radius: 999px;
  background: #63e0ff;
  box-shadow: 0 0 16px #4f8fff;
}

.auth-cosmic-description {
  max-width: 600px;
  color: #bec9df;
  font-size: 16px;
  line-height: 1.85;
}

.auth-cosmic-models {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 34px;
}

.auth-cosmic-models span {
  display: inline-flex;
  min-height: 42px;
  align-items: center;
  gap: 9px;
  padding: 0 15px;
  border: 1px solid rgba(102, 195, 255, 0.32);
  border-radius: 12px;
  color: #dbe7ff;
  background: rgba(5, 16, 45, 0.62);
  backdrop-filter: blur(12px);
  font-size: 12px;
  font-weight: 650;
}

.auth-cosmic-models img {
  width: 20px;
  height: 20px;
}

.auth-cosmic-caption {
  color: #667ca8;
  font-size: 9px;
}

.auth-cosmic-form-pane {
  display: flex;
  min-height: 100vh;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  overflow-y: auto;
  padding: 44px clamp(30px, 4vw, 72px);
  border-left: 1px solid rgba(94, 157, 255, 0.16);
  background: linear-gradient(135deg, rgba(2, 7, 22, 0.78), rgba(2, 6, 18, 0.96));
  backdrop-filter: blur(18px);
}

.auth-cosmic-mobile-brand {
  display: none;
}

.auth-cosmic-card {
  width: min(100%, 480px);
  padding: clamp(30px, 4vw, 44px);
  border: 1px solid rgba(94, 190, 255, 0.34);
  border-radius: 24px;
  background: linear-gradient(145deg, rgba(7, 20, 52, 0.9), rgba(3, 10, 31, 0.86));
  box-shadow:
    0 30px 90px rgba(0, 0, 0, 0.45),
    0 0 54px rgba(65, 95, 255, 0.16),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
}

.auth-cosmic-footer {
  min-height: 22px;
  margin-top: 25px;
  color: #8795b5;
  font-size: 14px;
  text-align: center;
}

.auth-cosmic-footer :deep(button) {
  margin-inline: auto;
  color: #8997b6;
}

.auth-cosmic-copyright {
  margin-top: 28px;
  color: #566687;
  font-size: 11px;
}

.auth-cosmic :deep(h2) {
  color: #f8fbff !important;
  font-size: 28px;
  letter-spacing: -0.025em;
}

.auth-cosmic :deep(.input-label) {
  color: #c9d5eb;
  font-size: 13px;
}

.auth-cosmic :deep(.input) {
  min-height: 48px;
  border-color: rgba(101, 151, 220, 0.3);
  color: #f5f8ff;
  background: rgba(2, 9, 28, 0.75);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.035);
}

.auth-cosmic :deep(.input::placeholder) {
  color: #667391;
}

.auth-cosmic :deep(.input:focus) {
  border-color: #52dfff;
  box-shadow: 0 0 0 3px rgba(70, 193, 255, 0.12), 0 0 20px rgba(68, 99, 255, 0.14);
}

.auth-cosmic :deep(.btn-primary) {
  min-height: 50px;
  border: 1px solid rgba(100, 225, 255, 0.76);
  border-radius: 12px;
  background: linear-gradient(110deg, #2dafff, #5d5dff 52%, #7946ff);
  box-shadow: 0 14px 38px rgba(49, 92, 255, 0.3);
}

.auth-cosmic :deep(.btn-secondary) {
  border-color: rgba(101, 151, 220, 0.34);
  color: #e7edff;
  background: rgba(8, 21, 52, 0.8);
}

.auth-cosmic :deep(.text-gray-500),
.auth-cosmic :deep(.text-gray-400),
.auth-cosmic :deep(.text-gray-700),
.auth-cosmic :deep(.dark\:text-gray-300),
.auth-cosmic :deep(.dark\:text-dark-400),
.auth-cosmic :deep(.dark\:text-dark-500) {
  color: #8997b6 !important;
}

.auth-cosmic :deep(.text-primary-600),
.auth-cosmic :deep(.dark\:text-primary-400) {
  color: #62dcff !important;
}

.auth-cosmic a:focus-visible,
.auth-cosmic button:focus-visible {
  outline: 2px solid #58e0ff;
  outline-offset: 4px;
}

@media (max-width: 920px) {
  .auth-cosmic {
    display: block;
    min-height: 100vh;
    overflow-y: auto;
  }

  .auth-cosmic-story {
    display: none;
  }

  .auth-cosmic-form-pane {
    min-height: 100vh;
    padding: 32px 20px;
    border-left: 0;
    background: rgba(2, 7, 22, 0.74);
  }

  .auth-cosmic-mobile-brand {
    display: inline-flex;
    margin-bottom: 28px;
  }
}

@media (max-width: 520px) {
  .auth-cosmic-form-pane {
    justify-content: flex-start;
    padding: 24px 15px 30px;
  }

  .auth-cosmic-card {
    padding: 26px 20px;
    border-radius: 20px;
  }
}
</style>
