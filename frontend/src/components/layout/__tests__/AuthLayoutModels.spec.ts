import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import AuthLayout from '../AuthLayout.vue'

vi.mock('@/stores', () => ({
  useAppStore: () => ({ fetchPublicSettings: vi.fn(), publicSettingsLoaded: true })
}))

describe('cosmic login model badges', () => {
  it('shows all five badges and updates More models with the language', async () => {
    const i18n = createI18n({ legacy: false, locale: 'zh', missingWarn: false, fallbackWarn: false,
      messages: {
        zh: { auth: { cosmic: { moreModels: () => '更多模型' } } },
        en: { auth: { cosmic: { moreModels: () => 'More models' } } }
      } })
    const wrapper = mount(AuthLayout, { props: { variant: 'cosmic' }, global: {
      plugins: [i18n], stubs: { RouterLink: { template: '<a><slot /></a>' } }
    } })
    expect(wrapper.findAll('.auth-cosmic-models span').map(x => x.text())).toEqual(['GPT', 'Claude', 'Gemini', 'Grok', '更多模型'])
    expect(wrapper.get('.auth-cosmic-models').attributes('aria-label')).toContain('更多模型')
    expect(wrapper.get('.auth-cosmic-models span:last-child img').attributes('src')).toBe('/brand/model-more.svg')
    i18n.global.locale.value = 'en'
    await nextTick()
    expect(wrapper.get('.auth-cosmic-models span:last-child').text()).toBe('More models')
    wrapper.unmount()
  })
})
