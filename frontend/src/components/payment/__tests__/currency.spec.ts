import { describe, expect, it } from 'vitest'
import { currencySymbol, formatPaymentAmount, formatPaymentAmountCode } from '../currency'

describe('formatPaymentAmount', () => {
  it('uses the currency default fraction digits', () => {
    expect(formatPaymentAmount(100, 'JPY', 'en-US')).not.toContain('.00')
    expect(formatPaymentAmount(100, 'KRW', 'en-US')).not.toContain('.00')
    expect(formatPaymentAmount(100, 'HKD', 'en-US')).toContain('.00')
  })

  it('uses the HKD code without a dollar symbol', () => {
    const formatted = formatPaymentAmount(2.35, 'HKD', 'zh-CN')
    expect(formatted).toContain('HKD')
    expect(formatted).not.toContain('$')
  })
})

describe('currencySymbol', () => {
  it('maps common payment currencies and falls back safely', () => {
    expect(currencySymbol('USD')).toBe('$')
    expect(currencySymbol('cny')).toBe('¥')
    expect(currencySymbol('HKD')).toBe('HKD ')
    expect(currencySymbol('EUR')).toBe('€')
    expect(currencySymbol('')).toBe('¥')
    expect(currencySymbol('XYZ')).toBe('XYZ')
  })
})

describe('formatPaymentAmountCode', () => {
  it('uses ISO currency codes for confirmation amounts', () => {
    expect(formatPaymentAmountCode(0, 'CNY', 'zh-CN')).toContain('CNY')
    expect(formatPaymentAmountCode(0, 'CNY', 'zh-CN')).not.toContain('¥')
    expect(formatPaymentAmountCode(12.6, 'HKD', 'zh-CN')).toContain('HKD')
  })
})
