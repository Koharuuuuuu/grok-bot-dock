<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import TopicReader from './TopicReader.vue'
import { filterTopics, makeAgentOnboarding, makeStopInstruction, topicTabs } from '../topic-model'
import { buildMCPServiceURL, readMCPAddressOverride } from '../ui-model'

const props = defineProps({ refreshKey: Number, themeMode: String })
const emit = defineEmits(['expired', 'connect', 'rest', 'legacy', 'notifications', 'logout', 'theme', 'share', 'delete'])
const topics = ref([]), tab = ref('all'), group = ref(''), query = ref(''), selectedID = ref(''), report = ref(null)
const reports = ref([]), cursor = ref(''), searchMatches = ref([]), searchCursor = ref(''), searchFrom = ref(''), searchTo = ref('')
const matchedTopicIDs = ref([]), dialogRevision = ref(0), dialogTopicID = ref('')
const selectedIDs = ref([]), listLimit = ref(30), busy = ref(false), error = ref(''), notice = ref('')
const dialog = ref(''), actionDialog = ref(null), evidence = ref(''), draftName = ref(''), draftGroup = ref('')
const guide = ref(''), copied = ref(false), copyFallback = ref(false), copyField = ref(null), capabilities = ref(null)
let searchTimer, searchAbort, searchVersion = 0, reportVersion = 0, navigationVersion = 0, active = true, lastFocus
const selected = computed(() => topics.value.find(t => t.id === selectedID.value))
const groups = computed(() => [...new Set(topics.value.map(t => t.group).filter(Boolean))].sort((a,b) => a.localeCompare(b, 'zh-CN')))
const counts = computed(() => ({ all: topics.value.filter(t => t.status !== 'archived').length, follow: topics.value.filter(t => t.followed && t.status !== 'archived').length, updates: topics.value.filter(t => t.unread_count > 0 && t.status !== 'archived').length, archived: topics.value.filter(t => t.status === 'archived').length }))
const visible = computed(() => filterTopics(topics.value, { tab: tab.value, group: group.value, query: query.value, reportTopicIDs: matchedTopicIDs.value }))
const pinned = computed(() => topics.value.filter(t => t.pinned && t.status !== 'archived'))
const endpoint = computed(() => buildMCPServiceURL(window.location.origin, readMCPAddressOverride()))
const statuses = { active: '接收更新', stop_requested: '待停止确认', archived: '已归档' }

async function api(path, options = {}) {
  const response = await fetch(path, { ...options, credentials: 'same-origin', headers: { ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...options.headers } })
  if (response.status === 401) { emit('expired'); throw new Error('登录会话已失效，请重新登录。') }
  if (!response.ok) { let message = `请求失败 (${response.status})`; try { message = (await response.json()).error?.message || message } catch {} throw new Error(message) }
  return response.status === 204 ? null : response.json()
}
async function loadTopics() {
  const data = await api('/api/v1/topics')
  if (!active) return
  topics.value = data.items
  selectedIDs.value = selectedIDs.value.filter(id => topics.value.some(t => t.id === id))
  if (selectedID.value && !selected.value) { selectedID.value = ''; report.value = null; navigate('/') }
}
function navigate(path) { if (window.location.pathname !== path) window.history.pushState({}, '', path) }
function showTab(key) { ++navigationVersion; ++reportVersion; tab.value = key; selectedID.value = ''; report.value = null; group.value = ''; selectedIDs.value = []; listLimit.value = 30; searchMatches.value = []; searchCursor.value = ''; matchedTopicIDs.value=[]; navigate('/') }
function showGroup(value) { showTab('all'); group.value = value }
async function loadReports(append = false) {
  const id = selectedID.value
  if (!id) return
  const version = ++reportVersion
  const params = new URLSearchParams({ limit: '20', q: query.value.trim() })
  if (append && cursor.value) params.set('cursor', cursor.value)
  const data = await api(`/api/v1/topics/${encodeURIComponent(id)}/reports?${params}`)
  if (!active || version !== reportVersion || id !== selectedID.value) return
  reports.value = append ? [...reports.value, ...data.items] : data.items
  cursor.value = data.next_cursor || ''
  if (report.value) {
    const reportID = report.value.id
    try { const refreshed = await api(`/api/v1/reports/${reportID}`); if (active && version === reportVersion && report.value?.id === reportID) report.value = refreshed } catch (e) { if (version === reportVersion) { report.value = null; error.value = e.message } }
  }
}
async function openTopic(topic) {
  ++navigationVersion
  error.value = ''; selectedID.value = topic.id; report.value = null; reports.value = []; cursor.value = ''; query.value = ''
  navigate(`/topics/${encodeURIComponent(topic.id)}`)
  try { await loadReports() } catch (e) { error.value = e.message }
}
async function openReport(item) {
  const version = ++navigationVersion
  error.value = ''
  try {
    const full = await api(`/api/v1/reports/${item.id}`)
    if (!active || version !== navigationVersion) return
    selectedID.value = full.topic_id; report.value = full; navigate(`/reports/${full.id}`)
    await api(`/api/v1/topics/${encodeURIComponent(full.topic_id)}/read`, { method: 'POST', body: JSON.stringify({ report_id: full.id }) })
    await loadTopics()
  } catch (e) { error.value = e.message }
}
function backToTopic() { report.value = null; navigate(`/topics/${encodeURIComponent(selectedID.value)}`); loadReports().catch(e => { error.value = e.message }) }
async function handleRoute() {
  const path = window.location.pathname
  if (/^\/reports\/\d+$/.test(path)) await openReport({ id: Number(path.split('/')[2]) })
  else if (/^\/topics\/[^/]+$/.test(path)) {
    const id = decodeURIComponent(path.split('/')[2]), topic = topics.value.find(t => t.id === id)
    if (topic) await openTopic(topic); else error.value = '主题不存在或已被移除。'
  } else { ++navigationVersion; ++reportVersion; selectedID.value = ''; report.value = null }
}
async function patch(topic, values) {
  await api(`/api/v1/topics/${encodeURIComponent(topic.id)}`, { method: 'PATCH', body: JSON.stringify(values) })
}
async function change(topic, values) {
  if (busy.value) return
  busy.value = true; error.value = ''; notice.value = ''
  try { await patch(topic, values); await loadTopics() } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function readAll() {
  try { await api(`/api/v1/topics/${selectedID.value}/read`, { method: 'POST', body: JSON.stringify({ report_id: 0 }) }); await loadTopics() } catch (e) { error.value = e.message }
}
async function batch(values) {
  if (busy.value) return
  busy.value = true; error.value = ''; let succeeded = 0; const failed = []
  for (const id of selectedIDs.value) {
    const topic = topics.value.find(t => t.id === id)
    try { await patch(topic, values); succeeded++ } catch { failed.push(id) }
  }
  selectedIDs.value = failed
  try { await loadTopics(); notice.value = `已更新 ${succeeded} 个主题。${failed.length ? `${failed.length} 个失败，已保留选中，可重试。` : ''}` } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function search(append = false) {
  clearTimeout(searchTimer); searchAbort?.abort()
  const term = query.value.trim(), version = ++searchVersion
  if (selected.value) { try { await loadReports() } catch (e) { error.value = e.message } return }
  if (!term) { searchMatches.value = []; searchCursor.value = ''; matchedTopicIDs.value=[]; return }
  if (term.length > 256) { error.value = '搜索关键词最多 256 字符。'; return }
  searchAbort = new AbortController()
  try {
    const params = new URLSearchParams({ q: term, limit: '20' })
    params.set('topic_tab', tab.value)
    if (group.value) params.set('topic_group', group.value)
    if (searchFrom.value) params.set('from', searchFrom.value)
    if (searchTo.value) params.set('to', searchTo.value)
    if (append && searchCursor.value) params.set('cursor', searchCursor.value)
    const data = await api(`/api/v1/search?${params}`, { signal: searchAbort.signal })
    if (version !== searchVersion || !active) return
    searchMatches.value = append ? [...searchMatches.value, ...data.items] : data.items
    matchedTopicIDs.value = data.topic_ids || data.items.map(item => item.topic_id)
    searchCursor.value = data.next_cursor || ''
  } catch (e) { if (e.name !== 'AbortError' && version === searchVersion) error.value = e.message }
}
watch([query,tab,group], () => { selectedIDs.value = []; clearTimeout(searchTimer); searchAbort?.abort(); ++searchVersion; searchMatches.value = []; searchCursor.value = ''; matchedTopicIDs.value=[]; searchTimer = setTimeout(() => search(), 250) })
watch(() => props.refreshKey, async () => {
  try { await loadTopics(); if (selected.value) await loadReports(); else if (query.value.trim()) await search() } catch (e) { error.value = e.message }
})
function openDialog(kind) {
  error.value = ''; copied.value = false; copyFallback.value = false; evidence.value = ''
  draftName.value = selected.value?.name || ''; draftGroup.value = selected.value?.group || ''
  dialogRevision.value = selected.value?.revision || 0; dialogTopicID.value=selectedID.value
  lastFocus = document.activeElement; dialog.value = kind
  nextTick(() => { actionDialog.value?.showModal(); actionDialog.value?.querySelector('button,input,textarea')?.focus() })
}
async function connect() {
  try { capabilities.value = await api('/api/v1/mcp/capabilities'); guide.value = makeAgentOnboarding(endpoint.value, capabilities.value.tools, capabilities.value.oauth_enabled); openDialog('connect'); await nextTick(); await copy(guide.value) }
  catch (e) { error.value = e.message }
}
function closeDialog() { actionDialog.value?.close(); dialog.value = ''; nextTick(() => lastFocus?.isConnected && lastFocus.focus()) }
async function copy(text) {
  copied.value = false; copyFallback.value = false
  try { await navigator.clipboard.writeText(text); copied.value = true }
  catch { guide.value = text; copyFallback.value = true; await nextTick(); copyField.value?.focus(); copyField.value?.select() }
}
async function submitDialog() {
  if (!selected.value || busy.value) return
  let values
  if (dialog.value === 'rename') values = { name: draftName.value }
  else if (dialog.value === 'group') values = { group: draftGroup.value }
  else if (dialog.value === 'stop') values = { action: 'request_stop' }
  else if (dialog.value === 'confirm') values = { action: 'confirm_stopped', evidence: evidence.value, revision: dialogRevision.value }
  else if (dialog.value === 'restore') values = { action: 'restore' }
  else return
  busy.value = true; error.value = ''
  try {
    await patch({id:dialogTopicID.value}, values); await loadTopics(); closeDialog()
    notice.value = values.action === 'request_stop' ? '已记录停止请求。请通知外部 Agent 停止，确认后才会归档。' : values.action === 'restore' ? '主题已恢复；请在外部平台另行恢复任务调度。' : '主题已更新。'
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
function stopGuide() { guide.value = makeStopInstruction(selected.value); openDialog('stop-guide') }
function selectAll(event) { selectedIDs.value = event.target.checked ? visible.value.map(t => t.id) : [] }
function date(value) { return value ? new Date(value).toLocaleString() : '暂无内容' }
onMounted(async () => { window.addEventListener('popstate', handleRoute); try { await loadTopics(); await handleRoute() } catch (e) { error.value = e.message } })
onUnmounted(() => { active = false; ++reportVersion; ++searchVersion; clearTimeout(searchTimer); searchAbort?.abort(); window.removeEventListener('popstate', handleRoute) })
</script>

<template>
  <div class="topic-space">
    <aside class="ts-sidebar">
      <a href="/" class="ts-brand" @click.prevent="showTab('all')"><img :src="'/brand/bolt-dock.svg'" alt="">Bolt Dock</a>
      <div class="ts-caption">主题空间</div>
      <nav aria-label="主题导航"><button v-for="item in topicTabs" :key="item.key" :class="{ active: tab === item.key && !selected }" @click="showTab(item.key)">{{ item.label }}<span>{{ counts[item.key] }}</span></button></nav>
      <div class="ts-caption">我的分组</div>
      <nav aria-label="分组导航"><button v-for="name in groups" :key="name" :class="{ active: group === name }" @click="showGroup(name)">{{ name }}</button><button @click="showGroup('__ungrouped')">未分组</button></nav>
    </aside>
    <main class="ts-main">
      <header class="ts-top"><span>个人空间 / {{ selected ? selected.name : '主题空间' }}</span><div><button class="ts-button" @click="$emit('theme')">{{ themeMode === 'dark' ? '浅色' : '深色' }}</button><button class="ts-button ts-connect-button" @click="connect">复制接入指令</button><button class="ts-text" @click="openDialog('settings')">设置</button></div></header>
      <div v-if="error && !dialog" class="ts-error" role="alert">{{ error }}<button @click="error = ''" aria-label="关闭错误">×</button></div>
      <template v-if="!selected">
        <div class="ts-heading"><div><h1>{{ group && group !== '__ungrouped' ? group : '你的主题空间' }}</h1><p>想法交给 Agent，成果在这里持续积累。</p></div><span class="ts-meta">{{ topics.length }} 个主题</span></div>
        <div v-if="!topics.length" class="ts-welcome"><h2>从一个想法开始</h2><p>在 Grok 等外部平台下发任务，首次正式报告推送后，主题会自动出现。</p></div>
        <div v-if="tab === 'follow' && pinned.length && !query && !group" class="ts-pins"><button v-for="topic in pinned" :key="topic.id" @click="openTopic(topic)"><span class="ts-meta">{{ topic.group || '未分组' }} · 置顶</span><h3>{{ topic.name }}</h3><p>{{ topic.summary }}</p><span v-if="topic.unread_count" class="ts-accent">{{ topic.unread_count }} 条未读</span></button></div>
        <nav class="ts-tabs" aria-label="列表筛选"><button v-for="item in topicTabs" :key="item.key" :class="{ active: tab === item.key }" @click="showTab(item.key)">{{ item.label }} <span>{{ counts[item.key] }}</span></button></nav>
        <form class="ts-search-tools" @submit.prevent="search()"><input v-model="query" aria-label="搜索主题和内容" placeholder="搜索主题、关注点或报告内容…"><select v-model="group" aria-label="按分组筛选"><option value="">所有分组</option><option value="__ungrouped">未分组</option><option v-for="name in groups" :key="name">{{ name }}</option></select><button class="ts-button">搜索</button></form>
        <div v-if="query" class="ts-date-tools"><label>从 <input v-model="searchFrom" type="date" @change="search()"></label><label>至 <input v-model="searchTo" type="date" @change="search()"></label></div>
        <div v-if="selectedIDs.length" class="ts-batch"><span>已选 {{ selectedIDs.length }} 个</span><input v-model="draftGroup" aria-label="批量分组名称" placeholder="分组名；留空为未分组" maxlength="128" list="topic-groups"><button class="ts-button" :disabled="busy" @click="batch({ group: draftGroup })">归组</button><button class="ts-button" :disabled="busy" @click="batch({ followed: true })">关注</button><button class="ts-button" :disabled="busy" @click="batch({ followed: false })">取消关注</button><button class="ts-button" :disabled="busy" @click="batch({ action: tab === 'archived' ? 'restore' : 'request_stop' })">{{ tab === 'archived' ? '恢复主题' : '请求停止' }}</button><button class="ts-text" @click="selectedIDs = []">取消选择</button></div>
        <datalist id="topic-groups"><option v-for="name in groups" :key="name" :value="name"></option></datalist>
        <div class="ts-table-heading"><input type="checkbox" aria-label="选择当前筛选全部主题" :checked="visible.length > 0 && visible.every(t => selectedIDs.includes(t.id))" @change="selectAll"><span>主题 / 最近内容</span><span>分组</span><span>最近更新</span><span></span></div>
        <div v-for="topic in visible.slice(0, listLimit)" :key="topic.id" class="ts-topic-row"><input v-model="selectedIDs" type="checkbox" :value="topic.id" :aria-label="`选择${topic.name}`"><button class="ts-topic-name" @click="openTopic(topic)"><strong>{{ topic.name }}</strong><span v-if="topic.status === 'stop_requested'" class="ts-stop">待停止</span><p>{{ topic.summary || '尚无报告' }}</p></button><span class="ts-group-cell">{{ topic.group || '未分组' }}</span><span class="ts-meta">{{ date(topic.last_report_at) }}<b v-if="topic.unread_count" class="ts-accent">{{ topic.unread_count }} 条未读</b><span v-else>{{ topic.report_count }} 份内容</span></span><button class="ts-follow" :aria-label="`${topic.followed ? '取消关注' : '关注'}${topic.name}`" :aria-pressed="topic.followed" :disabled="busy" @click="change(topic, { followed: !topic.followed })">{{ topic.followed ? '★' : '☆' }}</button></div>
        <p v-if="!visible.length && topics.length" class="ts-empty">当前条件下没有主题。可以切换“全部主题”或清除搜索。</p>
        <button v-if="visible.length > listLimit" class="ts-button" @click="listLimit += 30">展开更多主题</button>
        <section v-if="query && searchMatches.length" class="ts-search-results"><h2>报告内容匹配</h2><button v-for="item in searchMatches" :key="item.id" class="ts-report-result" @click="openReport(item)"><strong>{{ item.title }}</strong><span class="ts-meta">{{ item.source }} · {{ date(item.executed_at) }}</span><p>{{ item.summary }}</p></button><button v-if="searchCursor" class="ts-button" @click="search(true)">加载更多匹配内容</button></section>
      </template>
      <template v-else>
        <button class="ts-text" @click="showTab(tab)">← 返回主题空间</button>
        <header class="ts-topic-header"><span class="ts-meta">{{ selected.group || '未分组' }} · {{ statuses[selected.status] }}</span><h1>{{ selected.name }}</h1><p>来自 Agent 的正式成果，在同一主题内持续积累。</p><div class="ts-actions"><button class="ts-button" :disabled="busy" @click="change(selected, { followed: !selected.followed })">{{ selected.followed ? '已关注' : '关注主题' }}</button><button class="ts-button" :disabled="busy" @click="change(selected, { pinned: !selected.pinned })">{{ selected.pinned ? '取消置顶' : '置顶' }}</button><button class="ts-button" @click="openDialog('group')">归组</button><button class="ts-button" @click="openDialog('rename')">重命名</button><button v-if="selected.status === 'active'" class="ts-button" @click="openDialog('stop')">停止跟踪并归档…</button><button v-else class="ts-button" @click="openDialog('restore')">{{ selected.status === 'archived' ? '恢复主题…' : '取消停止请求…' }}</button></div></header>
        <div v-if="selected.status === 'stop_requested'" class="ts-stop-panel"><strong>等待外部任务停止确认</strong><p>本服务不会直接关闭 Grok 等平台的调度。请将停止指令交给相应 Agent，或在外部停掉后确认。</p><div class="ts-actions"><button class="ts-button" @click="stopGuide">复制停止指令</button><button class="ts-button" @click="openDialog('confirm')">我已在外部停止，确认归档…</button></div><p v-for="agent in selected.agents" :key="`${agent.source}:${agent.bot_id}`" class="ts-meta">{{ agent.source }} / {{ agent.bot_id }}：{{ agent.stopped ? '已确认停止' : '等待确认' }}</p></div>
        <div v-if="selected.status === 'archived'" class="ts-stop-panel"><strong>已归档 · 停止确认已记录</strong><p>{{ selected.stop_evidence }}</p><p>历史内容保留；恢复主题不会自动重启外部任务。</p></div>
        <TopicReader v-if="report" :report="report" @back="backToTopic" @share="$emit('share', $event)" @delete="$emit('delete', $event)" />
        <div v-else class="ts-topic-layout"><section><div class="ts-heading"><h2>内容记录 {{ selected.report_count }}</h2><button class="ts-text" @click="readAll">全部标为已读</button></div><form class="ts-search-tools" @submit.prevent="search()"><input v-model="query" placeholder="搜索这个主题的内容…" aria-label="搜索主题内容"><button class="ts-button">搜索</button></form><article v-for="item in reports" :key="item.id" class="ts-report-item"><span class="ts-meta">{{ date(item.executed_at) }} · {{ item.source }} · {{ item.display_name || item.bot_name }}</span><button class="ts-text ts-report-title" @click="openReport(item)"><h3>{{ item.title || '正式报告' }}</h3></button><p>{{ item.summary }}</p><button class="ts-text" @click="openReport(item)">阅读完整内容 →</button></article><button v-if="cursor" class="ts-button" @click="loadReports(true).catch(e => error = e.message)">加载更早内容</button><p v-if="!reports.length" class="ts-empty">暂无匹配内容。</p></section><aside class="ts-inspector"><h3>参与的 Agent</h3><p v-for="agent in selected.agents" :key="`${agent.source}:${agent.bot_id}`">{{ agent.source }}<small>{{ agent.bot_id }}</small></p><h3>推送方式</h3><p>由外部 Agent 执行和发布。关注、归组不会调用模型。</p><h3>主题延续</h3><p>主题标识稳定复用，不根据相似名称自动合并。</p></aside></div>
      </template>
      <p v-if="notice" class="ts-notice" role="status">{{ notice }}</p>
      <footer class="ts-footer">Bolt Dock · 私有主题空间 <span>数据保存在你的服务器</span></footer>
      <nav class="ts-mobile-nav" aria-label="主题导航"><button v-for="item in topicTabs" :key="item.key" :class="{ active: tab === item.key }" :aria-current="tab === item.key ? 'page' : undefined" @click="showTab(item.key)"><span>{{ item.label }}</span><small>{{ counts[item.key] }}</small></button></nav>
    </main>
    <dialog v-if="dialog" ref="actionDialog" class="ts-dialog" @cancel.prevent="closeDialog">
      <header><h2>{{ { connect: '复制一条指令，让 Agent 接入', settings: '接入与设置', stop: '停止跟踪并归档', confirm: '确认外部任务已停止', restore: '恢复主题', rename: '重命名主题', group: '主题归组', 'stop-guide': '停止跟踪指令' }[dialog] }}</h2><button class="ts-text" aria-label="关闭对话框" @click="closeDialog">×</button></header>
      <p v-if="error" class="ts-error" role="alert">{{ error }}</p>
      <template v-if="dialog === 'connect' || dialog === 'stop-guide'"><p>{{ dialog === 'connect' ? '把下面这段话交给外部 Agent。它会识别平台能力；需要授权或人工配置时会提示你。' : '请交给该主题的贡献任务；实际停止外部调度后，Agent 才能确认。' }}</p><textarea ref="copyField" :value="guide" readonly rows="12" aria-label="可复制的 Agent 指令"></textarea><button class="ts-button ts-primary" @click="copy(guide)">{{ copied ? '已复制' : '复制完整指令' }}</button><p v-if="copyFallback" role="status">剪贴板不可用，文本已选中，请手动复制。</p><p class="ts-meta">不包含管理员密钥。接入能力取决于外部平台，复制不等于已经连接。</p><button v-if="dialog === 'connect'" class="ts-text" @click="closeDialog(); $emit('connect')">高级 MCP / OAuth 配置</button></template>
      <template v-else-if="dialog === 'settings'"><div class="ts-settings"><button class="ts-button" @click="closeDialog(); $emit('connect')">MCP 与 OAuth 管理</button><button class="ts-button" @click="closeDialog(); $emit('rest')">REST 推送指令</button><button class="ts-button" @click="$emit('notifications')">启用桌面通知</button><button class="ts-button" @click="closeDialog(); $emit('legacy')">来源与旧版任务管理</button><button class="ts-button" @click="closeDialog(); $emit('logout')">退出登录</button></div></template>
      <form v-else @submit.prevent="submitDialog">
        <template v-if="dialog === 'rename'"><label>主题名称<input v-model="draftName" required maxlength="256"></label></template>
        <template v-else-if="dialog === 'group'"><label>分组名称<input v-model="draftGroup" maxlength="128" list="topic-groups" placeholder="留空为未分组"></label></template>
        <template v-else-if="dialog === 'stop'"><p>先记录停止请求，再由参与的 Agent 确认停止外部任务。确认前显示“待停止”；历史报告始终保留。</p></template>
        <template v-else-if="dialog === 'confirm'"><p>请先在所有相关平台停止这个主题的任务。此操作记录你的确认，不会替你操作外部平台。</p><label>停止确认说明<textarea v-model="evidence" required maxlength="1024" placeholder="例如：已在 Grok 任务页面停用该周期任务。"></textarea></label></template>
        <template v-else-if="dialog === 'restore'"><p>保留原主题和全部历史。主题恢复后，请在外部平台另行恢复任务；本服务不会声称外部调度已启动。</p></template>
        <footer><button class="ts-button" type="button" @click="closeDialog">取消</button><button class="ts-button ts-primary" :disabled="busy">{{ busy ? '处理中…' : dialog === 'stop' ? '记录停止请求' : '确认' }}</button></footer>
      </form>
    </dialog>
  </div>
</template>
