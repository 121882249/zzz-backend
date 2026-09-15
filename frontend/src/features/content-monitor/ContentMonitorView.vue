<template>
  <AppLayout>
  <div class="space-y-6">
    <section class="card p-5 sm:p-6">
      <div class="max-w-3xl">
        <h2 class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.contentMonitor.account') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('admin.contentMonitor.accountHint') }}</p>
        <textarea
          v-model="emailText"
          rows="3"
          class="input mt-4 w-full font-mono text-sm"
          :placeholder="t('admin.contentMonitor.placeholder')"
          :aria-label="t('admin.contentMonitor.account')"
        />
        <div class="mt-4 flex items-center gap-3">
          <button class="btn btn-primary" :disabled="saving" @click="saveConfig">
            {{ saving ? t('admin.contentMonitor.saving') : t('admin.contentMonitor.save') }}
          </button>
          <span v-if="message" class="text-sm text-emerald-600 dark:text-emerald-400">{{ message }}</span>
          <span v-if="error" class="text-sm text-red-600 dark:text-red-400">{{ error }}</span>
        </div>
      </div>
    </section>

    <section class="card overflow-hidden">
      <div class="flex items-center justify-between border-b border-gray-100 px-5 py-4 dark:border-dark-800 sm:px-6">
        <h2 class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.contentMonitor.records') }}</h2>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="loading || deleting" @click="loadEvents">{{ t('admin.contentMonitor.refresh') }}</button>
          <button class="btn btn-danger" :disabled="loading || deleting || total === 0" @click="deleteRecords">
            {{ deleting ? t('admin.contentMonitor.deleting') : t('admin.contentMonitor.deleteRecords') }}
          </button>
        </div>
      </div>
      <div class="overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-100 dark:divide-dark-800">
          <thead class="bg-gray-50/80 dark:bg-dark-900/50">
            <tr>
              <th class="whitespace-nowrap px-5 py-3 text-left text-xs font-medium text-gray-500 sm:px-6">{{ t('admin.contentMonitor.userAccount') }}</th>
              <th class="w-[50%] px-5 py-3 text-left text-xs font-medium text-gray-500 sm:px-6">{{ t('admin.contentMonitor.content') }}</th>
              <th class="px-5 py-3 text-left text-xs font-medium text-gray-500 sm:px-6">{{ t('admin.contentMonitor.time') }}</th>
              <th class="px-5 py-3 text-left text-xs font-medium text-gray-500 sm:px-6">{{ t('admin.contentMonitor.model') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
            <tr v-for="item in items" :key="item.id" class="align-top">
              <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-800 dark:text-dark-100 sm:px-6">{{ item.user_email || '—' }}</td>
              <td class="px-5 py-4 sm:px-6"><pre class="max-h-48 overflow-auto whitespace-pre-wrap break-words font-sans text-sm text-gray-800 dark:text-dark-100">{{ item.content || '—' }}</pre></td>
              <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-600 dark:text-dark-300 sm:px-6">{{ formatTime(item.created_at) }}</td>
              <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-800 dark:text-dark-100 sm:px-6">{{ item.model || '—' }}</td>
            </tr>
            <tr v-if="!loading && items.length === 0"><td colspan="4" class="px-6 py-14 text-center text-sm text-gray-500">{{ t('admin.contentMonitor.empty') }}</td></tr>
            <tr v-if="loading"><td colspan="4" class="px-6 py-14 text-center text-sm text-gray-500">…</td></tr>
          </tbody>
        </table>
      </div>
      <div v-if="total > 0" class="flex items-center justify-between border-t border-gray-100 px-5 py-4 text-sm dark:border-dark-800 sm:px-6">
        <span class="text-gray-500">{{ t('admin.contentMonitor.pageInfo', { page, pages: Math.max(pages, 1), total }) }}</span>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="page <= 1 || loading" @click="changePage(page - 1)">{{ t('admin.contentMonitor.previous') }}</button>
          <button class="btn btn-secondary" :disabled="page >= pages || loading" @click="changePage(page + 1)">{{ t('admin.contentMonitor.next') }}</button>
        </div>
      </div>
    </section>
  </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { deleteContentMonitorEvents, getContentMonitorConfig, listContentMonitorEvents, saveContentMonitorConfig, type ContentMonitorItem } from './api'

const { t, locale } = useI18n()
const emailText = ref('')
const items = ref<ContentMonitorItem[]>([])
const loading = ref(false)
const saving = ref(false)
const deleting = ref(false)
const message = ref('')
const error = ref('')
const page = ref(1)
const pages = ref(0)
const total = ref(0)
const pageSize = 20

function normalizedEmails(): string[] {
  return [...new Set(emailText.value.split(/[\n,;]+/).map((value) => value.trim().toLowerCase()).filter(Boolean))].sort()
}

async function loadConfig() {
  const config = await getContentMonitorConfig()
  emailText.value = (config.emails ?? []).join('\n')
}

async function loadEvents() {
  loading.value = true
  error.value = ''
  try {
    const result = await listContentMonitorEvents(page.value, pageSize)
    items.value = result.items ?? []
    total.value = result.total
    pages.value = result.pages
  } catch {
    error.value = t('admin.contentMonitor.loadFailed')
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  saving.value = true
  message.value = ''
  error.value = ''
  try {
    const config = await saveContentMonitorConfig(normalizedEmails())
    emailText.value = (config.emails ?? []).join('\n')
    message.value = t('admin.contentMonitor.saved')
    page.value = 1
    await loadEvents()
  } catch {
    error.value = t('admin.contentMonitor.saveFailed')
  } finally {
    saving.value = false
  }
}

async function deleteRecords() {
  if (!window.confirm(t('admin.contentMonitor.deleteConfirm'))) return
  deleting.value = true
  message.value = ''
  error.value = ''
  try {
    const result = await deleteContentMonitorEvents()
    message.value = t('admin.contentMonitor.deleted', { count: result.deleted_events })
    page.value = 1
    await loadEvents()
  } catch {
    error.value = t('admin.contentMonitor.deleteFailed')
  } finally {
    deleting.value = false
  }
}

function changePage(value: number) {
  page.value = value
  void loadEvents()
}

function formatTime(value: string): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat(locale.value.startsWith('zh') ? 'zh-CN' : 'en-US', { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(value))
}

onMounted(async () => {
  try {
    await loadConfig()
  } catch {
    error.value = t('admin.contentMonitor.loadFailed')
  }
  await loadEvents()
})
</script>
