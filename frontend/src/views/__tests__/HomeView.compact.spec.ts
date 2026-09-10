import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'

import HomeView from '../HomeView.vue'

const { appStore, authStore } = vi.hoisted(() => ({
  appStore: {
    cachedPublicSettings: {} as Record<string, unknown>,
    siteName: 'Fallback site',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  },
  authStore: {
    isAuthenticated: false,
    isAdmin: false,
    user: null as { email?: string } | null,
    checkAuth: vi.fn(),
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function mountHome(settings: Record<string, unknown> = {}) {
  appStore.cachedPublicSettings = {
    site_name: 'Test site',
    site_subtitle: 'Test subtitle',
    ...settings,
  }

  return mount(HomeView, {
    global: {
      stubs: {
        RouterLink: RouterLinkStub,
        LocaleSwitcher: { template: '<div data-testid="locale-switcher" />' },
        Icon: { template: '<span data-testid="icon" />' },
      },
    },
  })
}

function compactDestination(wrapper: ReturnType<typeof mountHome>) {
  return linkDestination(wrapper, 'compact-primary-action')
}

function linkDestination(wrapper: ReturnType<typeof mountHome>, testId: string) {
  return wrapper
    .findAllComponents(RouterLinkStub)
    .find((link) => link.attributes('data-testid') === testId)
    ?.props('to')
}

describe('HomeView compact mode', () => {
  beforeEach(() => {
    authStore.isAuthenticated = false
    authStore.isAdmin = false
    authStore.user = null
    authStore.checkAuth.mockClear()
    appStore.fetchPublicSettings.mockClear()
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ tag_name: 'v1.2.11' }),
    }))
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList)
  })

  it('renders custom HTML ahead of compact mode', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      home_content: '<section id="custom-home">Custom home</section>',
    })

    expect(wrapper.get('#custom-home').text()).toBe('Custom home')
    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
  })

  it('renders custom URL content ahead of compact mode', () => {
    const wrapper = mountHome({
      compact_home_enabled: true,
      home_content: ' https://example.com/home ',
    })

    expect(wrapper.get('iframe').attributes('src')).toBe('https://example.com/home')
    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
  })

  it('treats whitespace-only custom content as empty and selects compact mode', () => {
    const wrapper = mountHome({ compact_home_enabled: true, home_content: ' \n\t ' })

    expect(wrapper.get('[data-testid="compact-home"]').text()).toContain('TokenPro')
  })

  it.each([undefined, false])('selects the default home when compact mode is %s', (enabled) => {
    const settings = enabled === undefined ? {} : { compact_home_enabled: enabled }
    const wrapper = mountHome(settings)

    expect(wrapper.find('[data-testid="compact-home"]').exists()).toBe(false)
    expect(wrapper.find('.terminal-container').exists()).toBe(true)
  })

  it('links unauthenticated visitors to registration from the primary action', () => {
    expect(compactDestination(mountHome({ compact_home_enabled: true }))).toBe('/register')
  })

  it('links authenticated users to their dashboard', () => {
    authStore.isAuthenticated = true

    expect(compactDestination(mountHome({ compact_home_enabled: true }))).toBe('/dashboard')
  })

  it('links administrators to the admin dashboard', () => {
    authStore.isAuthenticated = true
    authStore.isAdmin = true

    const wrapper = mountHome({ compact_home_enabled: true })
    expect(compactDestination(wrapper)).toBe('/admin/dashboard')
    expect(authStore.checkAuth).toHaveBeenCalledOnce()
    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
  })

  it('keeps product, pricing, docs, community and model-plaza links out of the header', () => {
    const wrapper = mountHome({
      model_plaza_enabled: true,
      model_plaza_require_auth: false,
      doc_url: 'https://docs.example.com',
    })

    const destinations = wrapper.findAllComponents(RouterLinkStub).map((link) => link.props('to'))
    expect(destinations).not.toContain('/model-plaza')
    expect(wrapper.find('a[href="https://docs.example.com"]').exists()).toBe(false)
  })

  it('shows login and registration together for anonymous visitors', () => {
    const wrapper = mountHome()
    expect(linkDestination(wrapper, 'header-login')).toBe('/login')
    expect(linkDestination(wrapper, 'header-register')).toBe('/register')
    expect(linkDestination(wrapper, 'hero-primary-action')).toBe('/register')
  })

  it('links supported desktop builds from the device-aware download dock', async () => {
    const wrapper = mountHome()

    expect(wrapper.get('[data-testid="download-dock"]').exists()).toBe(true)
    expect(wrapper.findAll('.download-platform')).toHaveLength(3)
    await wrapper.get('[data-testid="download-platform-macos"]').trigger('click')
    expect(wrapper.get('[data-testid="download-builds"]').exists()).toBe(true)
    expect(wrapper.findAll('.download-build-card')).toHaveLength(2)
    expect(wrapper.findAll('.download-build-action')).toHaveLength(2)
    const downloads = wrapper.findAll('a.download-build-action')
    expect(downloads).toHaveLength(2)
    expect(downloads[0].attributes('href')).toBe('/downloads/latest/TokenPro-macOS-arm64.dmg')
    expect(downloads[1].attributes('href')).toBe('/downloads/latest/TokenPro-macOS-x64.dmg')
    expect(wrapper.get('.download-version').text()).toBe('v1.2.11')
    expect(wrapper.find('.download-build-copy small').exists()).toBe(false)
  })

  it('switches the visible build slots with the selected platform', async () => {
    const wrapper = mountHome()

    await wrapper.get('[data-testid="download-platform-windows"]').trigger('click')

    expect(wrapper.get('[data-testid="download-platform-windows"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="download-builds"]').text()).toContain('home.cosmic.buildX64')
    expect(wrapper.get('[data-testid="download-builds"]').text()).not.toContain('home.cosmic.buildArm64')
    expect(wrapper.find('a[href="/downloads/latest/TokenPro-Windows-x64.exe"]').exists()).toBe(true)
    expect(wrapper.findAll('.download-build-card')).toHaveLength(1)
    expect(wrapper.get('.download-build-list').classes()).toContain('download-build-list--single')
    expect(wrapper.findAll('button.download-build-action')).toHaveLength(0)
  })

  it('shows an expandable model family beyond the four representative providers', () => {
    const wrapper = mountHome()

    expect(wrapper.get('[data-testid="hero-more-models"]').text()).toContain('home.cosmic.moreModelFamily')
    expect(wrapper.findAll('.floating-model')).toHaveLength(5)
  })

  it('keeps the model family inside the hero instead of repeating a second model section', () => {
    const wrapper = mountHome()

    expect(wrapper.find('.model-constellation').exists()).toBe(false)
    expect(wrapper.findAll('.floating-model')).toHaveLength(5)
  })
})
