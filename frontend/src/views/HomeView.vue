<template>
  <div v-if="hasHomeContent" class="min-h-screen">
    <iframe v-if="isHomeContentUrl" :src="homeContent.trim()" class="h-screen w-full border-0" allowfullscreen></iframe>
    <div v-else v-html="homeContent"></div>
  </div>

  <div v-else-if="compactHomeEnabled" data-testid="compact-home" class="flex min-h-screen flex-col bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <header class="border-b border-gray-200 px-4 py-4 sm:px-6 dark:border-dark-800">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img :src="brandLogo" alt="TokenPro" class="h-9 w-9 shrink-0 rounded-lg object-cover" />
          <span class="min-w-0 truncate text-base font-semibold">{{ brandName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          <LocaleSwitcher />
          <button class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800" :title="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="toggleTheme">
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link v-if="!isAuthenticated" data-testid="header-login" to="/login" class="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium dark:border-dark-600">{{ t('home.login') }}</router-link>
          <router-link data-testid="header-register" :to="isAuthenticated ? dashboardPath : '/register'" class="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg bg-primary-600 px-4 py-2 text-sm font-medium text-white hover:bg-primary-700">{{ isAuthenticated ? t('home.dashboard') : t('auth.signUp') }}</router-link>
        </div>
      </nav>
    </header>
    <main class="flex min-w-0 flex-1 items-center justify-center px-4 py-16 sm:px-6">
      <div class="min-w-0 max-w-2xl text-center">
        <img :src="brandLogo" alt="TokenPro" class="mx-auto mb-6 h-20 w-20 rounded-2xl object-cover" />
        <h1 class="[overflow-wrap:anywhere] text-3xl font-bold md:text-4xl">{{ brandName }}</h1>
        <p class="mt-4 whitespace-pre-wrap [overflow-wrap:anywhere] text-base text-gray-600 dark:text-dark-300">{{ siteSubtitle }}</p>
        <router-link data-testid="compact-primary-action" :to="isAuthenticated ? dashboardPath : '/register'" class="mt-8 inline-flex min-h-10 items-center justify-center rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-primary-700">{{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}</router-link>
      </div>
    </main>
    <footer class="min-w-0 border-t border-gray-200 px-4 py-5 text-center text-sm text-gray-500 sm:px-6 dark:border-dark-800 dark:text-dark-400">&copy; {{ currentYear }} {{ brandName }}</footer>
  </div>

  <div v-else data-testid="cosmic-home" class="cosmic-home">
    <div class="cosmic-backdrop" aria-hidden="true"></div>
    <header class="cosmic-header">
      <nav class="cosmic-nav" aria-label="Primary navigation">
        <router-link to="/" class="brand-lockup" aria-label="TokenPro home">
          <img :src="brandLogo" alt="" class="brand-mark" />
          <span>{{ brandName }}</span>
        </router-link>
        <div class="header-actions">
          <LocaleSwitcher />
          <span class="header-divider" aria-hidden="true"></span>
          <button class="theme-button" :title="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="toggleTheme">
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link v-if="isAuthenticated" :to="dashboardPath" class="register-button">{{ t('home.dashboard') }}</router-link>
          <template v-else>
            <router-link data-testid="header-login" to="/login" class="login-button">{{ t('home.login') }}</router-link>
            <router-link data-testid="header-register" to="/register" class="register-button">{{ t('auth.signUp') }}</router-link>
          </template>
        </div>
      </nav>
    </header>

    <main class="cosmic-main">
      <section data-testid="download-dock" class="download-dock" aria-labelledby="download-dock-title">
        <div class="download-intro">
          <span class="download-symbol" aria-hidden="true"><Icon name="download" size="lg" /></span>
          <div>
            <p class="download-eyebrow">{{ t('home.cosmic.downloadEyebrow') }}</p>
            <h2 id="download-dock-title">{{ t('home.cosmic.downloadTitle') }}</h2>
          </div>
        </div>
        <div class="download-platforms" :aria-label="t('home.cosmic.platformsAriaLabel')">
          <button
            v-for="platform in downloadPlatforms"
            :key="platform.key"
            type="button"
            :data-testid="`download-platform-${platform.key}`"
            :class="['download-platform', { 'download-platform--active': selectedDownloadPlatform === platform.key }]"
            :aria-pressed="selectedDownloadPlatform === platform.key"
            @click="selectedDownloadPlatform = platform.key"
          >
            <Icon :name="platform.icon" size="sm" />
            <span>{{ t(platform.labelKey) }}</span>
          </button>
        </div>
        <div v-if="selectedDownloadPlatform" data-testid="download-builds" class="download-builds">
          <span class="download-builds-title">
            {{ t('home.cosmic.downloadVersionsLabel', { platform: selectedDownloadPlatformLabel }) }}
          </span>
          <div class="download-build-list">
            <article v-for="build in selectedDownloadBuilds" :key="build.id" class="download-build-card">
              <span class="download-build-icon" aria-hidden="true"><Icon name="cpu" size="sm" /></span>
              <span class="download-build-copy">
                <strong>{{ t(build.labelKey) }}</strong>
                <small>{{ build.architecture }}</small>
              </span>
              <a
                v-if="build.url"
                :href="build.url"
                class="download-build-action"
                rel="noopener noreferrer"
              >
                <Icon name="download" size="sm" />{{ t('home.cosmic.downloadAction') }}
              </a>
              <button v-else type="button" class="download-build-action" disabled>
                <Icon name="download" size="sm" />{{ t('home.cosmic.downloadAction') }}
              </button>
            </article>
          </div>
        </div>
      </section>

      <section class="hero-shell" aria-labelledby="home-hero-title">
        <div class="hero-copy">
          <p class="hero-kicker">{{ t('home.cosmic.kicker') }}</p>
          <h1 id="home-hero-title" class="hero-title">
            {{ t('home.cosmic.titleLineOne') }} <span>{{ t('home.cosmic.titleHighlight') }}</span>
          </h1>
          <p class="hero-description">{{ t('home.cosmic.description') }}</p>
          <router-link data-testid="hero-primary-action" :to="isAuthenticated ? dashboardPath : '/register'" class="hero-cta">
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
            <Icon name="arrowRight" size="md" :stroke-width="2" />
          </router-link>
          <div class="benefit-row">
            <div class="benefit-item"><Icon name="bolt" size="lg" /><span><strong>{{ t('home.features.multiAccount') }}</strong><small>{{ t('home.cosmic.stability') }}</small></span></div>
            <div class="benefit-item"><Icon name="shield" size="lg" /><span><strong>{{ t('home.features.unifiedGateway') }}</strong><small>{{ t('home.cosmic.easyAccess') }}</small></span></div>
            <div class="benefit-item"><Icon name="chart" size="lg" /><span><strong>{{ t('home.features.balanceQuota') }}</strong><small>{{ t('home.cosmic.transparentBilling') }}</small></span></div>
          </div>
        </div>

        <div class="hero-visual" aria-label="TokenPro multi-model API routing preview">
          <div v-for="model in representativeModels" :key="model.key" :class="['floating-model', `floating-model--${model.key}`]">
            <img :src="model.icon" alt="" /><span>{{ model.label }}</span>
          </div>
          <div data-testid="hero-more-models" class="floating-model floating-model--more-family">
            <Icon name="sparkles" size="lg" />
            <span>{{ t('home.cosmic.moreModelFamily') }}</span>
          </div>
          <div class="terminal-container">
            <div class="terminal-window">
              <div class="terminal-header">
                <div class="terminal-lights" aria-hidden="true"><i></i><i></i><i></i></div>
                <span>POST&nbsp;&nbsp;/v1/messages</span><b>200 OK</b>
              </div>
              <div class="terminal-content">
                <pre><span class="terminal-muted">$</span> curl -X POST https://api.tokenpro.work/v1/messages \
  -H <span class="terminal-gold">"Authorization: Bearer sk-••••••••"</span> \
  -H <span class="terminal-gold">"Content-Type: application/json"</span> \
  -d '{
    <span class="terminal-cyan">"model"</span>: <span class="terminal-green">"gpt-5.6-sol"</span>,
    <span class="terminal-cyan">"messages"</span>: [{ <span class="terminal-cyan">"role"</span>: <span class="terminal-green">"user"</span> }]
  }'</pre>
                <div class="terminal-meta"><span>{{ t('home.cosmic.unifiedApi') }}</span><span>{{ t('home.cosmic.multiModel') }}</span><span>{{ t('home.cosmic.concurrent') }}</span></div>
              </div>
            </div>
          </div>
        </div>
      </section>

    </main>
    <footer class="cosmic-footer"><span>&copy; {{ currentYear }} {{ brandName }}. {{ t('home.footer.allRightsReserved') }}</span><span>ONE API · MORE INTELLIGENCE</span></footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const brandLogo = '/brand/tokenpro-orbit.webp'
const brandName = 'TokenPro'
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const isHomeContentUrl = computed(() => /^https?:\/\//.test(homeContent.value.trim()))

const representativeModels = [
  { key: 'gpt', label: 'GPT', icon: '/brand/model-openai.svg' },
  { key: 'claude', label: 'Claude', icon: '/brand/model-claude.svg' },
  { key: 'gemini', label: 'Gemini', icon: '/brand/model-gemini.svg' },
  { key: 'grok', label: 'Grok', icon: '/brand/model-grok.svg' },
] as const
type DownloadPlatformKey = 'macos' | 'windows' | 'linux'
type DownloadPlatform = {
  key: DownloadPlatformKey
  labelKey: 'home.cosmic.platformMacos' | 'home.cosmic.platformWindows' | 'home.cosmic.platformLinux'
  icon: 'cpu' | 'grid' | 'terminal'
}
type DownloadBuild = {
  id: string
  labelKey: 'home.cosmic.buildAppleSilicon' | 'home.cosmic.buildIntel' | 'home.cosmic.buildX64' | 'home.cosmic.buildArm64'
  architecture: 'arm64' | 'x86_64'
  url: string
}
const downloadPlatforms: DownloadPlatform[] = [
  { key: 'macos', labelKey: 'home.cosmic.platformMacos', icon: 'cpu' },
  { key: 'windows', labelKey: 'home.cosmic.platformWindows', icon: 'grid' },
  { key: 'linux', labelKey: 'home.cosmic.platformLinux', icon: 'terminal' },
]
const downloadBuilds: Record<DownloadPlatformKey, DownloadBuild[]> = {
  macos: [
    { id: 'macos-arm64', labelKey: 'home.cosmic.buildAppleSilicon', architecture: 'arm64', url: '' },
    { id: 'macos-x64', labelKey: 'home.cosmic.buildIntel', architecture: 'x86_64', url: '' },
  ],
  windows: [
    { id: 'windows-x64', labelKey: 'home.cosmic.buildX64', architecture: 'x86_64', url: '' },
    { id: 'windows-arm64', labelKey: 'home.cosmic.buildArm64', architecture: 'arm64', url: '' },
  ],
  linux: [
    { id: 'linux-x64', labelKey: 'home.cosmic.buildX64', architecture: 'x86_64', url: '' },
    { id: 'linux-arm64', labelKey: 'home.cosmic.buildArm64', architecture: 'arm64', url: '' },
  ],
}
const selectedDownloadPlatform = ref<DownloadPlatformKey | null>(null)
const selectedDownloadPlatformLabel = computed(() => {
  const platform = downloadPlatforms.find((item) => item.key === selectedDownloadPlatform.value)
  return platform ? t(platform.labelKey) : ''
})
const selectedDownloadBuilds = computed(() => selectedDownloadPlatform.value
  ? downloadBuilds[selectedDownloadPlatform.value]
  : [])
const isDark = ref(document.documentElement.classList.contains('dark'))
const isAuthenticated = computed(() => authStore.isAuthenticated)
const dashboardPath = computed(() => authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
const currentYear = computed(() => new Date().getFullYear())

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}
function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}
function detectDownloadPlatform() {
  const browserNavigator = navigator as Navigator & { userAgentData?: { platform?: string } }
  const signature = [browserNavigator.userAgentData?.platform, navigator.platform, navigator.userAgent]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()

  let detected: DownloadPlatformKey | null = null
  if (/windows|win32|win64/.test(signature)) detected = 'windows'
  else if (/macintosh|mac os|macintel/.test(signature)) detected = 'macos'
  else if (/linux|x11/.test(signature)) detected = 'linux'

  selectedDownloadPlatform.value = detected
}
onMounted(async () => {
  initTheme()
  detectDownloadPlatform()
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) await appStore.fetchPublicSettings()
})
</script>

<style scoped>
.cosmic-home{--cyan:#4fe4ff;--violet:#7757ff;position:relative;min-height:100vh;overflow:hidden;color:#f7f9ff;background:#020612;color-scheme:dark}.cosmic-backdrop{position:absolute;inset:0 0 auto;height:min(830px,82vh);background-image:linear-gradient(90deg,rgba(2,6,18,.28),rgba(2,6,18,.08) 42%,rgba(2,6,18,.05)),linear-gradient(180deg,rgba(2,6,18,.08) 65%,#020612),url('/brand/orbital-gateway-bg.webp');background-position:center top;background-size:cover;pointer-events:none}.cosmic-header{position:relative;z-index:20}.cosmic-nav{display:flex;align-items:center;justify-content:space-between;width:min(1380px,calc(100% - 72px));min-height:104px;margin:auto}.brand-lockup{display:inline-flex;align-items:center;gap:14px;color:#fff;font-size:25px;font-weight:760;letter-spacing:-.035em;text-decoration:none}.brand-mark{width:48px;height:48px;border-radius:14px;object-fit:cover;box-shadow:0 0 0 1px rgba(122,174,255,.38),0 0 28px rgba(93,104,255,.35)}.header-actions{display:flex;align-items:center;gap:13px}.header-divider{width:1px;height:28px;background:rgba(150,175,232,.28)}.theme-button,.login-button,.register-button{display:inline-flex;min-width:46px;min-height:44px;align-items:center;justify-content:center;border-radius:12px;color:#eef4ff;transition:180ms ease}.theme-button{border:0;background:transparent;cursor:pointer}.theme-button:hover{background:rgba(79,228,255,.1)}.login-button{padding:0 20px;border:1px solid rgba(88,200,255,.72);background:rgba(5,16,42,.5);font-size:14px;font-weight:650}.register-button{padding:0 22px;border:1px solid rgba(99,214,255,.72);background:linear-gradient(110deg,rgba(50,172,255,.92),rgba(108,74,255,.94));box-shadow:0 10px 34px rgba(62,105,255,.3);font-size:14px;font-weight:700}.login-button:hover,.register-button:hover{transform:translateY(-1px);box-shadow:0 12px 34px rgba(50,169,255,.3)}.theme-button:focus-visible,.login-button:focus-visible,.register-button:focus-visible,.hero-cta:focus-visible,.brand-lockup:focus-visible{outline:2px solid var(--cyan);outline-offset:4px}.cosmic-main{position:relative;z-index:10}.hero-shell{display:grid;grid-template-columns:minmax(0,.88fr) minmax(520px,1.12fr);gap:40px;align-items:center;width:min(1380px,calc(100% - 72px));min-height:690px;margin:auto;padding:38px 0 50px}.hero-copy{max-width:650px}.hero-kicker,.section-kicker{margin:0 0 20px;color:#8fbaff;font-size:12px;font-weight:650;letter-spacing:.38em}.hero-title{margin:0;color:#fbfdff;font-size:clamp(48px,5.2vw,78px);font-weight:820;letter-spacing:-.055em;line-height:1.08;text-shadow:0 3px 28px rgba(49,98,221,.18)}.hero-title span{background:linear-gradient(100deg,#eafaff,#5be7ff 35%,#7485ff 68%,#ae72ff);-webkit-background-clip:text;background-clip:text;color:transparent}.hero-description{max-width:590px;margin:30px 0 0;color:#c3cce2;font-size:17px;line-height:1.85}.hero-cta{display:inline-flex;min-height:58px;align-items:center;gap:14px;margin-top:38px;padding:0 30px;border:1px solid rgba(92,224,255,.84);border-radius:13px;color:#fff;background:linear-gradient(110deg,#2eafff,#5864ff 48%,#7546ff);box-shadow:0 16px 50px rgba(45,111,255,.36),inset 0 1px 0 rgba(255,255,255,.24);font-size:17px;font-weight:720;transition:180ms ease}.hero-cta:hover{transform:translateY(-2px);box-shadow:0 20px 58px rgba(60,106,255,.48)}.benefit-row{display:flex;margin-top:58px}.benefit-item{display:flex;min-width:0;align-items:center;gap:13px;padding:0 27px;color:var(--cyan);border-left:1px solid rgba(118,151,218,.28)}.benefit-item:first-child{padding-left:0;border-left:0}.benefit-item span{display:flex;flex-direction:column;min-width:0}.benefit-item strong{color:#f4f7ff;font-size:14px;font-weight:700;white-space:nowrap}.benefit-item small{margin-top:5px;color:#8594b7;font-size:11px;white-space:nowrap}.hero-visual{position:relative;min-height:590px}.floating-model{position:absolute;z-index:4;display:flex;width:84px;min-height:84px;flex-direction:column;align-items:center;justify-content:center;gap:7px;border:1px solid rgba(95,213,255,.52);border-radius:18px;color:#fff;background:rgba(4,15,43,.72);box-shadow:0 15px 45px rgba(26,79,221,.28),inset 0 1px 0 rgba(255,255,255,.16);backdrop-filter:blur(14px)}.floating-model img{width:29px;height:29px;object-fit:contain}.floating-model span{font-size:12px;font-weight:680}.floating-model--gpt{top:65px;left:28%}.floating-model--claude{top:55px;right:4%}.floating-model--gemini{top:210px;right:-2%}.floating-model--grok{top:242px;left:18%}.terminal-container{position:absolute;z-index:5;right:0;bottom:22px;width:min(610px,92%)}.terminal-window{overflow:hidden;border:1px solid rgba(84,186,255,.5);border-radius:18px;background:rgba(2,10,30,.88);box-shadow:0 30px 80px rgba(0,0,0,.48),0 0 55px rgba(44,96,255,.18),inset 0 1px 0 rgba(255,255,255,.12);backdrop-filter:blur(18px);transform:perspective(1200px) rotateX(1deg) rotateY(-1.5deg);transition:transform 250ms ease}.terminal-window:hover{transform:none}.terminal-header{display:grid;grid-template-columns:1fr auto 1fr;align-items:center;min-height:48px;padding:0 18px;border-bottom:1px solid rgba(95,129,193,.18);color:#8eb6e8;font:11px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace}.terminal-header b{justify-self:end;color:#55efc4;font-weight:600}.terminal-lights{display:flex;gap:7px}.terminal-lights i{width:9px;height:9px;border-radius:999px;background:#ff6f67}.terminal-lights i:nth-child(2){background:#ffca58}.terminal-lights i:nth-child(3){background:#4bd98b}.terminal-content{display:grid;grid-template-columns:minmax(0,1fr) 128px;gap:18px;padding:20px 22px 24px}.terminal-content pre{min-width:0;overflow:hidden;margin:0;color:#b7c9e8;font:11px/1.72 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap}.terminal-muted{color:#67e79c}.terminal-gold{color:#e6b878}.terminal-cyan{color:#6be0ff}.terminal-green{color:#75e8aa}.terminal-meta{display:flex;flex-direction:column;justify-content:center;gap:14px;padding-left:18px;border-left:1px solid rgba(109,142,207,.22);color:#a9b7d2;font-size:12px}.model-constellation{position:relative;padding:36px max(36px,calc((100% - 1380px)/2)) 48px;border-top:1px solid rgba(72,171,255,.45);background:rgba(2,7,21,.92)}.constellation-heading{display:flex;align-items:end;justify-content:space-between;gap:36px}.constellation-heading .section-kicker{margin-bottom:9px;color:#6f8fd5;font-size:10px}.constellation-heading h2{margin:0;font-size:26px;letter-spacing:-.025em}.constellation-heading>p{max-width:540px;margin:0;color:#8e9bbb;font-size:13px;text-align:right}.constellation-heading>p span{display:block;margin-top:6px;color:#68dfff}.constellation-track{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-top:34px;padding:28px 18px 0;border-top:1px solid rgba(91,122,190,.24)}.constellation-model{position:relative;z-index:2;display:flex;min-width:92px;max-width:150px;flex-direction:column;align-items:center;gap:11px;text-align:center}.constellation-model strong{width:100%;overflow:hidden;color:#eaf0ff;font-size:13px;font-weight:650;text-overflow:ellipsis;white-space:nowrap}.model-orb{display:grid;width:56px;height:56px;place-items:center;border:1px solid rgba(89,220,255,.65);border-radius:999px;color:#8de9ff;background:radial-gradient(circle at 35% 28%,rgba(74,223,255,.35),rgba(42,31,112,.72) 62%,rgba(4,12,35,.92));box-shadow:0 0 24px rgba(70,132,255,.34),inset 0 0 16px rgba(124,102,255,.2)}.model-orb img{width:28px;height:28px}.constellation-model--discovered{min-width:74px}.model-orb--small{width:40px;height:40px}.future-orbs{display:flex;align-items:center;gap:26px;align-self:center;margin-top:4px}.future-orbs i{width:15px;height:15px;border:1px solid rgba(105,204,255,.6);border-radius:999px;background:#3e6cff;box-shadow:0 0 18px #426aff}.future-orbs i:nth-child(2){background:#744fff;box-shadow:0 0 18px #754dff}.future-orbs i:nth-child(3){background:#29b7e8;box-shadow:0 0 18px #29b7e8}.model-orb--more{width:58px;height:58px;border-color:rgba(147,106,255,.8)}.constellation-model--more{min-width:142px}.constellation-model--more strong{color:#bdc8e6}.cosmic-footer{position:relative;z-index:10;display:flex;justify-content:space-between;gap:20px;padding:20px max(36px,calc((100% - 1380px)/2));border-top:1px solid rgba(89,119,184,.18);color:#657392;background:#01050f;font-size:11px;letter-spacing:.04em}
@media(max-width:1120px){.hero-shell{grid-template-columns:1fr;min-height:auto;padding-top:82px}.hero-copy{max-width:760px}.hero-visual{min-height:560px}.floating-model--gpt{left:18%}.terminal-container{right:4%}.constellation-track{overflow-x:auto;justify-content:flex-start;padding-bottom:14px}.constellation-model{flex:0 0 116px}.future-orbs{flex:0 0 140px;justify-content:center}}
@media(max-width:720px){.cosmic-nav{width:calc(100% - 30px);min-height:80px}.brand-lockup{gap:9px;font-size:19px}.brand-mark{width:40px;height:40px;border-radius:11px}.header-actions{gap:7px}.header-divider{display:none}.theme-button{min-width:40px;min-height:40px}.login-button,.register-button{min-height:40px;padding:0 12px;font-size:12px}.hero-shell{width:calc(100% - 30px);gap:20px;padding-top:64px}.hero-kicker{font-size:9px;letter-spacing:.28em}.hero-title{font-size:clamp(41px,12vw,58px)}.hero-description{margin-top:24px;font-size:15px;line-height:1.75}.hero-cta{min-height:54px;margin-top:30px;padding:0 24px}.benefit-row{flex-direction:column;gap:18px;margin-top:44px}.benefit-item,.benefit-item:first-child{padding:0;border:0}.hero-visual{min-height:470px}.floating-model{width:68px;min-height:68px;border-radius:14px}.floating-model img{width:23px;height:23px}.floating-model--gpt{top:24px;left:4%}.floating-model--claude{top:8px;right:4%}.floating-model--gemini{top:112px;right:0}.floating-model--grok{top:130px;left:0}.terminal-container{right:0;bottom:24px;width:100%}.terminal-content{grid-template-columns:1fr;padding:16px}.terminal-content pre{font-size:9px}.terminal-meta{flex-direction:row;justify-content:flex-start;padding:13px 0 0;border-top:1px solid rgba(109,142,207,.22);border-left:0}.model-constellation{padding:32px 15px 38px}.constellation-heading{align-items:flex-start;flex-direction:column;gap:12px}.constellation-heading>p{text-align:left}.constellation-track{margin-top:24px;padding-inline:4px}.cosmic-footer{flex-direction:column;padding:20px 15px}}
@media(prefers-reduced-motion:reduce){.terminal-window,.hero-cta,.login-button,.register-button{transition:none}}
.hero-shell{grid-template-columns:minmax(540px,.92fr) minmax(0,1.08fr);min-height:520px;align-items:start;padding-top:14px;padding-bottom:24px}
.hero-copy{display:flex;height:500px;flex-direction:column}.hero-cta{align-self:flex-start}.benefit-row{margin-top:auto}.hero-visual{min-height:522px}
.hero-title{font-size:clamp(40px,3.35vw,56px);white-space:nowrap}
.floating-model--gpt{top:20px;left:35%}.floating-model--claude{top:25px;right:14%}.floating-model--gemini{top:145px;right:7%}.floating-model--grok{top:175px;left:28%}.floating-model--more-family{top:105px;left:54%}
@media(max-width:1120px){.hero-shell{grid-template-columns:1fr}}
.cosmic-backdrop{height:min(980px,94vh)}
.download-dock{display:grid;grid-template-columns:minmax(230px,1fr) auto;align-items:center;gap:18px;width:min(720px,calc(100% - 32px));min-height:82px;margin:8px 0 0 16px;padding:16px 18px;border:1px solid rgba(91,205,255,.36);border-radius:18px;background:linear-gradient(110deg,rgba(4,16,45,.78),rgba(11,19,60,.6));box-shadow:0 18px 54px rgba(0,0,0,.24),inset 0 1px 0 rgba(255,255,255,.12);backdrop-filter:blur(18px)}
.download-intro{display:flex;min-width:0;align-items:center;gap:12px}.download-symbol{display:grid;width:44px;height:44px;flex:0 0 44px;place-items:center;border:1px solid rgba(91,220,255,.48);border-radius:13px;color:#7ce8ff;background:radial-gradient(circle at 30% 20%,rgba(89,217,255,.25),rgba(73,64,199,.24))}.download-eyebrow{margin:0 0 2px;color:#82b7ff;font-size:8px;font-weight:700;letter-spacing:.22em}.download-intro h2{margin:0;color:#f7faff;font-size:17px;letter-spacing:-.02em}.download-platforms{display:flex;gap:6px}.download-platform{display:inline-flex;min-height:38px;align-items:center;gap:6px;padding:0 10px;border:1px solid rgba(105,137,200,.28);border-radius:11px;color:#92a2c3;background:rgba(2,9,28,.54);font-size:11px;font-weight:650;cursor:pointer;transition:160ms ease}.download-platform:hover{border-color:rgba(83,205,255,.5);color:#e8f8ff}.download-platform--active{border-color:rgba(80,224,255,.74);color:#f4fcff;background:linear-gradient(120deg,rgba(30,145,218,.3),rgba(92,70,215,.28));box-shadow:0 0 24px rgba(50,130,255,.16)}
.download-builds{display:grid;grid-column:1/-1;grid-template-columns:112px minmax(0,1fr);align-items:center;gap:12px;padding-top:12px;border-top:1px solid rgba(91,130,194,.2)}.download-builds-title{color:#91a4c9;font-size:10px;font-weight:650}.download-build-list{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px}.download-build-card{display:flex;min-width:0;align-items:center;gap:8px;padding:7px 8px;border:1px solid rgba(92,130,197,.24);border-radius:11px;background:rgba(2,9,28,.44)}.download-build-icon{display:grid;width:28px;height:28px;flex:0 0 28px;place-items:center;border-radius:8px;color:#7fdff7;background:rgba(48,125,214,.15)}.download-build-copy{display:flex;min-width:0;flex:1;flex-direction:column}.download-build-copy strong{overflow:hidden;color:#e8f1ff;font-size:11px;text-overflow:ellipsis;white-space:nowrap}.download-build-copy small{margin-top:1px;color:#7182a7;font:9px/1.3 ui-monospace,SFMono-Regular,Menlo,monospace}.download-build-action{display:inline-flex;min-height:28px;align-items:center;gap:4px;padding:0 8px;border:1px solid rgba(78,199,255,.36);border-radius:8px;color:#8ddff2;background:rgba(26,98,177,.18);font-size:10px;font-weight:650}.download-build-action:disabled{color:#667593;border-color:rgba(104,128,178,.2);background:rgba(6,14,33,.5);cursor:not-allowed}.floating-model--more-family{top:176px;left:49%;width:104px;min-height:66px;color:#b8eaff;border-color:rgba(143,107,255,.66);background:linear-gradient(145deg,rgba(14,30,73,.8),rgba(55,29,112,.7))}.floating-model--more-family svg{color:#a99bff}
@media(max-width:1120px){.hero-shell{padding-top:32px}}
@media(min-width:721px) and (max-width:1120px){.hero-shell{position:relative;display:block;min-height:450px;padding-top:12px;padding-bottom:20px}.hero-copy{position:relative;z-index:7;height:430px;max-width:49%}.hero-title{font-size:clamp(34px,4.3vw,42px)}.hero-description{max-width:96%;font-size:14px}.benefit-row{margin-top:auto}.benefit-item{gap:8px;padding:0 10px}.benefit-item strong{font-size:12px}.benefit-item small{font-size:9px}.hero-visual{position:absolute;top:0;right:0;width:49%;min-height:442px}.floating-model{width:72px;min-height:72px;border-radius:15px}.floating-model img{width:24px;height:24px}.floating-model--gpt{top:10px;left:38%}.floating-model--claude{top:6px;right:8%}.floating-model--gemini{top:100px;right:4%}.floating-model--grok{top:120px;left:34%}.floating-model--more-family{top:70px;left:48%;width:90px;min-height:58px}.terminal-container{right:0;bottom:12px;width:100%}.terminal-content{grid-template-columns:minmax(0,1fr) 96px;padding:16px}.terminal-content pre{font-size:9px}.terminal-meta{padding-left:12px;font-size:10px}}
@media(max-width:720px){.download-dock{grid-template-columns:1fr;width:calc(100% - 30px);margin:8px 15px 0;padding:17px;gap:16px}.download-symbol{width:44px;height:44px;flex-basis:44px}.download-platforms{display:grid;grid-template-columns:repeat(3,1fr);gap:7px}.download-platform{justify-content:center;padding:0 8px}.download-builds{grid-template-columns:1fr;gap:10px}.download-build-list{grid-template-columns:1fr}.hero-shell{padding-top:30px}.hero-copy{display:block;height:auto}.benefit-row{margin-top:44px}.hero-visual{min-height:520px}.floating-model--more-family{top:175px;left:50%;width:96px;min-height:56px;transform:translateX(-50%)}}
@media(max-width:720px){.hero-title{font-size:clamp(29px,9.2vw,38px)}}
@media(min-width:721px){
  .cosmic-home{display:grid;height:100svh;min-height:620px;grid-template-rows:76px minmax(0,1fr) 34px}
  .cosmic-backdrop{height:100%}
  .cosmic-nav{width:min(1380px,calc(100% - 40px));min-height:76px}
  .brand-lockup{gap:11px;font-size:22px}.brand-mark{width:42px;height:42px;border-radius:12px}
  .theme-button,.login-button,.register-button{min-height:38px}.login-button{padding:0 17px}.register-button{padding:0 19px}
  .cosmic-main{display:grid;min-height:0;grid-template-rows:auto minmax(0,1fr)}
  .download-dock{width:min(660px,calc(100% - 32px));min-height:60px;margin-top:4px;padding:10px 12px;gap:9px 12px;border-radius:16px}
  .download-symbol{width:38px;height:38px;flex-basis:38px;border-radius:11px}
  .download-intro{gap:10px}.download-intro h2{font-size:16px}.download-platform{min-height:34px;padding:0 9px}
  .download-builds{grid-template-columns:96px minmax(0,1fr);gap:8px;padding-top:8px}
  .download-build-card{padding:5px 7px}.download-build-icon{width:25px;height:25px;flex-basis:25px}.download-build-action{min-height:26px}
  .hero-shell{width:min(1380px,calc(100% - 40px));height:100%;min-height:0;padding-top:8px;padding-bottom:10px}
  .hero-copy{height:100%}.hero-kicker{margin-bottom:12px}.hero-description{margin-top:18px;line-height:1.65}.hero-cta{min-height:50px;margin-top:22px;padding:0 24px}
  .hero-visual{min-height:100%}.terminal-container{bottom:0}
  .cosmic-footer{align-items:center;min-height:34px;padding:0 max(20px,calc((100% - 1380px)/2));font-size:10px}
}
@media(min-width:721px) and (max-width:1120px){
  .hero-shell{min-height:0;padding-top:8px;padding-bottom:10px}
  .hero-copy{height:100%}.hero-visual{min-height:100%}
  .hero-description{margin-top:16px;line-height:1.6}.hero-cta{margin-top:18px}
  .floating-model--gpt{top:4px}.floating-model--claude{top:2px}.floating-model--gemini{top:91px}.floating-model--grok{top:111px}.floating-model--more-family{top:64px}
  .terminal-container{bottom:0}
}
@media(min-width:721px) and (max-height:639px){.cosmic-home{height:auto;min-height:640px;overflow:auto}}
.floating-model--more-family{width:84px;min-height:84px}
@media(min-width:721px){
  .download-dock{width:min(700px,calc(100% - 32px));padding:12px 14px;gap:11px 14px}
  .download-build-card{padding:7px 9px}
  .hero-copy{height:auto;align-self:center}
  .hero-description{margin-top:22px}.hero-cta{margin-top:26px}
  .benefit-row{gap:8px;margin-top:40px}
  .benefit-item,.benefit-item:first-child{flex:1;min-height:72px;align-items:center;padding:10px 11px;border:1px solid rgba(94,140,213,.2);border-radius:12px;background:rgba(3,13,37,.42)}
}
@media(min-width:1121px){
  .floating-model--gpt{top:12px;left:30%}.floating-model--claude{top:16px;right:12%}.floating-model--gemini{top:122px;right:5%}.floating-model--grok{top:128px;left:24%}.floating-model--more-family{top:82px;left:50%}
}
@media(min-width:721px) and (max-width:1120px){
  .hero-shell{display:grid;grid-template-columns:49% 49%;gap:2%;align-items:stretch}
  .hero-copy{position:static;max-width:none;transform:none}
  .hero-visual{position:relative;top:auto;right:auto;width:auto}
  .benefit-row{margin-top:36px}
  .floating-model--more-family{width:72px;min-height:72px}
  .terminal-container{bottom:62px}
}
@media(max-width:720px){.floating-model--more-family{width:68px;min-height:68px}}
</style>
