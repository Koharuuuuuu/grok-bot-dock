export function notifyReport(report, NotificationAPI, onClick = () => {}) {
  if (!NotificationAPI || NotificationAPI.permission !== 'granted') return null
  try {
    const notification = new NotificationAPI(report.bot_name || 'Bolt Dock', {
      body: report.title || report.summary || '收到新报告',
      tag: `${report.source}:${report.bot_id}`,
    })
    notification.onclick = onClick
    return notification
  } catch {
    return null
  }
}
