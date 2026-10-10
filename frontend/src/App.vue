<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { renderSanitizedMarkdown } from './markdown'
import { highlightParts, makeMCPConfig, makeSearchParams, notificationStatus, refreshesSelectedBot, searchSnippet } from './ui-model'
import { readImmersivePreference, readReaderFontSize, writeImmersivePreference, writeReaderFontSize } from './reader-preferences'
import { readThemePreference, writeThemePreference } from './theme-preferences'

const authenticated = ref(false)
const authReady = ref(false)
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
const immersive = ref(readImmersivePreference())
const readerFontSize = ref(readReaderFontSize())
const themeMode = ref(readThemePreference())
const themeMenuOpen = ref(false)
const systemThemeQuery = window.matchMedia('(prefers-color-scheme: dark)')
const systemPrefersDark = ref(systemThemeQuery.matches)
const narrowReader = ref(typeof window !== 'undefined' && window.matchMedia('(max-width: 800px)').matches)
const notificationState = ref('')
const promptOpen = ref(false)
const promptCopied = ref(false)
const promptFallback = ref(false)
const mcpConfigCopied = ref(false)
const mcpConfigFallback = ref(false)
const mcpConfigOpen = ref(false)
const pushTemplate = ref({ endpoint: '', source: '' })
const promptAdvanced = ref(false)
const defaultEndpoint = window.location.origin
const mcpConfig = computed(() => makeMCPConfig(window.location.origin))
const renameTarget = ref(null)
const renameValue = ref('')
const renameError = ref('')
const renameBusy = ref(false)
const readerDialog = ref(null)
const readerCloseButton = ref(null)
const themeOptions = [
  { value: 'light', label: '浅色', icon: '☼' },
  { value: 'dark', label: '深色', icon: '☾' },
  { value: 'system', label: '跟随系统', icon: '◐' },
]
let previousFocus
let lockedScrollY = 0
let previousBodyOverflow = ''
let previousBodyPaddingRight = ''
let streamAbort
let refreshTimer
let authChannel


const renderedMarkdown = computed(() => selectedReport.value
  ? renderSanitizedMarkdown(selectedReport.value.content || '') : '')
const visibleBots = computed(() => bots.value)
const filteredSources = computed(() => sources.value)
const isLoggedIn = computed(() => authenticated.value)
const effectiveTheme = computed(() => themeMode.value === 'system'
  ? (systemPrefersDark.value ? 'dark' : 'light')
  : themeMode.value)
const themeIcon = computed(() => themeOptions.find(option => option.value === themeMode.value)?.icon || '◐')
const themeDescription = computed(() => themeMode.value === 'system'
  ? `跟随设备外观（当前${effectiveTheme.value === 'dark' ? '深色' : '浅色'}）`
  : `始终使用${themeMode.value === 'dark' ? '深色' : '浅色'}外观`)
const activeCount = computed(() => searching.value ? searchResults.value.length : visibleBots.value.length)
const pushPrompt = computed(() => {
  const endpoint = pushTemplate.value.endpoint.trim().replace(/\/$/, '') || window.location.origin
  const sourceName = pushTemplate.value.source.trim()
  const payload = {
    source: sourceName,
    stable_task_id: '<stable platform task ID or stable task slug>',
    task_name: '<original stable task name>',
    executed_at: '<actual RFC3339 execution time with timezone>',
    external_id: '<unique ID for this formal report execution; reuse unchanged on retries>',
    title: '<final report title>',
    summary: '<short summary>',
    tags: [],
    content: '# Final report title\\n\\nComplete Markdown report',
  }
  return `You are a report delivery agent for Bolt Dock. Finish and validate the final report before sending it.\n\nPOST ${endpoint}/api/v1/reports\nContent-Type: application/json\nAuthorization: Bearer <BOLT_DOCK_ACCESS_TOKEN>\n\nSecure setup: read ACCESS_TOKEN from the execution environment's secret store and send it only in the Authorization header. Never include the token in this prompt, report, logs, screenshots, or source control. The server requires this Bearer token for pushes. The endpoint must be publicly reachable over HTTPS; localhost and private network addresses cannot be reached by an external agent.\n\nPayload shape (replace every placeholder with verified task/report values before sending):\n${JSON.stringify(payload, null, 2)}\n\nIdentity rules:\n- Detect the platform source when reliable (for example grok, codex, or qwen). If it cannot be identified, send source as an empty string; the server stores it as unknown.\n- Reuse the same stable_task_id for every run of this task. Use a real stable platform task ID when available; otherwise derive one from the unchanged task name/slug. Never use a random value, date, or run timestamp as task identity. The server deterministically derives bot_id from source and stable_task_id when bot_id is omitted. If you cannot obtain a stable identity, stop and report the missing identity; never create a new Bot per day.\n- Keep task_name stable. If it is unavailable, bot_name may be omitted and the server falls back to the derived bot_id.\n- Use the actual completion time in RFC3339 with its timezone. Generate one unique external_id for each completed formal report and persist/reuse that exact value for retries. Do not derive it from the calendar day.\n\nDelivery rules:\n1. Send exactly once only after the final formal Markdown report is complete and checked. Never send drafts, tests, samples, logs, intermediate output, failed/cancelled work, or empty content.\n2. Preserve the full original Markdown, links, headings, tables, and code. Do not send a summary in place of the report.\n3. HTTP 201 means a new report was stored; HTTP 200 means the same external_id was already stored. Only these responses confirm success. Retry network errors or 5xx a limited number of times with the same external_id; fix a 4xx instead of blindly retrying.\n4. If this platform cannot make HTTPS POST requests, say so clearly and do not claim success. Test only against an isolated test service, never against production.\n\nJSON example for reference only; no placeholder may be sent as a real value.`
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
    credentials: 'same-origin',
    headers: { ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...options.headers },
  })
  if (response.status === 401) {
    expireSession()
    throw new Error('登录会话已失效，请重新登录。')
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
  tokenInput.value = ''
  try {
    const response = await fetch('/api/v1/auth/login', {
      method: 'POST', credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token: value }),
    })
    if (!response.ok) throw new Error(response.status === 401 ? '访问密钥无效，请重试。' : `登录失败 (${response.status})`)
    authenticated.value = true
    try { await loadData() } catch (e) { if (!authenticated.value) throw e; error.value = e.message }
    tokenError.value = ''
    startStream()
  } catch (e) { if (authenticated.value) await fetch('/api/v1/auth/logout', { method: 'POST', credentials: 'same-origin' }).catch(() => {}); authenticated.value = false; tokenError.value = e.message }
}
async function logout() {
  try {
    const response = await fetch('/api/v1/auth/logout', { method: 'POST', credentials: 'same-origin' })
    if (!response.ok && response.status !== 401) throw new Error(`退出失败 (${response.status})`)
  } catch (e) { error.value = e.message || '无法连接服务器，当前会话尚未注销。'; return }
  authChannel?.postMessage({ type: 'logout' })
  expireSession(false)
}

function expireSession(broadcast = true) {
  if (broadcast) authChannel?.postMessage({ type: 'logout' })
  authenticated.value = false
  streamAbort?.abort()
  selected.value = null; selectedReport.value = null; reports.value = []; reportCursor.value = ''
  bots.value = []; sources.value = []; searchResults.value = []; searchCursor.value = ''; searching.value = false
}

async function loadData() {
  if (!authenticated.value) return
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
function closeReader() { selected.value = null; selectedReport.value = null; reports.value = [] }
function toggleImmersive() {
  if (narrowReader.value) return
  immersive.value = !immersive.value
  writeImmersivePreference(immersive.value)
}
function adjustReaderFont(delta) { readerFontSize.value = Math.min(24, Math.max(13, readerFontSize.value + delta)) }
function effectiveBotName(bot) { return bot.display_name || bot.bot_name || bot.bot_id }
function startRename(bot) {
  renameTarget.value = bot
  renameValue.value = effectiveBotName(bot)
  renameError.value = ''
}
async function saveRename(value = renameValue.value) {
  if (!renameTarget.value || renameBusy.value) return
  const target = renameTarget.value
  const displayName = value.trim()
  if (displayName.length > 80) { renameError.value = '名称最多 80 个字符。'; return }
  renameBusy.value = true
  renameError.value = ''
  try {
    const params = new URLSearchParams({ source: target.source, bot_id: target.bot_id })
    await api(`/api/v1/bots?${params}`, { method: 'PATCH', body: JSON.stringify({ display_name: displayName }) })
    renameTarget.value = null
    await loadData()
    if (selected.value?.source === target.source && selected.value?.bot_id === target.bot_id) await loadReports()
    if (searching.value) await runSearch()
    actionNotice.value = displayName ? 'Bot 展示名称已更新。' : 'Bot 已恢复原始名称。'
  } catch (e) { renameError.value = e.message }
  finally { renameBusy.value = false }
}

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
  const name = effectiveBotName(bot)
  if (!window.confirm(`确定永久删除 Bot「${name}」及其全部 ${count} 份报告？此操作无法撤销。`)) return
  deleting.value = `bot:${bot.source}:${bot.bot_id}`
  actionNotice.value = ''
  try {
    const params = new URLSearchParams({ source: bot.source, bot_id: bot.bot_id })
    await api(`/api/v1/bots?${params}`, { method: 'DELETE' })
    await loadData()
    if (searching.value) await runSearch()
    actionNotice.value = `Bot「${name}」及其 ${count} 份报告已删除。`
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
async function copyMCPConfig() {
  mcpConfigCopied.value = false
  mcpConfigFallback.value = false
  try {
    await navigator.clipboard.writeText(mcpConfig.value)
    mcpConfigCopied.value = true
    actionNotice.value = 'Codex MCP 配置已复制。请先在启动 Codex 的环境中设置 BOLT_DOCK_TOKEN。'
    setTimeout(() => { mcpConfigCopied.value = false }, 3000)
  } catch {
    mcpConfigOpen.value = true
    mcpConfigFallback.value = true
    requestAnimationFrame(() => { const field = document.querySelector('.mcp-config-preview'); field?.focus(); field?.select() })
  }
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
  while (!signal.aborted && authenticated.value) {
    try {
      const response = await fetch('/api/v1/events', { credentials: 'same-origin', headers: { Accept: 'text/event-stream' }, signal })
      if (response.status === 401) { expireSession(); return }
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
            if (['report.created', 'report.deleted', 'bot.deleted', 'bot.updated'].includes(event)) {
              clearTimeout(refreshTimer)
              refreshTimer = setTimeout(async () => {
                try {
                  await loadData()
                  const selectedBotChanged = ['report.deleted', 'bot.updated'].includes(event)
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
function onKey(event) {
  if (event.key !== 'Escape') return
  if (themeMenuOpen.value) themeMenuOpen.value = false
  else if (renameTarget.value) renameTarget.value = null
  else if (mcpConfigOpen.value) mcpConfigOpen.value = false
  else if (promptOpen.value) promptOpen.value = false
  else if (selected.value) closeReader()
}

watch(source, async () => {
  if (!authenticated.value) return
  searching.value = Boolean(query.value.trim())
  if (searching.value) await runSearch(); else await loadData()
})
watch(readerFontSize, px => writeReaderFontSize(px))
watch(themeMode, mode => writeThemePreference(mode))
watch(effectiveTheme, theme => {
  document.documentElement.dataset.theme = theme
  document.documentElement.style.colorScheme = theme
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#151a17' : '#f2f2ed')
}, { immediate: true })
watch([selected, promptOpen, mcpConfigOpen, renameTarget], async ([hasReader, hasPrompt, hasMCPConfig, hasRename], [hadReader, hadPrompt, hadMCPConfig, hadRename]) => {
  const shouldLock = Boolean(hasReader || hasPrompt || hasMCPConfig || hasRename)
  const wasLocked = Boolean(hadReader || hadPrompt || hadMCPConfig || hadRename)
  if (shouldLock && !wasLocked) {
    previousFocus = document.activeElement
    lockedScrollY = window.scrollY
    previousBodyOverflow = document.body.style.overflow
    previousBodyPaddingRight = document.body.style.paddingRight
    document.body.style.overflow = 'hidden'
    // html keeps a stable scrollbar gutter; adding body padding would shift the page twice.
  }
  if (!shouldLock && wasLocked) {
    document.body.style.overflow = previousBodyOverflow
    document.body.style.paddingRight = previousBodyPaddingRight
    window.scrollTo(0, lockedScrollY)
    await nextTick()
    if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
  }
  if (hasReader && !hadReader) {
    await nextTick()
    readerCloseButton.value?.focus({ preventScroll: true })
  }
})
function updateNarrowReader() { narrowReader.value = window.matchMedia('(max-width: 800px)').matches }
function updateSystemTheme(event) { systemPrefersDark.value = event.matches }
onMounted(async () => {
  window.addEventListener('keydown', onKey)
  window.addEventListener('resize', updateNarrowReader)
  systemThemeQuery.addEventListener('change', updateSystemTheme)
  if ('BroadcastChannel' in window) {
    authChannel = new BroadcastChannel('bolt-dock-auth')
    authChannel.onmessage = event => { if (event.data?.type === 'logout') expireSession(false) }
  }
  try {
    const response = await fetch('/api/v1/auth/session', { credentials: 'same-origin' })
    if (response.ok && (await response.json()).authenticated) {
      authenticated.value = true
      await loadData()
      startStream()
    }
  } catch {}
  authReady.value = true
})
onUnmounted(() => { window.removeEventListener('keydown', onKey); window.removeEventListener('resize', updateNarrowReader); systemThemeQuery.removeEventListener('change', updateSystemTheme); authChannel?.close(); streamAbort?.abort(); clearTimeout(refreshTimer); document.body.style.overflow = previousBodyOverflow; document.body.style.paddingRight = previousBodyPaddingRight })

function trapReaderFocus(event) {
  const focusable = [...readerDialog.value.querySelectorAll('a[href], button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled), [tabindex]:not([tabindex="-1"])')]
  if (!focusable.length) return
  const first = focusable[0], last = focusable[focusable.length - 1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}
</script>

<template>
  <main v-if="!authReady" class="gate" aria-live="polite"><div class="gate-card"><div class="brand-mark"><img src="/brand/bolt-dock.svg" alt=""></div><p class="eyebrow">PERSONAL REPORT SPACE</p><h1>正在连接 <em>Bolt Dock</em></h1><p class="gate-copy">正在恢复安全会话…</p></div></main>
  <main v-else-if="!isLoggedIn" class="gate">
    <div class="gate-card">
      <div class="brand-mark"><img src="/brand/bolt-dock.svg" alt="Bolt Dock"></div>
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

  <div v-else class="app-shell" @click="themeMenuOpen = false">
    <header class="topbar">
      <a class="wordmark" href="#" @click.prevent="closeReader(); clearSearch()"><span class="brand-mark small"><img src="/brand/bolt-dock.svg" alt=""></span><span>Bolt <b>Dock</b></span></a>
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
        <div class="theme-control" @click.stop>
          <button id="theme-trigger" class="icon-button theme-trigger" type="button" :aria-label="`外观：${themeMode === 'system' ? '跟随系统' : themeMode === 'dark' ? '深色' : '浅色'}`" :aria-expanded="themeMenuOpen" aria-controls="theme-menu" title="外观设置" @click="themeMenuOpen = !themeMenuOpen">{{ themeIcon }}</button>
          <div v-if="themeMenuOpen" id="theme-menu" class="theme-menu" role="group" aria-label="外观模式">
            <button v-for="option in themeOptions" :key="option.value" class="theme-option" :class="{ active: themeMode === option.value }" type="button" :aria-pressed="themeMode === option.value" @click="themeMode = option.value; themeMenuOpen = false">
              <span class="theme-option-icon" aria-hidden="true">{{ option.icon }}</span><span>{{ option.label }}</span><span v-if="themeMode === option.value" class="theme-check" aria-hidden="true">✓</span>
            </button>
            <p class="theme-description">{{ themeDescription }}</p>
          </div>
        </div>
        <button class="copy-prompt-button" @click="openPrompt"><span>＋</span> 推送指令</button>
        <button class="copy-mcp-button" :aria-label="mcpConfigCopied ? 'MCP 配置已复制' : '复制 MCP 配置'" :title="mcpConfigCopied ? 'MCP 配置已复制' : '复制 Codex MCP 配置'" @click="copyMCPConfig"><span>⧉</span><span class="copy-mcp-label">{{ mcpConfigCopied ? '已复制 MCP' : 'MCP 配置' }}</span></button>
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
              <h2><template v-for="(part, index) in highlightParts(item.title || item.display_name || item.bot_name, query)" :key="index"><mark v-if="part.match">{{ part.text }}</mark><span v-else>{{ part.text }}</span></template></h2>
              <p><template v-for="(part, index) in highlightParts(searchSnippet(item, query), query)" :key="index"><mark v-if="part.match">{{ part.text }}</mark><span v-else>{{ part.text }}</span></template></p>
              <div class="result-foot"><span>{{ item.display_name || item.bot_name }}</span><span>打开报告 ↗</span></div>
            </button>
            <button class="delete-action result-delete" :disabled="Boolean(deleting)" :aria-label="`删除报告：${item.title || item.bot_name}`" @click="deleteReport(item)">删除报告</button>
          </div>
          <div v-if="!searchResults.length && !busy" class="empty-state"><div class="empty-icon">⌕</div><h2>没有找到匹配报告</h2><p>试试更短的关键词，或切换到其他来源。</p></div>
          <button v-if="searchCursor" class="load-more" @click="runSearch(true)">加载更多</button>
        </div>
        <div v-else-if="!bots.length && !busy" class="empty-state welcome-empty"><div class="empty-icon">✳</div><p class="eyebrow">A QUIET DOCK</p><h2>等待第一份报告靠岸</h2><p>当 Bot 首次推送报告后，它会自动出现在这里。</p><span>仅显示已通过 API 推送的来源</span></div>
        <div v-else class="bot-grid">
          <div v-for="bot in bots" :key="`${bot.source}:${bot.bot_id}`" class="bot-card-shell">
            <button class="bot-card" :class="{ selected: selected?.source === bot.source && selected?.bot_id === bot.bot_id }" @click="openBot(bot)">
              <div class="card-topline"><span class="source-pill">{{ bot.source }}</span><span v-if="bot.unread_count" class="unread-pill"><i></i> 新报告</span><span v-else class="report-number">{{ bot.report_count }} 份报告</span></div>
              <div class="bot-identity"><div class="avatar-wrap"><img v-if="bot.avatar_url" :src="bot.avatar_url" alt="" @error="avatarFailed"><span class="avatar-fallback" :class="{ hidden: bot.avatar_url }">✳</span></div><div class="bot-titles"><h2>{{ effectiveBotName(bot) }}</h2><p>{{ bot.bot_id }}</p></div><span class="card-arrow">↗</span></div>
              <div class="card-preview"><span class="preview-label">LATEST REPORT</span><strong>{{ bot.latest_report?.title || '暂无报告' }}</strong><p>{{ bot.latest_report?.summary || '新报告将显示在这里。' }}</p></div>
              <div class="card-footer"><span><i class="time-icon">◷</i> {{ formatDate(bot.last_report_at) }}</span><span class="view-link">浏览报告 <b>→</b></span></div>
            </button>
            <button class="rename-action" :aria-label="`重命名 Bot：${effectiveBotName(bot)}`" @click.stop="startRename(bot)">重命名</button>
            <button class="delete-action bot-delete" :disabled="Boolean(deleting)" :aria-label="`删除 Bot：${effectiveBotName(bot)}`" @click.stop="deleteBot(bot)">删除 Bot</button>
          </div>
        </div>
        <div v-if="busy" class="loading-line"><span></span>正在更新…</div>
      </section>

      <div v-if="selected" class="reader-scrim" aria-hidden="true" @click="closeReader"></div>
      <section v-if="selected" ref="readerDialog" class="reader-panel" :class="{ immersive }" role="dialog" aria-modal="true" aria-labelledby="reader-title" tabindex="-1" @keydown.tab="trapReaderFocus">
        <div class="reader-toolbar"><button class="back-button" @click="closeReader"><span>←</span> 返回卡片</button><div class="reader-tools"><label class="date-filter"><span>日期</span><input v-model="reportDate" type="date" @change="loadReports()"></label><div class="font-tools" aria-label="Markdown 字号"><button class="icon-button" :disabled="readerFontSize <= 13" aria-label="缩小 Markdown 字号" title="缩小字号" @click="adjustReaderFont(-1)">−</button><span>{{ readerFontSize }}px</span><button class="icon-button" :disabled="readerFontSize >= 24" aria-label="放大 Markdown 字号" title="放大字号" @click="adjustReaderFont(1)">＋</button><button class="font-reset" aria-label="重置 Markdown 字号" title="重置为 16px" @click="readerFontSize = 16">重置</button></div><button class="icon-button" :disabled="narrowReader" :title="narrowReader ? '移动端详情已全屏显示' : (immersive ? '退出沉浸阅读' : '放大阅读')" :aria-label="immersive ? '退出沉浸阅读' : '放大阅读'" @click="toggleImmersive">{{ immersive ? '⊙' : '⛶' }}</button><button ref="readerCloseButton" class="icon-button close-button" title="关闭" aria-label="关闭阅读面板" @click="closeReader">×</button></div></div>
        <div class="reader-layout">
          <nav class="report-list" aria-label="报告历史">
            <div class="list-heading"><div><span class="eyebrow">{{ selected.source.toUpperCase() }} · HISTORY</span><h2 id="reader-title">{{ effectiveBotName(selected) }}</h2></div><span class="list-count">{{ reports.length }}</span></div>
            <div v-for="item in reports" :key="item.id" class="history-row">
              <button class="history-item" :class="{ active: selectedReport?.id === item.id }" @click="selectReport(item)"><span class="history-date">{{ formatDate(item.executed_at) }}</span><strong>{{ item.title || item.display_name || item.bot_name }}</strong><span>{{ item.summary || '打开查看完整报告' }}</span></button>
              <button class="delete-action history-delete" :disabled="Boolean(deleting)" :aria-label="`删除报告：${item.title || item.bot_name}`" @click="deleteReport(item)">删除</button>
            </div>
            <button v-if="reportCursor" class="history-more" :disabled="busy" @click="loadReports(true)">加载更早报告</button>
            <p v-if="!reports.length && !busy" class="history-empty">这个日期没有报告。</p>
          </nav>
          <article v-if="selectedReport" class="report-document" :style="{ '--reader-font-size': `${readerFontSize}px` }">
            <div class="document-meta"><span class="source-pill">{{ selectedReport.source }}</span><span>{{ selectedReport.display_name || selectedReport.bot_name }}</span><span>·</span><time>{{ formatDate(selectedReport.executed_at) }}</time><button class="delete-action document-delete" :disabled="Boolean(deleting)" @click="deleteReport(selectedReport)">删除报告</button></div>
            <div v-if="selectedReport.tags?.length" class="tag-row"><span v-for="tag in selectedReport.tags" :key="tag"># {{ tag }}</span></div>
            <h1>{{ selectedReport.title || '运行报告' }}</h1>
            <p v-if="selectedReport.summary" class="document-summary">{{ selectedReport.summary }}</p>
            <hr><div class="markdown-body" v-html="renderedMarkdown"></div>
            <footer class="document-end"><span></span> 报告正文结束 <span></span></footer>
          </article>
          <div v-else class="document-empty"><span>✳</span><p>选择一份报告开始阅读</p></div>
        </div>
      </section>
    </div>
    <footer class="app-footer"><span>BOLT DOCK <i>·</i> PERSONAL REPORT SPACE</span><span>数据只保存在你的服务端 <b>↗</b></span></footer>
    <div v-if="promptOpen" class="modal-scrim" @click.self="promptOpen = false">
      <section class="prompt-modal" role="dialog" aria-modal="true" aria-labelledby="prompt-title">
        <header class="modal-heading"><div><p class="eyebrow">REPORT DELIVERY TEMPLATE</p><h2 id="prompt-title">复制 AI 推送指令</h2></div><button class="icon-button" aria-label="关闭" @click="promptOpen = false">×</button></header>
        <p class="modal-intro">指令会自动使用当前站点地址，并要求 Agent 从稳定任务身份生成 Bot。外部 Agent 需要能通过公网 HTTPS 访问；localhost 和内网地址不可达。</p>
        <button class="advanced-toggle" :aria-expanded="promptAdvanced" @click="promptAdvanced = !promptAdvanced">{{ promptAdvanced ? '收起高级设置' : '高级设置' }}</button>
        <div v-if="promptAdvanced" class="template-fields">
          <label class="wide-field">服务地址覆盖<input v-model="pushTemplate.endpoint" :placeholder="defaultEndpoint"></label>
          <label class="wide-field">来源覆盖（留空时自动识别，无法识别则归类 unknown）<input v-model="pushTemplate.source" placeholder="例如 grok、codex；留空自动识别"></label>
        </div>
        <div class="preview-heading"><span>完整指令预览</span><span>Token 使用安全占位符</span></div>
        <textarea class="prompt-preview" :value="pushPrompt" readonly aria-label="完整推送指令预览"></textarea>
        <p v-if="promptFallback" class="copy-fallback">浏览器未开放剪贴板权限。预览内容已选中，请使用 ⌘C / Ctrl+C 复制。</p>
        <footer class="modal-actions"><span v-if="promptCopied" class="copy-success">✓ 已复制完整推送指令</span><span v-else class="copy-note">仅推送最终正式报告，测试请使用独立环境。</span><button class="primary-button copy-action" @click="copyPrompt">{{ promptCopied ? '再次复制' : '复制完整指令' }} <span>↗</span></button></footer>
      </section>
    </div>
    <div v-if="mcpConfigOpen" class="modal-scrim" @click.self="mcpConfigOpen = false">
      <section class="prompt-modal mcp-config-modal" role="dialog" aria-modal="true" aria-labelledby="mcp-config-title">
        <header class="modal-heading"><div><p class="eyebrow">REMOTE MCP · STREAMABLE HTTP</p><h2 id="mcp-config-title">Codex MCP 配置</h2></div><button class="icon-button" aria-label="关闭" @click="mcpConfigOpen = false">×</button></header>
        <p class="modal-intro">配置只包含当前服务地址和环境变量名，不包含 ACCESS_TOKEN。先在运行 Codex 的环境中设置 <code>BOLT_DOCK_TOKEN</code>，其值与 Bolt Dock 的 <code>ACCESS_TOKEN</code> 相同。</p>
        <div class="preview-heading"><span>config.toml 内容</span><span>Streamable HTTP</span></div>
        <textarea class="prompt-preview mcp-config-preview" :value="mcpConfig" readonly aria-label="Codex MCP 配置内容"></textarea>
        <p v-if="mcpConfigFallback" class="copy-fallback">浏览器未开放剪贴板权限。配置已选中，请使用 ⌘C / Ctrl+C 复制。</p>
        <footer class="modal-actions"><span class="copy-note">服务需要从 Codex 环境通过 HTTPS 访问。</span><button class="primary-button copy-action" @click="copyMCPConfig">再次复制配置 <span>↗</span></button></footer>
      </section>
    </div>
    <div v-if="renameTarget" class="modal-scrim" @click.self="renameTarget = null">
      <section class="rename-modal" role="dialog" aria-modal="true" aria-labelledby="rename-title">
        <header class="modal-heading"><div><p class="eyebrow">BOT DISPLAY NAME</p><h2 id="rename-title">重命名 Bot</h2></div><button class="icon-button" aria-label="关闭" @click="renameTarget = null">×</button></header>
        <p class="modal-intro">仅修改工作台展示名称，不会改变 Bot 身份或历史报告。</p>
        <form @submit.prevent="saveRename()">
          <label class="rename-field">展示名称<input v-model="renameValue" autofocus maxlength="80" autocomplete="off"></label>
          <p v-if="renameError" class="form-error" role="alert">{{ renameError }}</p>
          <footer class="modal-actions"><button class="font-reset" type="button" :disabled="renameBusy" @click="saveRename('')">恢复原始名称</button><span></span><button class="primary-button copy-action" type="submit" :disabled="renameBusy">{{ renameBusy ? '保存中…' : '保存名称' }} <span>↗</span></button></footer>
        </form>
      </section>
    </div>
  </div>
</template>
