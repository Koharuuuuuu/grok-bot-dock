# Grok Bot Dock

轻量、自托管的 AI Bot 报告聚合工作台。多个 Grok 定时任务通过统一 HTTPS API 推送 Markdown 报告，网页集中阅读、检索和接收通知。

## 核心能力
- 每个 Bot 自动生成一张响应式卡片；展示名称、最后执行时间、摘要、未读数量。
- 点击卡片查看 Markdown 报告及历史；支持日期筛选和分页。
- SQLite 持久化；FTS5 全局检索标题与正文，支持中文关键词。
- SSE 实时更新；用户授权后使用浏览器 Notification API 提醒。
- Go + Vue 3 + SQLite，单容器部署，低资源占用。

## 推送协议
`POST /api/v1/reports`，`Content-Type: application/json`：

```json
{
  "bot_id": "ai-news",
  "bot_name": "AI 行业动态",
  "executed_at": "2026-10-09T20:00:00+08:00",
  "content": "# 今日 AI 动态\n\n- 示例报告"
}
```

`bot_id` 稳定不变，`bot_name` 可修改；首次接收自动注册 Bot。建议使用 `Authorization: Bearer <token>` 的单一共享密钥，避免公网匿名写入。支持可选 `external_id` 用于幂等去重。Grok 定时任务能否自动 POST 需实际端到端验证，不属于服务端可保证的能力。

## 开发与部署
见 [技术设计](docs/TECHNICAL_DESIGN.md) 和 [执行计划](docs/EXECUTION_PLAN.md)。Codex 按执行计划逐项实现并更新勾选状态；使用 master 作为默认分支。开发前先阅读两份文档。前端使用 Node.js 构建，运行容器只保留 Go 二进制、前端静态文件和 SQLite 数据卷。

## 非目标
不做多用户平台、Redis、Elasticsearch、消息队列、MCP、AI 二次总结或复杂后台管理。

## Bot 头像
推送 API 可选 `avatar_url` 字段，Bot 可自行提交 HTTPS 头像地址；不传或头像加载失败时使用内置默认图标。相同 Bot 持续复用上次有效头像地址，直到后续推送更新；不得因缺省字段清空已有头像。

## Bolt Dock 通用信息源与 UI 基线（2026-10-09）
产品显示名称统一为 **Bolt Dock**（GitHub 仓库名不变）。取消左侧导航；顶部左侧品牌、中部默认跨全部来源的全局搜索、右侧来源下拉和统计（Bot 数、报告数、数据库大小）。来源下拉仅列出已通过 API 推送出现的来源，默认「全部来源」；**没有“自定义”或手动创建来源选项**。选择来源后卡片、搜索结果、统计同步筛选（数据库大小是全库物理值，需标明全库，不伪装成按来源统计）。Bot 卡片响应式平铺；未选中时占满内容区，选中后右侧打开 Markdown 阅读面板，面板有放大按钮进入沉浸式阅读，关闭后回到卡片。来源列表及卡片由推送数据驱动，前端不硬编码 Grok/Codex/千问。

通用推送协议新增必填 `source`（稳定的来源标识，如 `grok`、`codex`、`qwen`），唯一 Bot 键为 `(source, bot_id)`；可选 `title`、`summary`、`tags`、`external_id`、`avatar_url`，其余字段保持不变。示例见技术设计。此约定在 V1 开发前落实，不再依赖旧的仅 bot_id 唯一规则。

## 一键复制：交给 AI 的正式报告推送指令

将下面整段复制给 Grok Bolt、Codex 或其他 AI 定时任务。把 `https://YOUR_DOMAIN` 换成实际服务地址；如部署启用共享密钥，由执行环境安全注入 `BOLT_DOCK_TOKEN`，不要把密钥写进报告或公开提示词。

```text
你是一个定时信息收集 Agent。每次任务执行完成后，按以下规则向 Bolt Dock 推送正式报告。

接口：POST https://YOUR_DOMAIN/api/v1/reports
请求头：Content-Type: application/json
请求头：Authorization: Bearer <BOLT_DOCK_TOKEN>（由执行环境提供，不得泄露）

请求 JSON：
{
  "source": "grok",
  "bot_id": "your-stable-bot-id",
  "bot_name": "你的任务名称",
  "executed_at": "2026-10-09T20:00:00+08:00",
  "title": "本次正式报告标题",
  "summary": "简短摘要",
  "external_id": "your-stable-bot-id-2026-10-09",
  "content": "# 正式报告标题\\n\\n完整 Markdown 正文"
}

要求：
1. 只在本次任务最终正式报告已经完成、内容检查通过后推送一次；草稿、测试文件、模拟数据、调试日志、中间结果、空内容、失败或取消的任务一律不得推送。
2. 正文必须是完整 Markdown，保留标题、列表、表格和引用链接；不要推送文件路径、占位符或仅有摘要的内容。
3. source 与 bot_id 对同一个任务保持稳定；bot_name 可修改；executed_at 使用实际执行时间及明确时区；external_id 对同一次正式执行保持不变，重试时沿用以避免重复。
4. 发送前校验所有必填字段（source、bot_id、bot_name、executed_at、content）非空；仅发送最终 JSON，不要附加 Markdown 代码围栏。
5. 只有 HTTP 201（新建）或 HTTP 200（幂等重复）算成功；网络错误或 5xx 可有限重试并使用相同 external_id；4xx 先修复配置/报文，不要盲目重试。
6. 如果当前 AI 平台不能直接发起外部 HTTP POST，应明确报告不支持，不要虚构推送成功；由用户配置可用的 HTTP 调用能力后再执行。
7. 禁止把测试报告推到正式 API；接口联调必须在独立测试环境完成。
```

注意：上面的指令是发送规范，不保证任意 AI 定时任务平台具备外部 HTTP 请求能力。正式环境需先验证平台能发出 POST 请求。
