import { describe, expect, it, vi } from 'vitest'
import { notifyReport } from './notifications'

class FakeNotification {
  static permission = 'granted'

  constructor(title, options) {
    this.title = title
    this.options = options
  }
}

describe('notifyReport', () => {
  it('shows a system notification for a new report while the app is visible', () => {
    const openReport = vi.fn()
    const notification = notifyReport({
      bot_name: '机场监控',
      bot_id: 'airport',
      source: 'grok',
      title: '发现新套餐',
    }, FakeNotification, openReport)

    expect(notification).toBeInstanceOf(FakeNotification)
    expect(notification.title).toBe('机场监控')
    expect(notification.options.body).toBe('发现新套餐')
    expect(notification.options.tag).toBe('grok:airport')
    notification.onclick()
    expect(openReport).toHaveBeenCalledOnce()
  })

  it('does not attempt notifications without browser support or permission', () => {
    expect(notifyReport({}, undefined)).toBeNull()
    expect(notifyReport({}, { permission: 'denied' })).toBeNull()
  })

  it('contains browser constructor failures without interrupting the event stream', () => {
    class BlockedNotification {
      static permission = 'granted'
      constructor() { throw new Error('notifications unavailable') }
    }

    expect(notifyReport({}, BlockedNotification)).toBeNull()
  })
})
