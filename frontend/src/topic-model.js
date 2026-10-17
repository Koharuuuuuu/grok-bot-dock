import { buildMCPServiceURL, classifyMCPAddress } from './ui-model'

export const topicTabs = [
  { key: 'follow', label: '关注' }, { key: 'updates', label: '有更新' },
  { key: 'all', label: '全部主题' }, { key: 'archived', label: '已归档' },
]
export function filterTopics(items, { tab = 'all', group = '', query = '', reportTopicIDs = [] } = {}) {
  const term = query.trim().toLocaleLowerCase()
  const matches = new Set(reportTopicIDs)
  return items.filter(t => (tab === 'archived' ? t.status === 'archived' : t.status !== 'archived')
    && (tab !== 'follow' || t.followed) && (tab !== 'updates' || t.unread_count > 0)
    && (!group || (group === '__ungrouped' ? !t.group : t.group === group))
    && (!term || `${t.name} ${t.group} ${t.summary}`.toLocaleLowerCase().includes(term) || matches.has(t.id)))
}
export function makeAgentOnboarding(endpoint, tools, oauthEnabled) {
  const url = buildMCPServiceURL(endpoint)
  const names = tools.map(t => t.name)
  if (!names.includes('publish_report') || !names.includes('sync_topic_state')) throw new Error('服务端主题工具尚未就绪，请检查服务版本。')
  return `请把你完成的正式报告接入我的 Bolt Dock 主题空间。

MCP URL：${url}
传输：Streamable HTTP
可达性：${classifyMCPAddress(url) === 'local' ? '当前地址是本机或内网地址，只能由可访问该网络的客户端连接；托管 Agent 需先由我配置实际可达的 HTTPS 服务地址。' : '请确认当前平台能够访问此 HTTPS 地址；不要把页面可打开当作客户端已经连通。'}
认证：${oauthEnabled ? '使用服务端元数据发现 OAuth，发起浏览器登录并由我确认 reports:read / reports:write 授权。' : '在客户端安全设置中配置管理员单独提供的 Bearer 凭据。不要在对话中索要、回显或复制管理员密钥。'}

1. 先识别当前平台是否支持自定义远程 MCP，以及你是否有配置连接的权限。能自动配置时执行配置；需要我操作时准确说明步骤。不能配置或不支持时明确说明，不能声称已经接入。
2. 连接后发现实际工具（当前服务声明：${names.join(', ')}）。只有连接、授权和工具发现成功才算接入；本指令不授权上传测试、草稿、日志或历史私密资料。
3. 每个任务使用稳定的 source 和 stable_task_id（或 bot_id）。首次发布时提供 topic_name；没有 topic_id 时服务器会按任务稳定建立主题。保存返回的 topic_id，后续原样复用；多个任务贡献同一主题时显式使用该 topic_id。不能按日期、随机值或相似标题创建/猜测主题。
4. 仅在正式报告完成并检查后调用 publish_report，保留完整 Markdown。executed_at 为实际带时区的 RFC3339 完成时间；每次执行生成一个 external_id，网络重试原样复用。不能拿摘要代替原文。
5. 首次成功发布后，每次后续周期在开始耗费模型 Token 前，调用 sync_topic_state，使用相同 source 和任务身份。若 requested_action=stop，停止外部周期调度；只有确认实际停止后，才以 stopped=true 和返回的 revision 确认。若没有停止权限，告诉我如何在外部停止，不能伪造确认。
6. 本空间不负责替外部平台创建或重启调度；恢复主题后需要按我的要求另行恢复外部任务。HTTP 409/主题已归档时停止发布并报告状态，不能通过新建任务或主题绕过。

服务不会因为主题关注、分组或未分组就调用模型。不使用 URL、文案、报告或日志传递密钥。请报告已完成的步骤、仍需要我完成的授权，以及能力限制。`
}

export function makeStopInstruction(topic) {
  const tasks = topic.agents.map(a => `${a.source} / bot_id=${a.bot_id}`).join('\n')
  return `请停止以下 Bolt Dock 主题关联的外部周期任务，保留历史报告：\n主题：${topic.name}\ntopic_id：${topic.id}\n停止请求版本：${topic.revision}\n贡献任务：\n${tasks}\n请先确认有权停止对应平台调度，实际停止后再调用 sync_topic_state，以相同 source/bot_id、revision=${topic.revision}、stopped=true 确认。没有停止权限时，明确说明需要我在平台操作；不要仅停止本轮执行或伪造成功。`
}
