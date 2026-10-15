import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ContentMonitorView from '../ContentMonitorView.vue'

const api = vi.hoisted(() => ({
  getContentMonitorConfig: vi.fn(),
  listContentMonitorEvents: vi.fn(),
  saveContentMonitorConfig: vi.fn(),
  deleteContentMonitorEvents: vi.fn(),
}))
vi.mock('../api', () => api)
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh-CN' } }),
}))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  api.getContentMonitorConfig.mockResolvedValue({ emails: [] })
  api.listContentMonitorEvents.mockResolvedValue({
    items: [
      { id: 2, user_email: 'user@example.test', model: 'selected-model', content: '画一只猫\n保留换行', user_input_only: true, created_at: '2026-10-15T03:04:00Z' },
      { id: 1, user_email: 'user@example.test', model: 'selected-model', content: 'legacy system instructions and tool results', user_input_only: false, created_at: '2026-10-15T03:03:00Z' },
    ], total: 2, pages: 1,
  })
})

describe('Content Monitor input display', () => {
  it('shows current user input and hides unseparated historical context', async () => {
    const wrapper = mount(ContentMonitorView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    expect(wrapper.findAll('pre')).toHaveLength(1)
    expect(wrapper.get('pre').text()).toBe('画一只猫\n保留换行')
    expect(wrapper.text()).not.toContain('legacy system instructions')
    expect(wrapper.text()).toContain('admin.contentMonitor.legacyInputUnavailable')
    expect(wrapper.text()).toContain('selected-model')
    expect(api.listContentMonitorEvents).toHaveBeenCalledWith(1, 20)
  })
})
