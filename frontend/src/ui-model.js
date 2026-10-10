export function makeSearchParams({ query, source = '', cursor = '', from = '', to = '' }) {
  const params = new URLSearchParams({ q: query.trim(), limit: '30' })
  if (source) params.set('source', source)
  if (cursor) params.set('cursor', cursor)
  if (from) params.set('from', from)
  if (to) params.set('to', to)
  return params
}

export function makeMCPConfig(origin) {
  const base = String(origin || '').trim().replace(/\/+$/, '')
  const endpoint = `${base}/mcp`
  return `# Store the same secret value used by Bolt Dock's ACCESS_TOKEN in BOLT_DOCK_TOKEN.\n# Set BOLT_DOCK_TOKEN in the environment that launches Codex.\n[mcp_servers.bolt_dock]\nurl = "${endpoint}"\nbearer_token_env_var = "BOLT_DOCK_TOKEN"\n`
}

export function highlightParts(text, query) {
  if (!query) return [{ text, match: false }]
  const lowerText = text.toLocaleLowerCase()
  const lowerQuery = query.toLocaleLowerCase()
  const parts = []
  let offset = 0
  while (offset < text.length) {
    const index = lowerText.indexOf(lowerQuery, offset)
    if (index === -1) { parts.push({ text: text.slice(offset), match: false }); break }
    if (index > offset) parts.push({ text: text.slice(offset, index), match: false })
    parts.push({ text: text.slice(index, index + query.length), match: true })
    offset = index + query.length
  }
  return parts
}

export function searchSnippet(report, query, maxLength = 180) {
  maxLength = Math.max(maxLength, query.length + 40)
  const term = query.toLocaleLowerCase()
  const summary = report.summary || ''
  const summaryIncludesTerm = summary.toLocaleLowerCase().includes(term)
  const raw = summaryIncludesTerm ? summary : report.content || summary
  const text = raw
    .replace(/!?\[([^\]]*)\]\([^)]+\)/g, '$1')
    .replace(/[`*_>#~]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
  const index = text.toLocaleLowerCase().indexOf(term)
  if (index < 0) return text.slice(0, maxLength)
  const start = Math.max(0, index - Math.floor((maxLength - query.length) / 2))
  const end = Math.min(text.length, start + maxLength)
  return `${start ? '…' : ''}${text.slice(start, end)}${end < text.length ? '…' : ''}`
}

export function notificationStatus(supported, permission) {
  if (!supported) return '当前浏览器不支持通知'
  if (permission === 'granted') return '通知已开启'
  if (permission === 'denied') return '通知未获授权'
  return '通知不可用'
}

export function refreshesSelectedBot(selected, report) {
  return Boolean(selected && selected.source === report.source && selected.bot_id === report.bot_id)
}
