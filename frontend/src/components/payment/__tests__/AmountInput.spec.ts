import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AmountInput from '../AmountInput.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'payment.customAmount': '自定义金额',
    'payment.currencyNames.CNY': '人民币',
    'payment.currencyNames.HKD': '港币',
  }
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => messages[key] ?? key }),
  }
})

describe('AmountInput', () => {
  it('uses the compact quick amounts by default', () => {
    const wrapper = mount(AmountInput, { props: { modelValue: null } })

    expect(wrapper.findAll('button').map(button => button.text())).toEqual(['10', '50', '100'])
  })

  it('shows a colored currency code and matching custom amount label', () => {
    const hkd = mount(AmountInput, { props: { modelValue: null, currency: 'HKD' } })
    const cny = mount(AmountInput, { props: { modelValue: null, currency: 'CNY' } })

    expect(hkd.text()).toContain('HKD')
    expect(hkd.text()).not.toContain('$')
    expect(hkd.text()).toContain('自定义金额「港币」')
    expect(hkd.get('.text-orange-500').text()).toBe('HKD')
    expect(cny.text()).toContain('自定义金额「人民币」')
    expect(cny.get('.text-red-500').text()).toBe('CNY')
    expect(cny.text()).not.toContain('¥')
  })
})
