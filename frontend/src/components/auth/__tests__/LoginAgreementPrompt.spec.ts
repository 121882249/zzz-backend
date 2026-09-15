import { describe, expect, it, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'

import LoginAgreementPrompt from '../LoginAgreementPrompt.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const documents = [
  {
    id: 'api-use-compliance',
    title: 'API 使用与合规声明',
    content: 'Compliance content',
    updated_at: '2026-08-04',
  },
]

function mountPrompt(accepted: boolean) {
  return mount(LoginAgreementPrompt, {
    props: {
      accepted,
      documents,
      mode: 'modal',
      visible: false,
    },
    global: {
      stubs: {
        RouterLink: RouterLinkStub,
        Icon: { template: '<span data-testid="icon" />' },
        Teleport: true,
      },
    },
  })
}

describe('LoginAgreementPrompt', () => {
  it('keeps compliance document links visible after the agreement is accepted', () => {
    const wrapper = mountPrompt(true)
    const link = wrapper.getComponent(RouterLinkStub)

    expect(wrapper.get('[data-testid="login-agreement-links"]').text()).toContain('API 使用与合规声明')
    expect(link.props('to')).toEqual({
      name: 'LegalDocument',
      params: { documentId: 'api-use-compliance' },
    })
    expect(link.attributes('target')).toBe('_blank')
  })

  it('still shows the required notice before acceptance', () => {
    const wrapper = mountPrompt(false)

    expect(wrapper.text()).toContain('legal.loginAgreementPrompt.noticeTitle')
    expect(wrapper.find('[data-testid="login-agreement-links"]').exists()).toBe(false)
  })
})
