<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  guides,
  navItems,
  type GuideKey,
} from '@/content/agentGuides'

const route = useRoute()
const query = ref('')
const mobileOpen = ref(false)
const copiedValue = ref('')
let copyResetTimer: number | undefined

function resolveGuideKey(hash: string): GuideKey {
  const candidate = hash.replace(/^#/, '') as GuideKey
  return navItems.some((item) => item.key === candidate) ? candidate : 'codex-desktop'
}

const guideKey = computed(() => resolveGuideKey(route.hash))
const guide = computed(() => guides[guideKey.value])
const guideIndex = computed(() => navItems.findIndex((item) => item.key === guideKey.value))
const previous = computed(() => navItems[guideIndex.value - 1])
const next = computed(() => navItems[guideIndex.value + 1])
const filteredItems = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!needle) return navItems
  return navItems.filter((item) => item.label.toLowerCase().includes(needle))
})
async function copy(value: string) {
  await navigator.clipboard.writeText(value)
  copiedValue.value = value
  window.clearTimeout(copyResetTimer)
  copyResetTimer = window.setTimeout(() => {
    copiedValue.value = ''
  }, 1500)
}

function fieldCellIsCode(value: string) {
  return value.includes('https://') || value.includes('_') || value.startsWith('/') || value.startsWith('<')
}

function handleKeyboardShortcut(event: KeyboardEvent) {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault()
    document.querySelector<HTMLInputElement>('.agent-docs-page .global-search input')?.focus()
  }
}

watch(() => route.hash, () => {
  mobileOpen.value = false
  window.scrollTo({ top: 0, behavior: 'auto' })
})

onMounted(() => window.addEventListener('keydown', handleKeyboardShortcut))
onUnmounted(() => {
  window.removeEventListener('keydown', handleKeyboardShortcut)
  window.clearTimeout(copyResetTimer)
})
</script>

<template>
  <div class="agent-docs-page site-shell agent-site">
    <header class="topbar">
      <a class="brand" href="/" aria-label="打开 TokenPro 官网">
        <span class="brand-mark" aria-hidden="true"><i /><i /><i /></span>
        <span>TokenPro</span><span class="brand-product">接入指南</span>
      </a>
      <label class="global-search">
        <span>⌕</span>
        <input v-model="query" placeholder="搜索客户端…" aria-label="搜索客户端教程">
        <kbd>⌘ K</kbd>
      </label>
      <nav class="top-actions" aria-label="顶部导航">
        <a href="#codex-desktop">接入文档</a>
        <span class="language-button">简体中文</span>
        <a class="console-button" href="/login">获取 API Key <span>↗</span></a>
      </nav>
      <button class="mobile-menu" aria-label="打开教程目录" @click="mobileOpen = !mobileOpen">☰</button>
    </header>

    <div class="docs-layout">
      <aside class="sidebar" :class="{ open: mobileOpen }">
        <div class="mobile-search">
          <label><span>⌕</span><input v-model="query" placeholder="搜索客户端…"></label>
        </div>
        <nav aria-label="快速接入 Agent">
          <section class="nav-group agent-nav">
            <h2>快速接入 Agent</h2>
            <a
              v-for="item in filteredItems"
              :key="item.key"
              :href="`#${item.key}`"
              :class="{ active: guideKey === item.key }"
            >
              <span>{{ item.label }}</span><small>{{ item.meta }}</small>
            </a>
            <p v-if="!filteredItems.length" class="empty-search">没有找到相关教程</p>
          </section>
        </nav>
        <div class="sidebar-help">
          <span class="pulse-dot" />
          <div><strong>配置遇到问题？</strong><small>准备 request_id 与客户端版本</small></div>
          <span>↗</span>
        </div>
      </aside>

      <main id="top" class="article">
        <div class="prototype-banner">
          <span>TokenPro 实例</span>接口地址已按 tokenpro.work 配置；API Key 与模型 ID 请从登录后的控制台复制。
        </div>
        <div class="breadcrumb"><span>快速接入 Agent</span><b>/</b><span>{{ guide.label }}</span></div>
        <div class="article-heading">
          <div>
            <span class="eyebrow"><i /> {{ guide.eyebrow }}</span>
            <h1>{{ guide.title }}</h1>
            <p>{{ guide.description }}</p>
            <div class="guide-tags"><span v-for="tag in guide.tags" :key="tag">{{ tag }}</span></div>
          </div>
          <span class="updated">最后验证：2026-09-11</span>
        </div>

        <section class="connection-summary">
          <div v-for="item in guide.summary" :key="item.label">
            <small>{{ item.label }}</small>
            <code v-if="item.code">{{ item.value }}</code>
            <strong v-else>{{ item.value }}</strong>
          </div>
        </section>

        <div class="document-body">
          <section v-for="(section, sectionIndex) in guide.sections" :key="section.title" class="docs-section">
            <div class="section-label"><span>{{ String(sectionIndex + 1).padStart(2, '0') }}</span><i /></div>
            <h2>{{ section.title }}</h2>
            <p v-if="section.intro">{{ section.intro }}</p>

            <div v-if="section.links" class="official-links">
              <a v-for="link in section.links" :key="link.href" :href="link.href" target="_blank" rel="noopener noreferrer">
                <span><strong>{{ link.label }}</strong><small>{{ link.meta }}</small></span><b>↗</b>
              </a>
            </div>

            <div
              v-if="section.fields"
              class="content-table"
              :style="{ '--columns': section.fields[0].length }"
            >
              <div
                v-for="(row, rowIndex) in section.fields"
                :key="rowIndex"
                class="content-row"
                :class="{ 'content-head': rowIndex === 0 }"
              >
                <template v-for="(cell, cellIndex) in row" :key="cellIndex">
                  <code v-if="fieldCellIsCode(cell)">{{ cell }}</code>
                  <span v-else>{{ cell }}</span>
                </template>
              </div>
            </div>

            <ol v-if="section.steps" class="agent-steps">
              <li v-for="(step, stepIndex) in section.steps" :key="step">
                <b>{{ stepIndex + 1 }}</b><span>{{ step }}</span>
              </li>
            </ol>

            <div v-if="section.code" class="code-stack">
              <div v-for="block in section.code" :key="block.label" class="code-panel standalone">
                <div class="code-toolbar">
                  <span class="code-label">{{ block.label }}</span>
                  <button class="copy-button" aria-label="复制内容" @click="copy(block.value)">
                    <span>{{ copiedValue === block.value ? '✓' : '⧉' }}</span>{{ copiedValue === block.value ? '已复制' : '复制' }}
                  </button>
                </div>
                <pre><code>{{ block.value }}</code></pre>
              </div>
            </div>

            <div v-if="section.note" class="inline-note info"><span>i</span><p>{{ section.note }}</p></div>
            <div v-if="section.warning" class="inline-note warning"><span>!</span><p>{{ section.warning }}</p></div>
          </section>
        </div>

        <nav class="guide-pagination" aria-label="教程翻页">
          <a v-if="previous" :href="`#${previous.key}`"><small>上一篇</small><strong>← {{ previous.label }}</strong></a><span v-else />
          <a v-if="next" :href="`#${next.key}`" class="next"><small>下一篇</small><strong>{{ next.label }} →</strong></a><span v-else />
        </nav>
        <footer class="article-footer">
          <span>© 2026 TokenPro · 客户端接入指南</span>
          <div><a href="#codex-desktop">Codex</a><a href="#claude-desktop">Claude</a></div>
        </footer>
      </main>

      <aside class="toc" aria-label="教程概览">
        <h2>客户端接入</h2><span class="toc-status"><i /> 2 个教程已整理</span>
        <p>安装 · 配置 · 验证 · 恢复</p><div class="toc-divider" />
        <a href="#codex-desktop">Codex 客户端 <span>→</span></a>
        <a href="#claude-desktop">Claude 客户端 <span>→</span></a>
      </aside>
    </div>
  </div>
</template>

<style scoped src="../../styles/agent-docs.css"></style>
