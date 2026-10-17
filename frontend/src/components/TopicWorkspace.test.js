import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import TopicWorkspace from './TopicWorkspace.vue'

let topic, topicList, reports, requests, wrapper
function response(data, status = 200) { return { ok: status < 400, status, json: async () => structuredClone(data) } }
beforeEach(() => {
  window.history.replaceState({}, '', '/')
  HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', '') }
  HTMLDialogElement.prototype.close = function () { this.removeAttribute('open') }
  topic = { id: 'airport', name: '机场情报监控', group: '', followed: false, pinned: false, status: 'active', revision: 0, report_count: 1, unread_count: 1, last_report_at: '2026-10-17T09:00:00Z', summary: '正式报告摘要', agents: [{ source: 'grok', bot_id: 'task-airport', stopped: false }] }
  topicList = [topic]
  reports = [{ id: 1, topic_id: 'airport', source: 'grok', bot_name: '机场任务', title: '候选情报', summary: '正式报告摘要', executed_at: '2026-10-17T09:00:00Z', content: '# 候选情报\n\n## 原始证据\n\n完整报告。' }]
  requests = []
  vi.stubGlobal('fetch', vi.fn(async (path, options = {}) => {
    requests.push({ path, ...options })
    if (path === '/api/v1/topics') return response({ items: topicList })
    if (path.startsWith('/api/v1/topics/') && options.method === 'PATCH') {
      const target = topicList.find(item => path === `/api/v1/topics/${item.id}`)
      const patch = JSON.parse(options.body)
      if (!target) return response({ error: { message: 'missing topic' } }, 404)
      if (patch.action === 'request_stop') { target.status = 'stop_requested'; target.revision++ }
      else if (patch.action === 'confirm_stopped') { target.status = 'archived'; target.stop_evidence = patch.evidence }
      else Object.assign(target, patch)
      return response({ ok: true })
    }
    if (path.startsWith('/api/v1/topics/airport/reports')) return response({ items: reports })
    if (path === '/api/v1/reports/1') return response(reports[0])
    if (path === '/api/v1/topics/airport/read') { topic.unread_count = 0; return response({ ok: true }) }
    if (path === '/api/v1/mcp/capabilities') return response({ oauth_enabled: true, tools: [{ name: 'publish_report' }, { name: 'sync_topic_state' }] })
    return response({ items: [] })
  }))
})
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = ''; vi.unstubAllGlobals() })
async function mountWorkspace() { wrapper = mount(TopicWorkspace, { attachTo: document.body, props: { refreshKey: 0, themeMode: 'light' } }); await flushPromises(); return wrapper }
async function clickText(text) { const button = wrapper.findAll('button').find(b => b.text() === text); expect(button, text).toBeTruthy(); await button.trigger('click'); await flushPromises() }
describe('real topic workspace interactions', () => {
  it('opens topic, preserves stable identity during grouping, and marks only the selected report read', async () => {
    await mountWorkspace()
    expect(wrapper.findAll('.ts-topic-row')).toHaveLength(1)
    await wrapper.find('.ts-topic-name').trigger('click'); await flushPromises()
    expect(window.location.pathname).toBe('/topics/airport')
    await clickText('归组')
    await wrapper.find('dialog input').setValue('生活与消费')
    await wrapper.find('dialog form').trigger('submit'); await flushPromises()
    expect(topic.group).toBe('生活与消费')
    expect(topic.id).toBe('airport')
    await wrapper.find('.ts-report-title').trigger('click'); await flushPromises()
    expect(window.location.pathname).toBe('/reports/1')
    expect(wrapper.find('.markdown-body').text()).toContain('完整报告')
    expect(wrapper.find('.topic-reader-toc').text()).toContain('原始证据')
    expect(requests.find(r => r.path.endsWith('/read')).body).toBe('{"report_id":1}')
  })
  it('does not archive on a stop request and requires explicit external-stop evidence', async () => {
    await mountWorkspace(); await wrapper.find('.ts-topic-name').trigger('click'); await flushPromises()
    await clickText('停止跟踪并归档…'); await wrapper.find('dialog form').trigger('submit'); await flushPromises()
    expect(topic.status).toBe('stop_requested')
    expect(wrapper.text()).toContain('等待外部任务停止确认')
    await clickText('我已在外部停止，确认归档…')
    expect(wrapper.find('dialog textarea').attributes('required')).toBeDefined()
    await wrapper.find('dialog textarea').setValue('已在 Grok 停止周期任务')
    await wrapper.find('dialog form').trigger('submit'); await flushPromises()
    expect(topic.status).toBe('archived')
    expect(wrapper.text()).toContain('已在 Grok 停止周期任务')
    const confirmation=requests.find(r=>r.method==='PATCH'&&JSON.parse(r.body).action==='confirm_stopped')
    expect(JSON.parse(confirmation.body).revision).toBe(1)
  })
  it('provides manual copy fallback without claiming the client connected', async () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    await mountWorkspace(); await clickText('复制接入指令'); await clickText('复制完整指令')
    expect(wrapper.find('dialog textarea').element.value).toContain('sync_topic_state')
    expect(wrapper.text()).toContain('剪贴板不可用')
    expect(wrapper.text()).toContain('复制不等于已经连接')
  })
  it('copies instructions on the entry click when clipboard access is available', async () => {
    const writeText=vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText}})
    await mountWorkspace();await clickText('复制接入指令')
    expect(writeText).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('已复制')
  })
  it('clears selected topics when the group filter changes before a batch operation', async () => {
    topic.group = 'A'
    const other = { ...topic, id: 'topic-b', name: '主题 B', group: 'B', agents: [] }
    topicList = [topic, other]
    await mountWorkspace()
    await wrapper.find('[aria-label="选择机场情报监控"]').setValue(true)
    expect(wrapper.find('.ts-batch').text()).toContain('已选 1 个')

    await wrapper.find('[aria-label="按分组筛选"]').setValue('B')
    await flushPromises()
    expect(wrapper.find('.ts-batch').exists()).toBe(false)

    await wrapper.find('[aria-label="选择主题 B"]').setValue(true)
    await wrapper.find('.ts-batch input').setValue('B-next')
    await clickText('归组')
    expect(topic.group).toBe('A')
    expect(other.group).toBe('B-next')
    const patchedIDs = requests.filter(r => r.method === 'PATCH').map(r => r.path)
    expect(patchedIDs).toEqual(['/api/v1/topics/topic-b'])
  })
  it('emits session expiry instead of showing private data after unauthorized response', async () => {
    fetch.mockResolvedValue(response({ error: { message: 'unauthorized' } }, 401))
    await mountWorkspace()
    expect(wrapper.emitted('expired')).toHaveLength(1)
    expect(wrapper.findAll('.ts-topic-row')).toHaveLength(0)
  })
})
