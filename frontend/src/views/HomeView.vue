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
      <section class="hero-shell" aria-labelledby="home-hero-title">
        <div class="hero-copy">
          <p class="hero-kicker">{{ t('home.cosmic.kicker') }}</p>
          <h1 id="home-hero-title" class="hero-title">
            {{ t('home.cosmic.titleLineOne') }}<br />{{ t('home.cosmic.titleLineTwo') }} <span>{{ t('home.cosmic.titleHighlight') }}</span>
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

      <section class="model-constellation" aria-labelledby="model-constellation-title">
        <div class="constellation-heading">
          <div><p class="section-kicker">TOKENPRO MODEL NETWORK</p><h2 id="model-constellation-title">{{ t('home.cosmic.modelConstellation') }}</h2></div>
          <p>{{ t('home.cosmic.modelConstellationDescription') }}<span v-if="catalogCount > 0">{{ t('home.cosmic.connectedModels', { count: catalogCount }) }}</span></p>
        </div>
        <div class="constellation-track">
          <div v-for="model in representativeModels" :key="`rail-${model.key}`" class="constellation-model">
            <span class="model-orb"><img :src="model.icon" alt="" /></span><strong>{{ model.label }}</strong>
          </div>
          <div v-for="model in extraModels" :key="model" class="constellation-model constellation-model--discovered" :title="model">
            <span class="model-orb model-orb--small"><Icon name="sparkles" size="md" /></span><strong>{{ model }}</strong>
          </div>
          <div v-if="extraModels.length === 0" class="future-orbs" aria-hidden="true"><i></i><i></i><i></i></div>
          <div class="constellation-model constellation-model--more">
            <span class="model-orb model-orb--more"><Icon name="more" size="lg" /></span><strong>{{ t('home.cosmic.moreModels') }}</strong>
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
import { getModelPlaza } from '@/api/modelPlaza'
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
const discoveredModels = ref<string[]>([])
const catalogCount = computed(() => discoveredModels.value.length)
const representativePattern = /(^|[-_.\s])(gpt|openai|o[1-9]|claude|gemini|grok)([-_.\s]|$)/i
const extraModels = computed(() => discoveredModels.value.filter((name) => !representativePattern.test(name)).slice(0, 4))
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
async function loadPublicModelCatalog() {
  if (appStore.cachedPublicSettings?.model_plaza_enabled !== true) return
  if (appStore.cachedPublicSettings?.model_plaza_require_auth === true && !isAuthenticated.value) return
  try {
    const response = await getModelPlaza()
    const seen = new Set<string>()
    discoveredModels.value = response.groups.flatMap((group) => group.models.map((model) => model.name.trim())).filter((name) => {
      const normalized = name.toLocaleLowerCase()
      if (!name || seen.has(normalized)) return false
      seen.add(normalized)
      return true
    })
  } catch { discoveredModels.value = [] }
}
onMounted(async () => {
  initTheme()
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) await appStore.fetchPublicSettings()
  await loadPublicModelCatalog()
})
</script>

<style scoped>
.cosmic-home{--cyan:#4fe4ff;--violet:#7757ff;position:relative;min-height:100vh;overflow:hidden;color:#f7f9ff;background:#020612;color-scheme:dark}.cosmic-backdrop{position:absolute;inset:0 0 auto;height:min(830px,82vh);background-image:linear-gradient(90deg,rgba(2,6,18,.28),rgba(2,6,18,.08) 42%,rgba(2,6,18,.05)),linear-gradient(180deg,rgba(2,6,18,.08) 65%,#020612),url('/brand/orbital-gateway-bg.webp');background-position:center top;background-size:cover;pointer-events:none}.cosmic-header{position:relative;z-index:20}.cosmic-nav{display:flex;align-items:center;justify-content:space-between;width:min(1380px,calc(100% - 72px));min-height:104px;margin:auto}.brand-lockup{display:inline-flex;align-items:center;gap:14px;color:#fff;font-size:25px;font-weight:760;letter-spacing:-.035em;text-decoration:none}.brand-mark{width:48px;height:48px;border-radius:14px;object-fit:cover;box-shadow:0 0 0 1px rgba(122,174,255,.38),0 0 28px rgba(93,104,255,.35)}.header-actions{display:flex;align-items:center;gap:13px}.header-divider{width:1px;height:28px;background:rgba(150,175,232,.28)}.theme-button,.login-button,.register-button{display:inline-flex;min-width:46px;min-height:44px;align-items:center;justify-content:center;border-radius:12px;color:#eef4ff;transition:180ms ease}.theme-button{border:0;background:transparent;cursor:pointer}.theme-button:hover{background:rgba(79,228,255,.1)}.login-button{padding:0 20px;border:1px solid rgba(88,200,255,.72);background:rgba(5,16,42,.5);font-size:14px;font-weight:650}.register-button{padding:0 22px;border:1px solid rgba(99,214,255,.72);background:linear-gradient(110deg,rgba(50,172,255,.92),rgba(108,74,255,.94));box-shadow:0 10px 34px rgba(62,105,255,.3);font-size:14px;font-weight:700}.login-button:hover,.register-button:hover{transform:translateY(-1px);box-shadow:0 12px 34px rgba(50,169,255,.3)}.theme-button:focus-visible,.login-button:focus-visible,.register-button:focus-visible,.hero-cta:focus-visible,.brand-lockup:focus-visible{outline:2px solid var(--cyan);outline-offset:4px}.cosmic-main{position:relative;z-index:10}.hero-shell{display:grid;grid-template-columns:minmax(0,.88fr) minmax(520px,1.12fr);gap:40px;align-items:center;width:min(1380px,calc(100% - 72px));min-height:690px;margin:auto;padding:38px 0 50px}.hero-copy{max-width:650px}.hero-kicker,.section-kicker{margin:0 0 20px;color:#8fbaff;font-size:12px;font-weight:650;letter-spacing:.38em}.hero-title{margin:0;color:#fbfdff;font-size:clamp(48px,5.2vw,78px);font-weight:820;letter-spacing:-.055em;line-height:1.08;text-shadow:0 3px 28px rgba(49,98,221,.18)}.hero-title span{background:linear-gradient(100deg,#eafaff,#5be7ff 35%,#7485ff 68%,#ae72ff);-webkit-background-clip:text;background-clip:text;color:transparent}.hero-description{max-width:590px;margin:30px 0 0;color:#c3cce2;font-size:17px;line-height:1.85}.hero-cta{display:inline-flex;min-height:58px;align-items:center;gap:14px;margin-top:38px;padding:0 30px;border:1px solid rgba(92,224,255,.84);border-radius:13px;color:#fff;background:linear-gradient(110deg,#2eafff,#5864ff 48%,#7546ff);box-shadow:0 16px 50px rgba(45,111,255,.36),inset 0 1px 0 rgba(255,255,255,.24);font-size:17px;font-weight:720;transition:180ms ease}.hero-cta:hover{transform:translateY(-2px);box-shadow:0 20px 58px rgba(60,106,255,.48)}.benefit-row{display:flex;margin-top:58px}.benefit-item{display:flex;min-width:0;align-items:center;gap:13px;padding:0 27px;color:var(--cyan);border-left:1px solid rgba(118,151,218,.28)}.benefit-item:first-child{padding-left:0;border-left:0}.benefit-item span{display:flex;flex-direction:column;min-width:0}.benefit-item strong{color:#f4f7ff;font-size:14px;font-weight:700;white-space:nowrap}.benefit-item small{margin-top:5px;color:#8594b7;font-size:11px;white-space:nowrap}.hero-visual{position:relative;min-height:590px}.floating-model{position:absolute;z-index:4;display:flex;width:84px;min-height:84px;flex-direction:column;align-items:center;justify-content:center;gap:7px;border:1px solid rgba(95,213,255,.52);border-radius:18px;color:#fff;background:rgba(4,15,43,.72);box-shadow:0 15px 45px rgba(26,79,221,.28),inset 0 1px 0 rgba(255,255,255,.16);backdrop-filter:blur(14px)}.floating-model img{width:29px;height:29px;object-fit:contain}.floating-model span{font-size:12px;font-weight:680}.floating-model--gpt{top:65px;left:28%}.floating-model--claude{top:55px;right:4%}.floating-model--gemini{top:210px;right:-2%}.floating-model--grok{top:242px;left:18%}.terminal-container{position:absolute;z-index:5;right:0;bottom:22px;width:min(610px,92%)}.terminal-window{overflow:hidden;border:1px solid rgba(84,186,255,.5);border-radius:18px;background:rgba(2,10,30,.88);box-shadow:0 30px 80px rgba(0,0,0,.48),0 0 55px rgba(44,96,255,.18),inset 0 1px 0 rgba(255,255,255,.12);backdrop-filter:blur(18px);transform:perspective(1200px) rotateX(1deg) rotateY(-1.5deg);transition:transform 250ms ease}.terminal-window:hover{transform:none}.terminal-header{display:grid;grid-template-columns:1fr auto 1fr;align-items:center;min-height:48px;padding:0 18px;border-bottom:1px solid rgba(95,129,193,.18);color:#8eb6e8;font:11px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace}.terminal-header b{justify-self:end;color:#55efc4;font-weight:600}.terminal-lights{display:flex;gap:7px}.terminal-lights i{width:9px;height:9px;border-radius:999px;background:#ff6f67}.terminal-lights i:nth-child(2){background:#ffca58}.terminal-lights i:nth-child(3){background:#4bd98b}.terminal-content{display:grid;grid-template-columns:minmax(0,1fr) 128px;gap:18px;padding:20px 22px 24px}.terminal-content pre{min-width:0;overflow:hidden;margin:0;color:#b7c9e8;font:11px/1.72 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap}.terminal-muted{color:#67e79c}.terminal-gold{color:#e6b878}.terminal-cyan{color:#6be0ff}.terminal-green{color:#75e8aa}.terminal-meta{display:flex;flex-direction:column;justify-content:center;gap:14px;padding-left:18px;border-left:1px solid rgba(109,142,207,.22);color:#a9b7d2;font-size:12px}.model-constellation{position:relative;padding:36px max(36px,calc((100% - 1380px)/2)) 48px;border-top:1px solid rgba(72,171,255,.45);background:rgba(2,7,21,.92)}.constellation-heading{display:flex;align-items:end;justify-content:space-between;gap:36px}.constellation-heading .section-kicker{margin-bottom:9px;color:#6f8fd5;font-size:10px}.constellation-heading h2{margin:0;font-size:26px;letter-spacing:-.025em}.constellation-heading>p{max-width:540px;margin:0;color:#8e9bbb;font-size:13px;text-align:right}.constellation-heading>p span{display:block;margin-top:6px;color:#68dfff}.constellation-track{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-top:34px;padding:28px 18px 0;border-top:1px solid rgba(91,122,190,.24)}.constellation-model{position:relative;z-index:2;display:flex;min-width:92px;max-width:150px;flex-direction:column;align-items:center;gap:11px;text-align:center}.constellation-model strong{width:100%;overflow:hidden;color:#eaf0ff;font-size:13px;font-weight:650;text-overflow:ellipsis;white-space:nowrap}.model-orb{display:grid;width:56px;height:56px;place-items:center;border:1px solid rgba(89,220,255,.65);border-radius:999px;color:#8de9ff;background:radial-gradient(circle at 35% 28%,rgba(74,223,255,.35),rgba(42,31,112,.72) 62%,rgba(4,12,35,.92));box-shadow:0 0 24px rgba(70,132,255,.34),inset 0 0 16px rgba(124,102,255,.2)}.model-orb img{width:28px;height:28px}.constellation-model--discovered{min-width:74px}.model-orb--small{width:40px;height:40px}.future-orbs{display:flex;align-items:center;gap:26px;align-self:center;margin-top:4px}.future-orbs i{width:15px;height:15px;border:1px solid rgba(105,204,255,.6);border-radius:999px;background:#3e6cff;box-shadow:0 0 18px #426aff}.future-orbs i:nth-child(2){background:#744fff;box-shadow:0 0 18px #754dff}.future-orbs i:nth-child(3){background:#29b7e8;box-shadow:0 0 18px #29b7e8}.model-orb--more{width:58px;height:58px;border-color:rgba(147,106,255,.8)}.constellation-model--more{min-width:142px}.constellation-model--more strong{color:#bdc8e6}.cosmic-footer{position:relative;z-index:10;display:flex;justify-content:space-between;gap:20px;padding:20px max(36px,calc((100% - 1380px)/2));border-top:1px solid rgba(89,119,184,.18);color:#657392;background:#01050f;font-size:11px;letter-spacing:.04em}
@media(max-width:1120px){.hero-shell{grid-template-columns:1fr;min-height:auto;padding-top:82px}.hero-copy{max-width:760px}.hero-visual{min-height:560px}.floating-model--gpt{left:18%}.terminal-container{right:4%}.constellation-track{overflow-x:auto;justify-content:flex-start;padding-bottom:14px}.constellation-model{flex:0 0 116px}.future-orbs{flex:0 0 140px;justify-content:center}}
@media(max-width:720px){.cosmic-nav{width:calc(100% - 30px);min-height:80px}.brand-lockup{gap:9px;font-size:19px}.brand-mark{width:40px;height:40px;border-radius:11px}.header-actions{gap:7px}.header-divider{display:none}.theme-button{min-width:40px;min-height:40px}.login-button,.register-button{min-height:40px;padding:0 12px;font-size:12px}.hero-shell{width:calc(100% - 30px);gap:20px;padding-top:64px}.hero-kicker{font-size:9px;letter-spacing:.28em}.hero-title{font-size:clamp(41px,12vw,58px)}.hero-description{margin-top:24px;font-size:15px;line-height:1.75}.hero-cta{min-height:54px;margin-top:30px;padding:0 24px}.benefit-row{flex-direction:column;gap:18px;margin-top:44px}.benefit-item,.benefit-item:first-child{padding:0;border:0}.hero-visual{min-height:470px}.floating-model{width:68px;min-height:68px;border-radius:14px}.floating-model img{width:23px;height:23px}.floating-model--gpt{top:24px;left:4%}.floating-model--claude{top:8px;right:4%}.floating-model--gemini{top:112px;right:0}.floating-model--grok{top:130px;left:0}.terminal-container{right:0;bottom:24px;width:100%}.terminal-content{grid-template-columns:1fr;padding:16px}.terminal-content pre{font-size:9px}.terminal-meta{flex-direction:row;justify-content:flex-start;padding:13px 0 0;border-top:1px solid rgba(109,142,207,.22);border-left:0}.model-constellation{padding:32px 15px 38px}.constellation-heading{align-items:flex-start;flex-direction:column;gap:12px}.constellation-heading>p{text-align:left}.constellation-track{margin-top:24px;padding-inline:4px}.cosmic-footer{flex-direction:column;padding:20px 15px}}
@media(prefers-reduced-motion:reduce){.terminal-window,.hero-cta,.login-button,.register-button{transition:none}}
.hero-shell{grid-template-columns:minmax(540px,.92fr) minmax(0,1.08fr)}
.hero-title{font-size:clamp(48px,4.55vw,70px)}
@media(max-width:1120px){.hero-shell{grid-template-columns:1fr}}
</style>
