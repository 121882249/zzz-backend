import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'

import HomeView from '../HomeView.vue'

const { appStore, authStore, getModelPlaza } = vi.hoisted(() => ({
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
  getModelPlaza: vi.fn(),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/api/modelPlaza', () => ({ getModelPlaza }))

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
    getModelPlaza.mockReset()
    getModelPlaza.mockResolvedValue({ description: '', groups: [] })
    localStorage.clear()
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

  it('deduplicates the live model catalog and exposes future model families', async () => {
    getModelPlaza.mockResolvedValue({
      description: '',
      groups: [
        { models: [{ name: 'gpt-5.6-sol' }, { name: 'DeepSeek V4' }, { name: 'Mistral Large' }] },
        { models: [{ name: 'deepseek v4' }, { name: 'Qwen 4' }] },
      ],
    })

    const wrapper = mountHome({ model_plaza_enabled: true })
    await flushPromises()

    expect(getModelPlaza).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('DeepSeek V4')
    expect(wrapper.text()).toContain('Mistral Large')
    expect(wrapper.text()).toContain('Qwen 4')
    expect(wrapper.findAll('.constellation-model--discovered')).toHaveLength(3)
  })

  it('does not request an authenticated-only model catalog for anonymous visitors', async () => {
    mountHome({ model_plaza_enabled: true, model_plaza_require_auth: true })
    await flushPromises()

    expect(getModelPlaza).not.toHaveBeenCalled()
  })
})
