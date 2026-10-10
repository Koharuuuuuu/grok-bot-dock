import { beforeEach, describe, expect, it } from 'vitest'
import createDOMPurify from 'dompurify'
import { JSDOM } from 'jsdom'
import { renderSanitizedMarkdown } from './markdown'
import { buildMCPServiceURL, classifyMCPAddress, highlightParts, loginFailureMessage, makeCodexMCPConfig, makeMCPGenericGuide, makeSearchParams, notificationStatus, readMCPAddressOverride, refreshesSelectedBot, searchSnippet, sessionExpiredMessage, writeMCPAddressOverride } from './ui-model'
import { readImmersivePreference, readReaderFontSize, writeImmersivePreference, writeReaderFontSize } from './reader-preferences'

const dom = new JSDOM('<!doctype html><html><body></body></html>')
const purifier = createDOMPurify(dom.window)

describe('Bolt Dock interface helpers', () => {
  it('renders GFM tables and strips executable report markup', () => {
    const html = renderSanitizedMarkdown('# Report\n\n| Name | Value |\n| --- | --- |\n| <img src=x onerror=alert(1)> | ok |\n\n<script>alert(1)</script><img src=x onerror=alert(1)>', purifier)
    expect(html).toContain('<table>')
    const rendered = new JSDOM(html).window.document
    expect(rendered.querySelector('script')).toBeNull()
    expect(rendered.querySelector('img')).toBeNull()
    expect(rendered.querySelector('[onerror]')).toBeNull()
  })

  it('searches across sources by default and filters only when selected', () => {
    const all = makeSearchParams({ query: '  人工智能  ' })
    expect(all.get('q')).toBe('人工智能')
    expect(all.has('source')).toBe(false)
    const scoped = makeSearchParams({ query: '人工智能', source: 'qwen', cursor: 'abc', from: '2026-10-01', to: '2026-10-09' })
    expect(scoped.get('source')).toBe('qwen')
    expect(scoped.get('cursor')).toBe('abc')
    expect(scoped.get('from')).toBe('2026-10-01')
    expect(scoped.get('to')).toBe('2026-10-09')
  })

  it('builds a neutral MCP endpoint from the page origin or a reverse proxy override', () => {
    expect(buildMCPServiceURL('https://reports.example.com')).toBe('https://reports.example.com/mcp')
    expect(buildMCPServiceURL('https://internal.example/app/', 'https://public.example/dock')).toBe('https://public.example/dock/mcp')
    expect(buildMCPServiceURL('https://reports.example.com', 'https://public.example/mcp')).toBe('https://public.example/mcp')
    expect(buildMCPServiceURL('http://localhost:18765')).toBe('http://localhost:18765/mcp')
    expect(() => buildMCPServiceURL('https://reports.example.com', 'http://public.example')).toThrow('HTTPS')
    expect(() => buildMCPServiceURL('https://reports.example.com', 'https://user:pass@example.com')).toThrow('凭据')
  })

  it('warns when an MCP URL points to localhost or a private network', () => {
    expect(classifyMCPAddress('http://localhost:18765/mcp')).toBe('local')
    expect(classifyMCPAddress('https://192.168.1.5/mcp')).toBe('local')
    expect(classifyMCPAddress('https://[::ffff:192.168.1.5]/mcp')).toBe('local')
    expect(classifyMCPAddress('https://mcp.example.com/mcp')).toBe('public')
  })

  it('creates generic MCP instructions from server capabilities without exposing secrets', () => {
    const guide = makeMCPGenericGuide('https://reports.example.com/mcp', [
      { name: 'publish_report', access: '写入', description: '发布正式报告', example: '{"stable_task_id":"task-identifier"}' },
      { name: 'search_reports', access: '只读', description: '查询历史', example: '{"query":"关键词","limit":10}' },
    ])
    expect(guide).toContain('Streamable HTTP')
    expect(guide).toContain('Authorization: Bearer <由服务管理员安全提供>')
    expect(guide).toContain('接入步骤：')
    expect(guide).toContain('示例参数：{"query":"关键词","limit":10}')
    expect(guide).toContain('publish_report')
    expect(guide).toContain('search_reports')
    expect(guide).not.toContain('BOLT_DOCK_TOKEN')
    expect(guide).not.toContain('ACCESS_TOKEN=')
  })

  it('describes OAuth capabilities without suggesting an administrator secret', () => {
    const guide = makeMCPGenericGuide('https://reports.example.com/mcp', [], true)
    expect(guide).toContain('PKCE S256')
    expect(guide).toContain('reports:read / reports:write')
    expect(guide).toContain('无 refresh token')
    expect(guide).not.toContain('Authorization: Bearer <由服务管理员安全提供>')
  })

  it('keeps the Codex-specific environment variable inside its optional example only', () => {
    const example = makeCodexMCPConfig('https://reports.example.com/mcp')
    expect(example).toContain('url = "https://reports.example.com/mcp"')
    expect(example).toContain('bearer_token_env_var = "BOLT_DOCK_TOKEN"')
    expect(example).not.toContain('ACCESS_TOKEN=')
    expect(example).toContain('Codex CLI')
  })

  it('persists only the optional MCP address override and tolerates blocked storage', () => {
    const values = new Map()
    const storage = { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) }
    expect(readMCPAddressOverride(storage)).toBe('')
    writeMCPAddressOverride('https://public.example', storage)
    expect(readMCPAddressOverride(storage)).toBe('https://public.example')
    writeMCPAddressOverride('', storage)
    expect(readMCPAddressOverride(storage)).toBe('')
    const unavailable = { getItem() { throw new Error('blocked') }, setItem() { throw new Error('blocked') }, removeItem() { throw new Error('blocked') } }
    expect(readMCPAddressOverride(unavailable)).toBe('')
    expect(() => writeMCPAddressOverride('https://public.example', unavailable)).not.toThrow()
  })

  it('marks matching search snippets without injecting HTML', () => {
    expect(highlightParts('AI 今日动态', 'AI')).toEqual([{ text: 'AI', match: true }, { text: ' 今日动态', match: false }])
    expect(highlightParts('<script>', 'script').map(part => part.text).join('')).toBe('<script>')
  })

  it('shows a matching content excerpt when the summary does not contain the query', () => {
    const snippet = searchSnippet({ summary: 'A short generic summary', content: '# Daily report\n\nThe 人工智能 market changed.' }, '人工智能')
    expect(snippet).toContain('人工智能')
    expect(highlightParts(snippet, '人工智能').some(part => part.match)).toBe(true)
  })

  it('refreshes a selected card when its source and bot identity match an SSE report', () => {
    expect(refreshesSelectedBot({ source: 'grok', bot_id: 'daily' }, { source: 'grok', bot_id: 'daily' })).toBe(true)
    expect(refreshesSelectedBot({ source: 'grok', bot_id: 'daily' }, { source: 'codex', bot_id: 'daily' })).toBe(false)
    expect(refreshesSelectedBot(null, { source: 'grok', bot_id: 'daily' })).toBe(false)
  })

  it('degrades notifications when unsupported or permission is denied', () => {
    expect(notificationStatus(false, 'default')).toBe('当前浏览器不支持通知')
    expect(notificationStatus(true, 'denied')).toBe('通知未获授权')
    expect(notificationStatus(true, 'granted')).toBe('通知已开启')
  })

  it('keeps failed-login and expired-session messages independent of credentials and response bodies', () => {
    const submittedToken = 'sensitive-token-marker'
    const responseBody = { error: { message: `invalid credential ${submittedToken}` } }
    const messages = [loginFailureMessage(401), loginFailureMessage(500), sessionExpiredMessage]
    expect(messages[0]).toBe('访问密钥无效，请重试。')
    expect(messages[1]).toBe('登录失败 (500)')
    expect(messages[2]).toBe('登录会话已失效，请重新登录。')
    expect(JSON.stringify(messages)).not.toContain(submittedToken)
    expect(JSON.stringify(messages)).not.toContain(responseBody.error.message)
  })

  it('persists versioned global reading preferences and falls back on invalid values', () => {
    const values = new Map()
    const storage = { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value) }
    expect(readImmersivePreference(storage)).toBe(false)
    writeImmersivePreference(true, storage)
    expect(readImmersivePreference(storage)).toBe(true)
    writeReaderFontSize(24, storage)
    expect(readReaderFontSize(storage)).toBe(24)
    writeReaderFontSize(25, storage)
    expect(readReaderFontSize(storage)).toBe(16)
    values.set('bolt-dock:reader-font-size:v1', '{broken')
    expect(readReaderFontSize(storage)).toBe(16)
    values.set('bolt-dock:reader-preferences:v1', JSON.stringify({ version: 2, immersive: true }))
    expect(readImmersivePreference(storage)).toBe(false)
  })

  it('keeps working when browser storage is present but unavailable', () => {
    const unavailable = { getItem() { throw new Error('storage blocked') }, setItem() { throw new Error('storage blocked') } }
    expect(readImmersivePreference(unavailable)).toBe(false)
    expect(readReaderFontSize(unavailable)).toBe(16)
    expect(() => writeImmersivePreference(true, unavailable)).not.toThrow()
    expect(() => writeReaderFontSize(20, unavailable)).not.toThrow()
  })
})
