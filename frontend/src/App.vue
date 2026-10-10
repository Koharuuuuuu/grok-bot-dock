<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { renderSanitizedMarkdown } from './markdown'
import { buildMCPServiceURL, classifyMCPAddress, highlightParts, makeCodexMCPConfig, makeMCPGenericGuide, makeSearchParams, normalizeMCPAddressBase, notificationStatus, readMCPAddressOverride, refreshesSelectedBot, searchSnippet, writeMCPAddressOverride } from './ui-model'
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
const mcpConfigOpen = ref(false)
const mcpCapabilities = ref(null)
const mcpCapabilitiesLoading = ref(false)
const mcpCapabilitiesError = ref('')
const mcpAddressOverride = ref(readMCPAddressOverride())
const mcpAddressDraft = ref(mcpAddressOverride.value || window.location.origin)
const mcpAddressError = ref('')
const mcpCopyStatus = ref('')
const mcpCopyFallback = ref(false)
const mcpFallbackText = ref('')
const oauthClients = ref([])
const oauthGrants = ref([])
const oauthClientName = ref('')
const oauthRedirectURI = ref('')
const oauthConfidential = ref(false)
const oauthCreatedClient = ref(null)
const oauthManagementError = ref('')
const oauthManagementBusy = ref(false)
const mcpDialog = ref(null)
const mcpCloseButton = ref(null)
const pushTemplate = ref({ endpoint: '', source: '' })
const promptAdvanced = ref(false)
const defaultEndpoint = window.location.origin
const mcpServiceURL = computed(() => {
  try { return buildMCPServiceURL(window.location.origin, mcpAddressOverride.value) }
  catch { return '' }
})
const mcpAddressScope = computed(() => classifyMCPAddress(mcpServiceURL.value))
const mcpCodexExample = computed(() => makeCodexMCPConfig(mcpServiceURL.value || buildMCPServiceURL(window.location.origin)))
const mcpGenericGuide = computed(() => makeMCPGenericGuide(mcpServiceURL.value, mcpCapabilities.value?.tools || [], mcpCapabilities.value?.oauth_enabled === true))
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
async function openMCPAccess() {
  mcpConfigOpen.value = true
  mcpAddressDraft.value = mcpAddressOverride.value || window.location.origin
  mcpAddressError.value = ''
  mcpCopyStatus.value = ''
  mcpCapabilities.value = null
  mcpCapabilitiesError.value = ''
  mcpCapabilitiesLoading.value = true
  try {
    mcpCapabilities.value = await api('/api/v1/mcp/capabilities')
    oauthCreatedClient.value = null
    oauthManagementError.value = ''
    if (mcpCapabilities.value.oauth_enabled) await loadOAuthManagement()
  } catch (e) { mcpCapabilitiesError.value = e.message || '无法读取当前服务的 MCP 能力清单。' }
  finally { mcpCapabilitiesLoading.value = false }
}
async function loadOAuthManagement() {
  const [clients, grants] = await Promise.all([api('/api/v1/oauth/clients'), api('/api/v1/oauth/grants')])
  oauthClients.value = clients.items || []
  oauthGrants.value = grants.items || []
}
async function createOAuthClient() {
  if (!oauthClientName.value.trim() || !oauthRedirectURI.value.trim() || oauthManagementBusy.value) return
  oauthManagementBusy.value = true
  oauthManagementError.value = ''
  try {
    oauthCreatedClient.value = await api('/api/v1/oauth/clients', { method: 'POST', body: JSON.stringify({
      name: oauthClientName.value.trim(), redirect_uris: oauthRedirectURI.value.split(/\s+/).filter(Boolean), confidential: oauthConfidential.value,
    }) })
    oauthClientName.value = ''; oauthRedirectURI.value = ''; oauthConfidential.value = false
    await loadOAuthManagement()
  } catch (e) { oauthManagementError.value = e.message }
  finally { oauthManagementBusy.value = false }
}
async function disableOAuthClient(client) {
  if (!window.confirm(`停用 ${client.client_name}？相关授权和 Token 会立即失效。`)) return
  try { await api(`/api/v1/oauth/clients/${encodeURIComponent(client.client_id)}`, { method: 'DELETE' }); await loadOAuthManagement() }
  catch (e) { oauthManagementError.value = e.message }
}
async function rotateOAuthClientSecret(client) {
  if (!window.confirm(`为 ${client.client_name} 轮换 Client Secret？新 Secret 只显示一次。`)) return
  try {
    oauthCreatedClient.value = { ...await api(`/api/v1/oauth/clients/${encodeURIComponent(client.client_id)}/rotate-secret`, { method: 'POST' }), notice: 'Client Secret 已轮换' }
    await loadOAuthManagement()
  } catch (e) { oauthManagementError.value = e.message }
}
async function revokeOAuthGrant(grant) {
  if (!window.confirm(`撤销 ${grant.client_name} 的这条授权？现有 Token 会立即失效。`)) return
  try { await api(`/api/v1/oauth/grants/${grant.id}`, { method: 'DELETE' }); await loadOAuthManagement() }
  catch (e) { oauthManagementError.value = e.message }
}
function copyOAuthClientSecret() {
  if (!oauthCreatedClient.value?.client_secret) return
  copyMCPText(oauthCreatedClient.value.client_secret, 'Client Secret 已复制；此处不再显示第二次。')
}
function saveMCPAddress() {
  try {
    const base = normalizeMCPAddressBase(mcpAddressDraft.value)
    const pageBase = normalizeMCPAddressBase(window.location.origin)
    mcpAddressOverride.value = base === pageBase ? '' : base
    writeMCPAddressOverride(mcpAddressOverride.value)
    mcpAddressDraft.value = base
    mcpAddressError.value = ''
    mcpCopyStatus.value = 'MCP 服务地址已保存到此浏览器。'
  } catch (e) { mcpAddressError.value = e.message }
}
function resetMCPAddress() {
  mcpAddressDraft.value = window.location.origin
  saveMCPAddress()
}
async function copyMCPText(value, successMessage) {
  if (!value) return
  mcpCopyStatus.value = ''
  mcpCopyFallback.value = false
  mcpFallbackText.value = value
  try {
    await navigator.clipboard.writeText(value)
    mcpCopyStatus.value = successMessage
  } catch {
    mcpCopyFallback.value = true
    requestAnimationFrame(() => {
      const field = document.querySelector('.mcp-copy-fallback-field')
      field?.focus()
      field?.select()
    })
  }
}
function copyMCPServiceURL() { return copyMCPText(mcpServiceURL.value, '已复制 MCP 服务地址。') }
function copyMCPGenericGuide() { return copyMCPText(mcpGenericGuide.value, '已复制通用 MCP 接入说明。') }
function copyCodexExample() { return copyMCPText(mcpCodexExample.value, '已复制 Codex CLI 示例；请在客户端安全设置中配置密钥。') }
function openRESTPushInstructions() { mcpConfigOpen.value = false; openPrompt() }
function trapMCPFocus(event) {
  if (event.key !== 'Tab' || !mcpDialog.value) return
  const focusable = [...mcpDialog.value.querySelectorAll('button:not(:disabled), input:not(:disabled), textarea:not(:disabled), summary')]
    .filter(element => element.getClientRects().length > 0)
  if (!focusable.length) { event.preventDefault(); mcpDialog.value.focus(); return }
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
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
  if (hasMCPConfig && !hadMCPConfig) {
    await nextTick()
    mcpCloseButton.value?.focus({ preventScroll: true })
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
        <button class="copy-prompt-button" aria-label="打开 REST API 推送指令" title="REST API 推送指令" @click="openPrompt"><span>＋</span> REST 推送</button>
        <button class="mcp-access-button" aria-label="打开 MCP 接入" title="MCP 接入" @click="openMCPAccess"><span>⌁</span><span class="mcp-access-label">MCP 接入</span></button>
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
        <header class="modal-heading"><div><p class="eyebrow">REST API · REPORT DELIVERY</p><h2 id="prompt-title">REST API 推送指令</h2></div><button class="icon-button" aria-label="关闭 REST API 推送指令" @click="promptOpen = false">×</button></header>
        <p class="modal-intro">此路径通过 REST API 的 HTTPS POST 推送报告，适合脚本、Webhook 和不支持远程 MCP 的平台。支持 MCP 的客户端可从顶栏「MCP 接入」查看独立 MCP 服务地址。指令会使用当前站点地址，并要求 Agent 从稳定任务身份生成 Bot。</p>
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
      <section ref="mcpDialog" class="prompt-modal mcp-access-modal" role="dialog" aria-modal="true" aria-labelledby="mcp-access-title" tabindex="-1" @keydown.tab="trapMCPFocus">
        <header class="modal-heading"><div><p class="eyebrow">REMOTE MCP · STREAMABLE HTTP</p><h2 id="mcp-access-title">MCP 服务接入</h2></div><button ref="mcpCloseButton" class="icon-button" aria-label="关闭 MCP 接入" @click="mcpConfigOpen = false">×</button></header>
        <p class="modal-intro">将 Bot Dock 作为 MCP 服务，供支持远程 Streamable HTTP MCP 的 AI Agent 发布和查询报告。MCP 是独立服务，不依赖 Codex。</p>

        <section class="mcp-access-section" aria-labelledby="mcp-address-heading">
          <div class="mcp-section-heading"><h3 id="mcp-address-heading">服务连接信息</h3><span class="mcp-protocol-badge">{{ mcpCapabilities?.transport || (mcpCapabilitiesLoading ? '读取能力中…' : '未确认') }}</span></div>
          <label class="mcp-address-label" for="mcp-address-input">对外服务地址（可覆盖反向代理域名）</label>
          <div class="mcp-address-editor"><input id="mcp-address-input" v-model="mcpAddressDraft" type="url" autocomplete="url" spellcheck="false" aria-describedby="mcp-address-help"><button class="secondary-button" @click="saveMCPAddress">保存地址</button><button class="text-button" @click="resetMCPAddress">恢复页面地址</button></div>
          <p id="mcp-address-help" class="mcp-field-help">页面地址会自动补上 <code>/mcp</code>。如反向代理使用专用域名或路径，可填写对外 origin/path；覆盖值只保存在此浏览器。</p>
          <p v-if="mcpAddressError" class="form-error" role="alert">{{ mcpAddressError }}</p>
          <div class="mcp-service-url"><div><span class="mcp-field-label">MCP 服务地址</span><code :title="mcpServiceURL">{{ mcpServiceURL || '请填写有效的 HTTPS 服务地址' }}</code></div><button class="primary-button" :disabled="!mcpServiceURL" @click="copyMCPServiceURL">复制服务地址</button></div>
          <p v-if="mcpAddressScope === 'invalid'" class="mcp-capability-error" role="alert">当前 MCP 服务地址无效，请输入有效地址后保存。</p>
          <p v-else-if="mcpAddressScope === 'local'" class="mcp-local-warning" role="note">此地址指向本机或私有网络，只适用于能够访问该网络的客户端；远程 Agent 无法通过公网访问。请配置可达的 HTTPS 反向代理地址。</p>
          <p v-else-if="mcpCapabilities" class="mcp-field-help">认证方式：<code>{{ mcpCapabilities.authentication }}</code>。页面不会读取管理员密钥，也不显示在线状态；地址能否从目标客户端访问取决于网络和反向代理配置。</p>
        </section>

        <section class="mcp-access-section" aria-labelledby="mcp-tools-heading">
          <div class="mcp-section-heading"><h3 id="mcp-tools-heading">当前实例提供的工具</h3><span v-if="mcpCapabilitiesLoading" class="mcp-capability-state">读取中…</span></div>
          <p v-if="mcpCapabilitiesError" class="mcp-capability-error" role="alert">{{ mcpCapabilitiesError }} 页面不会根据版本假定工具已就绪。</p>
          <ul v-else-if="mcpCapabilities" class="mcp-tool-list">
            <li v-for="tool in mcpCapabilities.tools" :key="tool.name">
              <div class="mcp-tool-title"><code>{{ tool.name }}</code><span>{{ tool.access }}</span></div>
              <p>{{ tool.description }}</p>
              <small>{{ tool.fields.join('；') }}</small>
              <div class="mcp-tool-example"><span>示例参数</span><code>{{ tool.example }}</code></div>
            </li>
          </ul>
          <p v-else-if="!mcpCapabilitiesLoading" class="mcp-field-help">当前实例没有返回工具能力清单。</p>
          <p class="mcp-field-help">Agent 能发现并调用工具不代表它会自动推送；这取决于具体客户端、权限和任务设置。</p>
        </section>

        <section v-if="mcpCapabilities?.oauth_enabled" class="mcp-access-section" aria-labelledby="oauth-owner-heading">
          <div class="mcp-section-heading"><h3 id="oauth-owner-heading">OAuth 客户端与授权</h3><span class="mcp-protocol-badge">PKCE · 独立凭据</span></div>
          <p class="mcp-field-help">可在此预登记客户端，也可由支持 CIMD 的公开客户端提交 HTTPS 元数据地址；两种方式都要核对精确回调 URI 并经管理员同意。机密客户端的 Client Secret 仅在创建或轮换时显示一次；OAuth 默认无刷新令牌，Token 有效期 15 分钟，授权最长 30 天。</p>
          <form class="oauth-client-form" @submit.prevent="createOAuthClient">
            <label>客户端名称<input v-model="oauthClientName" maxlength="120" required autocomplete="off"></label>
            <label>精确回调 URI（每行一个）<textarea v-model="oauthRedirectURI" required rows="2" spellcheck="false" placeholder="https://client.example/oauth/callback"></textarea></label>
            <label class="oauth-checkbox"><input v-model="oauthConfidential" type="checkbox"> 此客户端可安全保存 Client Secret（机密客户端）</label>
            <button class="secondary-button" :disabled="oauthManagementBusy">登记客户端</button>
          </form>
          <p v-if="oauthManagementError" class="form-error" role="alert">{{ oauthManagementError }}</p>
          <div v-if="oauthCreatedClient" class="oauth-created-client" role="status"><strong>{{ oauthCreatedClient.notice || '客户端已登记' }}：{{ oauthCreatedClient.client_id }}</strong><p v-if="oauthCreatedClient.client_secret">Client Secret（只显示本次）：<code>{{ oauthCreatedClient.client_secret }}</code> <button class="secondary-button" @click="copyOAuthClientSecret">复制 Secret</button></p><p v-else>此客户端为公开客户端，不签发 Client Secret。</p></div>
          <ul v-if="oauthClients.length" class="oauth-managed-list"><li v-for="client in oauthClients" :key="client.client_id"><div><strong>{{ client.client_name }}</strong><code>{{ client.client_id }}</code><small v-if="client.registration_type === 'cimd'">Client ID Metadata Document（公开客户端）</small><small v-else>管理员预注册客户端</small><small>{{ client.redirect_uris.join(' · ') }}</small><small v-if="client.disabled_at">已停用</small></div><button v-if="!client.disabled_at && client.confidential" class="text-button" @click="rotateOAuthClientSecret(client)">轮换 Secret</button><button v-if="!client.disabled_at" class="text-button" @click="disableOAuthClient(client)">停用</button></li></ul>
          <h4>已授权记录</h4>
          <ul v-if="oauthGrants.length" class="oauth-managed-list"><li v-for="grant in oauthGrants" :key="grant.id"><div><strong>{{ grant.client_name }} <small>授权 #{{ grant.id }}</small></strong><small>{{ grant.scopes.join(' · ') }} · {{ grant.resource }}</small><small>{{ grant.revoked_at ? '已撤销' : `有效期至 ${grant.expires_at}` }}</small></div><button v-if="!grant.revoked_at" class="text-button" @click="revokeOAuthGrant(grant)">撤销</button></li></ul>
          <p v-else class="mcp-field-help">目前没有 OAuth 授权记录。</p>
        </section>

        <section class="mcp-access-section mcp-security-note" aria-labelledby="mcp-security-heading">
          <h3 id="mcp-security-heading">认证与安全边界</h3>
          <p v-if="mcpCapabilities?.oauth_enabled">OAuth 使用独立的不透明 Bearer Token 和按客户端授权；旧 <code>ACCESS_TOKEN</code> 仍兼容。OAuth 的读取权限覆盖整个单所有者报告库，写入权限可声明 source / bot_id；这不是多租户隔离。管理员共享密钥不会作为 OAuth 凭据发放。</p>
          <p v-else>当前访问模式：{{ mcpCapabilities?.access_model || '由本服务管理员配置；没有工具能力清单时，请先更新服务端。' }}。服务复用管理员设置的 <code>ACCESS_TOKEN</code> 作为共享 Bearer 密钥，仅适合服务所有者本人或受信任的单用户客户端。持有该密钥的 MCP 客户端可发布并检索报告；不要把密钥发给不受信任的人或写入剪贴板。</p>
          <p v-if="!mcpCapabilities?.oauth_enabled"><code>ACCESS_TOKEN</code> 是服务器配置；本实例未启用 OAuth 独立授权。</p>
        </section>

        <section class="mcp-access-section mcp-copy-section" aria-labelledby="mcp-copy-heading">
          <div class="mcp-section-heading"><h3 id="mcp-copy-heading">通用接入说明</h3><button class="secondary-button" :disabled="!mcpCapabilities" @click="copyMCPGenericGuide">复制通用说明</button></div>
          <p class="mcp-field-help">MCP 协议没有统一的客户端配置文件格式。OAuth 已启用时，优先让客户端从服务元数据启动 OAuth 授权；旧 Bearer 可继续供可信客户端使用。</p>
          <ol class="mcp-connection-steps"><li>复制上方 MCP 服务 URL。</li><li v-if="mcpCapabilities?.oauth_enabled">在支持远程 MCP OAuth 的客户端中启动授权，登录 Bolt Dock 并选择所需权限。</li><li v-else>在支持远程 MCP 的客户端中填写 URL，并在客户端安全凭据设置中配置管理员提供的 Bearer Token。</li><li>允许客户端调用所需工具；发布后在 Bot Dock 工作台查看报告。</li></ol>
          <details class="mcp-client-example">
            <summary>可选客户端示例：Codex CLI</summary>
            <p>Codex CLI 0.162.0-alpha.2 已完成连接和工具调用测试。示例使用本地环境变量名 <code>BOLT_DOCK_TOKEN</code>，它只是本示例自行选择的名称，不是 MCP 标准字段，也不是服务器必需的环境变量。请在启动 Codex CLI 的环境中安全设置为服务端 ACCESS_TOKEN 的值。</p>
            <pre class="mcp-client-config">{{ mcpCodexExample }}</pre>
            <button class="secondary-button" @click="copyCodexExample">复制 Codex CLI 示例</button>
          </details>
          <p class="mcp-field-help">其他客户端需要支持自定义远程 MCP、Streamable HTTP 和 Bearer 凭据。未逐一验证的产品与版本均需先确认其支持情况。</p>
          <details class="mcp-client-example mcp-troubleshooting">
            <summary>连接故障排查</summary>
            <ul><li><code>401</code>：检查客户端是否已安全设置正确的服务端 Bearer 密钥。</li><li><code>404</code>：检查反向代理是否将 <code>/mcp</code> 转发到 Bolt Dock。</li><li>协议不兼容：确认客户端支持远程 Streamable HTTP MCP；只支持 stdio 的客户端不能直接连接。</li><li>浏览器型客户端跨域失败：MCP 会拒绝不允许的跨源 Origin；请使用受支持的服务端连接方式并检查 HTTPS 代理转发。</li></ul>
          </details>
          <div class="mcp-copy-actions"><button class="secondary-button" @click="openRESTPushInstructions">改用 REST 推送指令</button></div>
          <p v-if="mcpCopyStatus" class="mcp-copy-status" role="status">{{ mcpCopyStatus }}</p>
          <label v-if="mcpCopyFallback" class="mcp-copy-fallback">浏览器未开放剪贴板权限，内容已选中，请按 ⌘C / Ctrl+C 复制。
            <textarea class="prompt-preview mcp-copy-fallback-field" :value="mcpFallbackText" readonly aria-label="待手动复制的 MCP 内容"></textarea>
          </label>
        </section>
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
