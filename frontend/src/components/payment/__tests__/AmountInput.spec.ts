import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
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

enableAutoUnmount(afterEach)

function mountInput(value: number | null = null) {
  return mount(AmountInput, { props: { modelValue: value } })
}

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

describe('recharge amount input', () => {
  it.each(['10abc', '10.555', '-10', '1e2'])('restores the accepted amount after rejecting %s', async (value) => {
    const wrapper = mountInput(10)
    const input = wrapper.get('input')
    await input.setValue(value)
    expect((input.element as HTMLInputElement).value).toBe('10')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('restores the last typed amount rather than a stale prop', async () => {
    const wrapper = mountInput()
    const input = wrapper.get('input')
    await input.setValue('12.50')
    await input.setValue('12.500')
    expect((input.element as HTMLInputElement).value).toBe('12.50')
    expect(wrapper.emitted('update:modelValue')).toEqual([[12.5]])
  })

  it('preserves decimal editing and allows clearing the amount', async () => {
    const wrapper = mountInput()
    const input = wrapper.get('input')
    for (const value of ['0', '0.', '0.5', '0.50', '']) await input.setValue(value)
    expect(wrapper.emitted('update:modelValue')).toEqual([[null], [null], [0.5], [0.5], [null]])
    expect((input.element as HTMLInputElement).value).toBe('')
  })
})
