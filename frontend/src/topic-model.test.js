import { describe, expect, it } from 'vitest'
import { filterTopics, makeAgentOnboarding, makeStopInstruction } from './topic-model'

const items = [
  { id: 'airport', name: '机场监控', group: '', summary: '优惠变化', followed: false, unread_count: 2, status: 'active' },
  { id: 'tibo', name: 'Tibo 重置监控', group: 'AI', summary: '', followed: true, unread_count: 0, status: 'active' },
  { id: 'old', name: '旧课题', group: '研究', summary: '', followed: true, unread_count: 3, status: 'archived' },
]
describe('topic organization and agent instructions', () => {
  it('keeps organization independent from agent identity and archived content', () => {
    expect(filterTopics(items, { tab: 'follow' }).map(t => t.id)).toEqual(['tibo'])
    expect(filterTopics(items, { tab: 'updates' }).map(t => t.id)).toEqual(['airport'])
    expect(filterTopics(items, { group: '__ungrouped' }).map(t => t.id)).toEqual(['airport'])
    expect(filterTopics(items, { tab: 'archived' }).map(t => t.id)).toEqual(['old'])
    expect(filterTopics(items, { query: '正文证据', reportTopicIDs: ['tibo'] }).map(t => t.id)).toEqual(['tibo'])
  })
  it('copies capability-aware instructions without inventing auth or scheduling support', () => {
    const tools = [{ name: 'publish_report' }, { name: 'sync_topic_state' }]
    const guide = makeAgentOnboarding('https://reports.example.com', tools, true)
    expect(guide).toContain('https://reports.example.com/mcp')
    expect(guide).toContain('OAuth')
    expect(guide).toContain('不能声称已经接入')
    expect(guide).toContain('external_id')
    expect(guide).toContain('stopped=true')
    expect(guide).toContain('不能按日期')
    expect(() => makeAgentOnboarding('https://reports.example.com', [], false)).toThrow('尚未就绪')
    const stop = makeStopInstruction({ name: '机场监控', id: 'airport', revision: 7, agents: [{ source: 'grok', bot_id: 'stable-task' }] })
    expect(stop).toContain('revision=7')
    expect(stop).toContain('实际停止')
    expect(stop).toContain('stable-task')
  })
})
