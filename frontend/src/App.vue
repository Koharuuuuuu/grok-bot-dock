<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { renderSanitizedMarkdown } from './markdown'
import { highlightParts, makeSearchParams, notificationStatus, refreshesSelectedBot, searchSnippet } from './ui-model'

const token = ref('')
const tokenInput = ref('')
const tokenError = ref('')
const source = ref('')
const sources = ref([])
const bots = ref([])
const stats = ref({ bot_count: 0, report_count: 0, database_bytes: 0 })
const selected = ref(null)
const reports = ref([])
const reportCursor = ref('')
const selectedReport = ref(null)
const reportDate = ref('')
const query = ref('')
const searchResults = ref([])
const searchCursor = ref('')
const searchFrom = ref('')
const searchTo = ref('')
const searching = ref(false)
const busy = ref(false)
const error = ref('')
const actionNotice = ref('')
const deleting = ref('')
const immersive = ref(false)
const notificationState = ref('')
const promptOpen = ref(false)
const promptCopied = ref(false)
const promptFallback = ref(false)
const pushTemplate = ref({ endpoint: 'https://YOUR_DOMAIN', source: '', botId: '', botName: '' })
let streamAbort
let refreshTimer

const renderedMarkdown = computed(() => selectedReport.value
  ? renderSanitizedMarkdown(selectedReport.value.content || '') : '')
const visibleBots = computed(() => bots.value)
const filteredSources = computed(() => sources.value)
const isLoggedIn = computed(() => Boolean(token.value))
const activeCount = computed(() => searching.value ? searchResults.value.length : visibleBots.value.length)
const pushPrompt = computed(() => {
  const endpoint = pushTemplate.value.endpoint.trim().replace(/\/$/, '') || 'https://YOUR_DOMAIN'
  const sourceName = pushTemplate.value.source.trim() || 'your-source-id'
  const botId = pushTemplate.value.botId.trim() || 'your-stable-bot-id'
  const botName = pushTemplate.value.botName.trim() || 'Your task name'
  const executedAt = new Date().toISOString()
  const executionDate = executedAt.slice(0, 10)
  return `你是一个定时信息收集 Agent。每次任务执行完成后，按以下规则向 Bolt Dock 推送正式报告。\n\n接口：POST ${endpoint}/api/v1/reports\n请求头：Content-Type: application/json\n请求头：Authorization: Bearer <BOLT_DOCK_TOKEN>（由执行环境提供，不得泄露）\n\n请求 JSON：\n${JSON.stringify({ source: sourceName, bot_id: botId, bot_name: botName, executed_at: executedAt, title: '本次正式报告标题', summary: '简短摘要', tags: ['可选标签'], external_id: `${botId}-${executionDate}`, content: '# 正式报告标题\\n\\n完整 Markdown 正文' }, null, 2)}\n\n要求：\n1. 只在本次任务最终正式报告已经完成、内容检查通过后推送一次；草稿、测试文件、模拟数据、调试日志、中间结果、空内容、失败或取消的任务一律不得推送。\n2. 正文必须是完整 Markdown，保留标题、列表、表格和引用链接；不要推送文件路径、占位符或仅有摘要的内容。\n3. source 与 bot_id 对同一个任务保持稳定；bot_name 可修改；executed_at 使用实际执行时间及明确时区；external_id 对同一次正式执行保持不变，重试时沿用以避免重复。\n4. 发送前校验 source、bot_id、bot_name、executed_at、content 非空；只发送最终 JSON，不要附加 Markdown 代码围栏。\n5. 只有 HTTP 201（新建）或 HTTP 200（幂等重复）算成功；网络错误或 5xx 可有限重试并使用相同 external_id；4xx 先修复配置/报文，不要盲目重试。\n6. 如果当前 AI 平台不能直接发起外部 HTTP POST，应明确报告不支持，不要虚构推送成功；由用户配置可用的 HTTP 调用能力后再执行。\n7. 禁止把测试报告推到正式 API；接口联调必须在独立测试环境完成。`
})

function formatDate(value) {
  if (!value) return '暂无报告'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let n = bytes, i = 0
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(i ? 1 : 0)} ${units[i]}`
}
function avatarFailed(event) { event.target.hidden = true; event.target.nextElementSibling?.classList.remove('hidden') }

async function api(url, options = {}) {
  const response = await fetch(url, {
    ...options,
    headers: { Authorization: `Bearer ${token.value}`, ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...options.headers },
  })
  if (response.status === 401) {
    token.value = ''; throw new Error('访问密钥无效，请重新输入。')
  }
  if (!response.ok) {
    let message = `请求失败 (${response.status})`
    try { message = (await response.json()).error?.message || message } catch {}
    throw new Error(message)
  }
  return response.json()
}

async function login() {
  const value = tokenInput.value.trim()
  if (!value) { tokenError.value = '请输入访问密钥。'; return }
  token.value = value
  try {
    await loadData()
    tokenError.value = ''
    startStream()
    tokenInput.value = ''
  } catch (e) { token.value = ''; tokenError.value = e.message }
}
function logout() {
  token.value = ''
  streamAbort?.abort(); selected.value = null
}

async function loadData() {
  if (!token.value) return
  error.value = ''
  try {
    const params = new URLSearchParams()
    if (source.value) params.set('source', source.value)
    const [sourceData, botData] = await Promise.all([
      api('/api/v1/sources'), api(`/api/v1/bots?${params}`),
    ])
    sources.value = sourceData.items || []
    if (source.value && !sources.value.some(item => item.source === source.value)) source.value = ''
    bots.value = botData.items || []
    stats.value = botData.stats || stats.value
    if (selected.value) {
      const updated = bots.value.find(bot => bot.source === selected.value.source && bot.bot_id === selected.value.bot_id)
      if (updated) selected.value = updated
      else closeReader()
    }
  } catch (e) { error.value = e.message; throw e }
}

async function openBot(bot) {
  searching.value = false; selected.value = bot; selectedReport.value = null
  reportDate.value = ''; reports.value = []; reportCursor.value = ''
  await loadReports()
}
async function loadReports(append = false) {
  if (!selected.value) return
  busy.value = true; error.value = ''
  try {
    if (!append) selectedReport.value = null
    const params = new URLSearchParams({ source: selected.value.source, bot_id: selected.value.bot_id, limit: '20' })
    if (reportDate.value) params.set('date', reportDate.value)
    if (append && reportCursor.value) params.set('cursor', reportCursor.value)
    const page = await api(`/api/v1/reports?${params}`)
    reports.value = append ? [...reports.value, ...page.items] : page.items
    reportCursor.value = page.next_cursor || ''
    if (!append && reports.value.length) selectReport(reports.value[0])
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function selectReport(report) {
  selectedReport.value = report
  if (selected.value) {
    try {
      await api('/api/v1/bots/read', { method: 'POST', body: JSON.stringify({ source: selected.value.source, bot_id: selected.value.bot_id, report_id: report.id }) })
      const bot = bots.value.find(item => item.source === selected.value.source && item.bot_id === selected.value.bot_id)
      if (bot?.latest_report?.id <= report.id) bot.unread_count = 0
    } catch {}
  }
}
function closeReader() { selected.value = null; selectedReport.value = null; reports.value = []; immersive.value = false }
function toggleImmersive() { immersive.value = !immersive.value }

async function deleteReport(report) {
  const name = report.title || '未命名报告'
  if (!window.confirm(`确定永久删除报告「${name}」？此操作无法撤销。`)) return
  deleting.value = `report:${report.id}`
  actionNotice.value = ''
  try {
    await api(`/api/v1/reports/${report.id}`, { method: 'DELETE' })
    await loadData()
    if (selected.value?.source === report.source && selected.value?.bot_id === report.bot_id) await loadReports()
    if (searching.value) await runSearch()
    actionNotice.value = '报告已删除。'
  } catch (e) { error.value = e.message }
  finally { deleting.value = '' }
}

async function deleteBot(bot) {
  const count = bot.report_count || 0
  if (!window.confirm(`确定永久删除 Bot「${bot.bot_name}」及其全部 ${count} 份报告？此操作无法撤销。`)) return
  deleting.value = `bot:${bot.source}:${bot.bot_id}`
  actionNotice.value = ''
  try {
    const params = new URLSearchParams({ source: bot.source, bot_id: bot.bot_id })
    await api(`/api/v1/bots?${params}`, { method: 'DELETE' })
    await loadData()
    if (searching.value) await runSearch()
    actionNotice.value = `Bot「${bot.bot_name}」及其 ${count} 份报告已删除。`
  } catch (e) { error.value = e.message }
  finally { deleting.value = '' }
}

async function runSearch(append = false) {
  const term = query.value.trim()
  if (!term) { searching.value = false; searchResults.value = []; await loadData(); return }
  searching.value = true; busy.value = true; selected.value = null; selectedReport.value = null; error.value = ''
  try {
    const params = makeSearchParams({ query: term, source: source.value, cursor: append ? searchCursor.value : '', from: searchFrom.value, to: searchTo.value })
    const page = await api(`/api/v1/search?${params}`)
    searchResults.value = append ? [...searchResults.value, ...page.items] : page.items
    searchCursor.value = page.next_cursor || ''
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
function openSearchResult(report) {
  const bot = bots.value.find(item => item.source === report.source && item.bot_id === report.bot_id) || {
    source: report.source, bot_id: report.bot_id, bot_name: report.bot_name, avatar_url: report.avatar_url,
  }
  openBot(bot).then(() => selectReport(reports.value.find(item => item.id === report.id) || report))
}
function clearSearch() { query.value = ''; searchFrom.value = ''; searchTo.value = ''; searching.value = false; searchResults.value = []; loadData() }
function openPrompt() { promptOpen.value = true; promptCopied.value = false; promptFallback.value = false }
async function copyPrompt() {
  try { await navigator.clipboard.writeText(pushPrompt.value); promptCopied.value = true; promptFallback.value = false }
  catch { promptCopied.value = false; promptFallback.value = true; requestAnimationFrame(() => { const field = document.querySelector('.prompt-preview'); field?.focus(); field?.select() }) }
}

async function enableNotifications() {
  if (!('Notification' in window)) { notificationState.value = '当前浏览器不支持通知'; return }
  try {
    const permission = await Notification.requestPermission()
    notificationState.value = notificationStatus(true, permission)
  } catch { notificationState.value = notificationStatus(true, 'default') }
  setTimeout(() => { notificationState.value = '' }, 2600)
}
function notify(report) {
  if (!('Notification' in window) || Notification.permission !== 'granted' || document.visibilityState === 'visible') return
  const note = new Notification(report.bot_name || 'Bolt Dock', { body: report.title || report.summary || '收到新报告', tag: `${report.source}:${report.bot_id}` })
  note.onclick = () => { window.focus(); const bot = bots.value.find(x => x.source === report.source && x.bot_id === report.bot_id); if (bot) openBot(bot) }
}
async function startStream() {
  streamAbort?.abort(); streamAbort = new AbortController()
  const { signal } = streamAbort
  let connectedBefore = false
  while (!signal.aborted && token.value) {
    try {
      const response = await fetch('/api/v1/events', { headers: { Authorization: `Bearer ${token.value}`, Accept: 'text/event-stream' }, signal })
      if (!response.ok || !response.body) throw new Error('stream unavailable')
      if (connectedBefore) await loadData()
      connectedBefore = true
      const reader = response.body.getReader(), decoder = new TextDecoder()
      let buffer = ''
      while (!signal.aborted) {
        const { value, done } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        let split
        while ((split = buffer.indexOf('\n\n')) !== -1) {
          const message = buffer.slice(0, split); buffer = buffer.slice(split + 2)
          if (message.startsWith(':')) continue
          const event = message.split('\n').find(line => line.startsWith('event: '))?.slice(7)
          const data = message.split('\n').find(line => line.startsWith('data: '))?.slice(6)
          if (data) {
            const payload = JSON.parse(data)
            if (event === 'report.created') notify(payload)
            if (['report.created', 'report.deleted', 'bot.deleted'].includes(event)) {
              clearTimeout(refreshTimer)
              refreshTimer = setTimeout(async () => {
                try {
                  await loadData()
                  const selectedBotChanged = event === 'report.deleted'
                    && selected.value?.source === payload.source
                    && selected.value?.bot_id === payload.bot_id
                  if (selectedBotChanged || (event === 'report.created' && refreshesSelectedBot(selected.value, payload))) await loadReports()
                  if (searching.value) await runSearch()
                } catch {}
              }, 120)
            }
          }
        }
      }
    } catch (e) { if (signal.aborted) return }
    await new Promise(resolve => setTimeout(resolve, 3000))
  }
}
function onKey(event) { if (event.key === 'Escape' && promptOpen.value) promptOpen.value = false; else if (event.key === 'Escape' && immersive.value) immersive.value = false; else if (event.key === 'Escape' && selected.value) closeReader() }

watch(source, async () => {
  if (!token.value) return
  searching.value = Boolean(query.value.trim())
  if (searching.value) await runSearch(); else await loadData()
})
onMounted(async () => {
  window.addEventListener('keydown', onKey)
})
onUnmounted(() => { window.removeEventListener('keydown', onKey); streamAbort?.abort(); clearTimeout(refreshTimer) })
</script>

<template>
  <main v-if="!isLoggedIn" class="gate">
    <div class="gate-card">
      <div class="brand-mark">B<span>.</span></div>
      <p class="eyebrow">PERSONAL REPORT SPACE</p>
      <h1>欢迎来到 <em>Bolt Dock</em></h1>
      <p class="gate-copy">输入部署时设置的访问密钥，查看你的 Bot 报告。</p>
      <form @submit.prevent="login">
        <label class="sr-only" for="access-token">访问密钥</label>
        <input id="access-token" v-model="tokenInput" type="password" autocomplete="current-password" placeholder="访问密钥" autofocus>
        <button class="primary-button" type="submit">进入工作台 <span>↗</span></button>
      </form>
      <p v-if="tokenError" class="form-error">{{ tokenError }}</p>
      <div class="gate-foot"><span class="live-dot"></span> 本地密钥保护 · 同源连接</div>
    </div>
    <div class="gate-orbit orbit-one"></div><div class="gate-orbit orbit-two"></div>
  </main>

  <div v-else class="app-shell" :class="{ 'reader-open': selected, immersive }">
    <header class="topbar">
      <a class="wordmark" href="#" @click.prevent="closeReader(); clearSearch()"><span class="brand-mark small">B<span>.</span></span><span>Bolt <b>Dock</b></span></a>
      <form class="search-box" @submit.prevent="runSearch()">
        <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="10.8" cy="10.8" r="6.8"/><path d="m16 16 5 5"/></svg>
        <input v-model="query" aria-label="搜索所有报告" placeholder="搜索所有报告…" @keydown.esc="clearSearch">
        <span v-if="!query" class="key-hint">↵</span>
        <button v-else class="clear-search" type="button" aria-label="清除搜索" @click="clearSearch">×</button>
      </form>
      <div class="top-actions">
        <label class="source-select-wrap"><span class="sr-only">选择来源</span>
          <select v-model="source" aria-label="筛选报告来源"><option value="">全部来源</option><option v-for="item in filteredSources" :key="item.source" :value="item.source">{{ item.source }} <small>· {{ item.report_count }}</small></option></select><span class="select-caret">⌄</span>
        </label>
        <button class="copy-prompt-button" @click="openPrompt"><span>＋</span> 推送指令</button>
        <div class="top-divider"></div>
        <div class="stat"><span class="stat-value">{{ stats.bot_count }}</span><span class="stat-label">Bots</span></div>
        <div class="stat"><span class="stat-value">{{ stats.report_count }}</span><span class="stat-label">报告</span></div>
        <div class="stat database-stat"><span class="stat-value">{{ formatBytes(stats.database_bytes) }}</span><span class="stat-label">全库</span></div>
        <button class="icon-button notify-button" title="启用桌面通知" aria-label="启用桌面通知" @click="enableNotifications">◉</button>
        <button class="icon-button logout-button" title="退出登录" aria-label="退出登录" @click="logout">↗</button>
      </div>
    </header>

    <div v-if="notificationState" class="toast">{{ notificationState }}</div>
    <div class="content-area">
      <section class="main-column">
        <div class="page-heading">
          <div><p class="eyebrow">{{ source ? source.toUpperCase() + ' · REPORTS' : 'YOUR SOURCES · LIVE' }}</p><h1>{{ searching ? '搜索结果' : '报告工作台' }}<span class="heading-count">{{ activeCount }}</span></h1></div>
          <div class="heading-meta"><span class="live-dot"></span> 实时同步</div>
        </div>
        <div v-if="searching" class="search-filters"><span>报告日期</span><label>从<input v-model="searchFrom" type="date" @change="runSearch()"></label><label>至<input v-model="searchTo" type="date" @change="runSearch()"></label></div>
        <div v-if="error" class="error-banner"><span>!</span>{{ error }}<button @click="error = ''">×</button></div>
        <div v-if="actionNotice" class="action-notice" role="status">{{ actionNotice }}<button aria-label="关闭提示" @click="actionNotice = ''">×</button></div>
        <div v-if="searching" class="result-grid">
          <div v-for="item in searchResults" :key="item.id" class="result-row">
            <button class="result-card" @click="openSearchResult(item)">
              <div class="result-top"><span class="source-pill">{{ item.source }}</span><span>{{ formatDate(item.executed_at) }}</span></div>
              <h2><template v-for="(part, index) in highlightParts(item.title || item.bot_name, query)" :key="index"><mark v-if="part.match">{{ part.text }}</mark><span v-else>{{ part.text }}</span></template></h2>
              <p><template v-for="(part, index) in highlightParts(searchSnippet(item, query), query)" :key="index"><mark v-if="part.match">{{ part.text }}</mark><span v-else>{{ part.text }}</span></template></p>
              <div class="result-foot"><span>{{ item.bot_name }}</span><span>打开报告 ↗</span></div>
            </button>
            <button class="delete-action result-delete" :disabled="Boolean(deleting)" :aria-label="`删除报告：${item.title || item.bot_name}`" @click="deleteReport(item)">删除报告</button>
          </div>
          <div v-if="!searchResults.length && !busy" class="empty-state"><div class="empty-icon">⌕</div><h2>没有找到匹配报告</h2><p>试试更短的关键词，或切换到其他来源。</p></div>
          <button v-if="searchCursor" class="load-more" @click="runSearch(true)">加载更多</button>
        </div>
        <div v-else-if="!bots.length && !busy" class="empty-state welcome-empty"><div class="empty-icon">✳</div><p class="eyebrow">A QUIET DOCK</p><h2>等待第一份报告靠岸</h2><p>当 Bot 首次推送报告后，它会自动出现在这里。</p><span>仅显示已通过 API 推送的来源</span></div>
        <div v-else class="bot-grid" :class="{ 'with-reader': selected }">
          <div v-for="bot in bots" :key="`${bot.source}:${bot.bot_id}`" class="bot-card-shell">
            <button class="bot-card" :class="{ selected: selected?.source === bot.source && selected?.bot_id === bot.bot_id }" @click="openBot(bot)">
              <div class="card-topline"><span class="source-pill">{{ bot.source }}</span><span v-if="bot.unread_count" class="unread-pill"><i></i> 新报告</span><span v-else class="report-number">{{ bot.report_count }} 份报告</span></div>
              <div class="bot-identity"><div class="avatar-wrap"><img v-if="bot.avatar_url" :src="bot.avatar_url" alt="" @error="avatarFailed"><span class="avatar-fallback" :class="{ hidden: bot.avatar_url }">✳</span></div><div class="bot-titles"><h2>{{ bot.bot_name }}</h2><p>{{ bot.bot_id }}</p></div><span class="card-arrow">↗</span></div>
              <div class="card-preview"><span class="preview-label">LATEST REPORT</span><strong>{{ bot.latest_report?.title || '暂无报告' }}</strong><p>{{ bot.latest_report?.summary || '新报告将显示在这里。' }}</p></div>
              <div class="card-footer"><span><i class="time-icon">◷</i> {{ formatDate(bot.last_report_at) }}</span><span class="view-link">浏览报告 <b>→</b></span></div>
            </button>
            <button class="delete-action bot-delete" :disabled="Boolean(deleting)" :aria-label="`删除 Bot：${bot.bot_name}`" @click.stop="deleteBot(bot)">删除 Bot</button>
          </div>
        </div>
        <div v-if="busy" class="loading-line"><span></span>正在更新…</div>
      </section>

      <aside v-if="selected" class="reader-panel" :class="{ immersive }">
        <div class="reader-toolbar"><button class="back-button" @click="closeReader"><span>←</span> 返回卡片</button><div class="reader-tools"><label class="date-filter"><span>日期</span><input v-model="reportDate" type="date" @change="loadReports()"></label><button class="icon-button" :title="immersive ? '退出沉浸阅读' : '放大阅读'" :aria-label="immersive ? '退出沉浸阅读' : '放大阅读'" @click="toggleImmersive">{{ immersive ? '⊙' : '⛶' }}</button><button class="icon-button close-button" title="关闭" aria-label="关闭阅读面板" @click="closeReader">×</button></div></div>
        <div class="reader-layout">
          <nav class="report-list" aria-label="报告历史">
            <div class="list-heading"><div><span class="eyebrow">{{ selected.source.toUpperCase() }} · HISTORY</span><h2>{{ selected.bot_name }}</h2></div><span class="list-count">{{ reports.length }}</span></div>
            <div v-for="item in reports" :key="item.id" class="history-row">
              <button class="history-item" :class="{ active: selectedReport?.id === item.id }" @click="selectReport(item)"><span class="history-date">{{ formatDate(item.executed_at) }}</span><strong>{{ item.title || item.bot_name }}</strong><span>{{ item.summary || '打开查看完整报告' }}</span></button>
              <button class="delete-action history-delete" :disabled="Boolean(deleting)" :aria-label="`删除报告：${item.title || item.bot_name}`" @click="deleteReport(item)">删除</button>
            </div>
            <button v-if="reportCursor" class="history-more" :disabled="busy" @click="loadReports(true)">加载更早报告</button>
            <p v-if="!reports.length && !busy" class="history-empty">这个日期没有报告。</p>
          </nav>
          <article v-if="selectedReport" class="report-document">
            <div class="document-meta"><span class="source-pill">{{ selectedReport.source }}</span><span>{{ selectedReport.bot_name }}</span><span>·</span><time>{{ formatDate(selectedReport.executed_at) }}</time><button class="delete-action document-delete" :disabled="Boolean(deleting)" @click="deleteReport(selectedReport)">删除报告</button></div>
            <div v-if="selectedReport.tags?.length" class="tag-row"><span v-for="tag in selectedReport.tags" :key="tag"># {{ tag }}</span></div>
            <h1>{{ selectedReport.title || '运行报告' }}</h1>
            <p v-if="selectedReport.summary" class="document-summary">{{ selectedReport.summary }}</p>
            <hr><div class="markdown-body" v-html="renderedMarkdown"></div>
            <footer class="document-end"><span></span> 报告正文结束 <span></span></footer>
          </article>
          <div v-else class="document-empty"><span>✳</span><p>选择一份报告开始阅读</p></div>
        </div>
      </aside>
    </div>
    <footer class="app-footer"><span>BOLT DOCK <i>·</i> PERSONAL REPORT SPACE</span><span>数据只保存在你的服务端 <b>↗</b></span></footer>
    <div v-if="promptOpen" class="modal-scrim" @click.self="promptOpen = false">
      <section class="prompt-modal" role="dialog" aria-modal="true" aria-labelledby="prompt-title">
        <header class="modal-heading"><div><p class="eyebrow">REPORT DELIVERY TEMPLATE</p><h2 id="prompt-title">复制 AI 推送指令</h2></div><button class="icon-button" aria-label="关闭" @click="promptOpen = false">×</button></header>
        <p class="modal-intro">填入任务信息，Bolt Dock 会生成可直接交给 AI Agent 的完整推送规范。</p>
        <div class="template-fields">
          <label class="wide-field">服务地址<input v-model="pushTemplate.endpoint" placeholder="https://YOUR_DOMAIN"></label>
          <label>来源标识<input v-model="pushTemplate.source" placeholder="小写字母、数字和连字符"></label>
          <label>稳定 Bot ID<input v-model="pushTemplate.botId" placeholder="例如 daily-briefing"></label>
          <label class="wide-field">Bot 名称<input v-model="pushTemplate.botName" placeholder="例如 每日行业简报"></label>
        </div>
        <div class="preview-heading"><span>完整指令预览</span><span>Token 使用安全占位符</span></div>
        <textarea class="prompt-preview" :value="pushPrompt" readonly aria-label="完整推送指令预览"></textarea>
        <p v-if="promptFallback" class="copy-fallback">浏览器未开放剪贴板权限。预览内容已选中，请使用 ⌘C / Ctrl+C 复制。</p>
        <footer class="modal-actions"><span v-if="promptCopied" class="copy-success">✓ 已复制完整推送指令</span><span v-else class="copy-note">仅推送最终正式报告，测试请使用独立环境。</span><button class="primary-button copy-action" @click="copyPrompt">{{ promptCopied ? '再次复制' : '复制完整指令' }} <span>↗</span></button></footer>
      </section>
    </div>
  </div>
</template>
