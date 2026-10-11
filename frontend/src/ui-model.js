export function makeSearchParams({ query, source = '', cursor = '', from = '', to = '' }) {
  const params = new URLSearchParams({ q: query.trim(), limit: '30' })
  if (source) params.set('source', source)
  if (cursor) params.set('cursor', cursor)
  if (from) params.set('from', from)
  if (to) params.set('to', to)
  return params
}

const MCP_ADDRESS_OVERRIDE_KEY = 'bolt-dock:mcp-address-override:v1'

function hostnameIsPrivate(hostname) {
  const host = hostname.toLowerCase().replace(/^\[|\]$/g, '')
  const mappedIPv4 = host.match(/^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/)
  if (mappedIPv4) {
    const upper = Number.parseInt(mappedIPv4[1], 16)
    const lower = Number.parseInt(mappedIPv4[2], 16)
    return hostnameIsPrivate(`${upper >> 8}.${upper & 255}.${lower >> 8}.${lower & 255}`)
  }
  if (host === 'localhost' || host.endsWith('.localhost') || host.endsWith('.local') || host.endsWith('.internal') || host.endsWith('.test')) return true
  if (!host.includes('.') && !host.includes(':')) return true
  if (host === '::' || host === '::1' || /^(fc|fd|fe[89ab])/i.test(host)) return true
  const octets = host.split('.').map(Number)
  if (octets.length !== 4 || octets.some(value => !Number.isInteger(value) || value < 0 || value > 255)) return false
  const [a, b] = octets
  return a === 0 || a === 10 || a === 127 || a >= 224 ||
    (a === 100 && b >= 64 && b <= 127) ||
    (a === 169 && b === 254) ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && b === 168) ||
    (a === 198 && (b === 18 || b === 19))
}

export function normalizeMCPAddressBase(value) {
  let parsed
  try { parsed = new URL(String(value || '').trim()) }
  catch { throw new Error('请输入有效的 MCP 服务地址。') }
  if (!['http:', 'https:'].includes(parsed.protocol)) throw new Error('MCP 服务地址必须使用 HTTP 或 HTTPS。')
  if (parsed.username || parsed.password) throw new Error('服务地址不能包含凭据。')
  if (parsed.search || parsed.hash) throw new Error('服务地址不能包含查询参数或片段。')
  const privateHost = hostnameIsPrivate(parsed.hostname)
  if (parsed.protocol !== 'https:' && !privateHost) throw new Error('远程 MCP 服务地址必须使用 HTTPS。')
  parsed.pathname = parsed.pathname.replace(/\/+$/, '').replace(/\/mcp$/i, '')
  return parsed.toString().replace(/\/$/, '')
}

export function buildMCPServiceURL(pageOrigin, override = '') {
  const base = normalizeMCPAddressBase(override || pageOrigin)
  const parsed = new URL(base)
  parsed.pathname = `${parsed.pathname.replace(/\/+$/, '')}/mcp`.replace(/^\/\//, '/')
  return parsed.toString()
}

export function classifyMCPAddress(value) {
  try {
    const url = new URL(value)
    return hostnameIsPrivate(url.hostname) || url.protocol !== 'https:' ? 'local' : 'public'
  } catch { return 'invalid' }
}

export function readMCPAddressOverride(storage) {
  try { return (storage ?? globalThis.localStorage)?.getItem(MCP_ADDRESS_OVERRIDE_KEY) || '' } catch { return '' }
}

export function writeMCPAddressOverride(value, storage) {
  try {
    const target = storage ?? globalThis.localStorage
    if (value) target?.setItem(MCP_ADDRESS_OVERRIDE_KEY, value)
    else target?.removeItem(MCP_ADDRESS_OVERRIDE_KEY)
  } catch {}
}

export function makeMCPGenericGuide(endpoint, tools = [], oauthEnabled = false) {
  const toolList = tools.map(tool => `- ${tool.name}（${tool.access}）：${tool.description}\n  示例参数：${tool.example}`).join('\n')
  const auth = oauthEnabled
    ? '认证方式：OAuth Authorization Code + PKCE S256（元数据发现、浏览器登录与所有者同意）；旧 Bearer 仅供可信客户端继续使用。OAuth access token 有效 15 分钟，无 refresh token，read/write 权限分开授权。'
    : '认证方式：HTTP Authorization 请求头，Bearer Token\n请求头示例：Authorization: Bearer <由服务管理员安全提供>'
  const steps = oauthEnabled
    ? '2. 在支持远程 MCP OAuth 的客户端中启动授权；登录 Bolt Dock 并主动选择 reports:read / reports:write 权限。\n3. 若客户端不支持 PKCE S256 或要求 refresh token，该客户端当前不可用。'
    : '2. 在支持自定义远程 MCP 的客户端中填写 URL，并在客户端安全凭据设置中配置由管理员安全提供的 Bearer Token。\n3. 允许客户端调用 publish_report；完成发布后，在 Bot Dock 工作台查看报告。'
  const safety = oauthEnabled
    ? 'OAuth 仅支持所有者预注册客户端，读取权限覆盖整个单所有者报告库；这不构成多租户隔离。未在真实 Grok、Claude、Cursor 等平台验证的连接不能视为兼容。'
    : '当前服务复用管理员 ACCESS_TOKEN 作为共享密钥，仅适用于服务所有者本人或受信任的单用户客户端；不要公开分享密钥。'
  return `Bot Dock MCP 接入说明\n\n服务地址：${endpoint}\n传输协议：Streamable HTTP\n${auth}\n\n接入步骤：\n1. 复制上方 MCP 服务 URL。\n${steps}\n\n${safety} MCP 不定义通用客户端配置文件格式，请按客户端的远程 MCP 配置方式填写。\n\n可用工具与示例参数：\n${toolList}\n\nAgent 能发现并调用工具不代表它会自动推送报告；行为取决于客户端能力、授权和任务设置。`
}

export function makeCodexMCPConfig(endpoint) {
  return `# Optional example for the tested Codex CLI 0.162.0-alpha.2 client.\n# BOLT_DOCK_TOKEN is a local variable name chosen for this example; set its value securely.\n[mcp_servers.bolt_dock]\nurl = "${endpoint}"\nbearer_token_env_var = "BOLT_DOCK_TOKEN"\n`
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

export function loginFailureMessage(status) {
  return status === 401 ? '访问密钥无效，请重试。' : `登录失败 (${status})`
}

export const sessionExpiredMessage = '登录会话已失效，请重新登录。'

export function refreshesSelectedBot(selected, report) {
  return Boolean(selected && selected.source === report.source && selected.bot_id === report.bot_id)
}
