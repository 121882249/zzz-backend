<template>
  <main class="flex min-h-screen items-center justify-center bg-slate-950 px-6 text-white">
    <section class="w-full max-w-md rounded-3xl border border-indigo-300/20 bg-indigo-950/40 p-8 text-center shadow-2xl backdrop-blur-xl">
      <div v-if="processing">
        <div class="mx-auto h-9 w-9 animate-spin rounded-full border-2 border-indigo-300 border-t-transparent"></div>
        <h1 class="mt-5 text-xl font-semibold">正在连接 TokenPro 客户端</h1>
        <p class="mt-2 text-sm text-indigo-200/70">正在安全建立网页登录状态…</p>
      </div>
      <div v-else>
        <h1 class="text-xl font-semibold">无法完成客户端登录</h1>
        <p class="mt-3 text-sm text-rose-200">{{ errorMessage }}</p>
        <button class="btn btn-primary mt-6" type="button" @click="router.replace('/login')">前往登录</button>
      </div>
    </section>
  </main>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authAPI } from '@/api'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const processing = ref(true)
const errorMessage = ref('登录票据无效或已经过期，请回到 TokenPro 客户端重试。')

function safeRedirect(value: unknown): string {
  return typeof value === 'string' && value.startsWith('/') && !value.startsWith('//')
    ? value
    : '/dashboard'
}

onMounted(async () => {
  const fragment = new URLSearchParams(window.location.hash.slice(1))
  const ticket = fragment.get('ticket')?.trim() ?? ''
  window.history.replaceState({}, '', window.location.pathname + window.location.search)
  if (!ticket) {
    processing.value = false
    return
  }

  try {
    const response = await authAPI.exchangeDesktopTicket(ticket)
    authStore.acceptAuthResponse(response)
    await router.replace(safeRedirect(route.query.redirect))
  } catch (error) {
    const message = (error as { message?: string }).message
    if (message) errorMessage.value = message
    processing.value = false
  }
})
</script>
