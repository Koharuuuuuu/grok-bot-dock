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
