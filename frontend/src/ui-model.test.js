import { beforeEach, describe, expect, it } from 'vitest'
import createDOMPurify from 'dompurify'
import { JSDOM } from 'jsdom'
import { renderSanitizedMarkdown } from './markdown'
import { highlightParts, makeSearchParams, notificationStatus, refreshesSelectedBot, searchSnippet } from './ui-model'

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
})
