# Agent 接入指南

本文说明如何将 Agent 的正式报告写入 Bolt Dock，以及 REST、远程 MCP 和 OAuth 的边界。服务器安装、HTTPS、密钥管理和备份见[部署指南](DEPLOYMENT.md)。

## 推荐接入流程

1. 登录工作台，选择「复制接入指令」。
2. 将文案交给目标 Agent；文案包含服务地址、主题身份、最终报告规则和授权要求，不包含管理员密钥。
3. 将密钥放入 Agent 平台自己的安全凭据存储，再配置 REST 或 MCP。
4. 先进行一次受控测试，确认报告只在任务完成后发布，并出现在预期主题中。

复制指令不代表客户端已连接，也不保证平台会自动执行或定时触发。Agent 是否支持远程 MCP、HTTPS POST 和任务调度，需按产品、版本和账号能力单独确认。

## 主题身份与发布规则

首次正式报告会自动创建主题。后续执行应复用稳定的 `source` 与 `bot_id`，或复用 `stable_task_id`；多个独立任务只有显式使用相同 `topic_id` 才会共同贡献同一主题。服务不会按相似标题自动合并主题。

每次执行完成后生成唯一 `external_id`，重试时必须原样复用，以避免重复报告。只发送经过检查的最终报告；草稿、日志、中间结果、失败或取消任务不得发布。归档主题拒绝新报告；收到停止请求的外部任务应先停止，再确认相同版本。

## REST API

向 `POST /api/v1/reports` 发送 JSON，并在 `Authorization` 头中提供 `ACCESS_TOKEN`。必需字段是 RFC3339 格式的 `executed_at` 和非空 Markdown `content`。来源和 Bot 身份可以省略，但若省略 `bot_id`，必须提供稳定的 `stable_task_id`；否则服务器拒绝创建不稳定身份。

```sh
curl -X POST "https://YOUR_DOMAIN/api/v1/reports" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary '{
    "source":"grok",
    "stable_task_id":"airport-monitor",
    "bot_name":"外盘机场监控",
    "executed_at":"2026-10-17T20:00:00+08:00",
    "external_id":"run-unique-id",
    "title":"机场候选情报",
    "summary":"本次检查结果",
    "content":"# 候选机场\n\n完整 Markdown 报告"
  }'
```

同一 Bot 的 `(source, bot_id, external_id)` 再次提交时返回原报告，不会重复创建。完整接口列表见[技术设计](TECHNICAL_DESIGN.md)。

## 远程 MCP

服务端点为 `https://<你的公开域名>/mcp`，传输为 Streamable HTTP。客户端工具包括：

| 工具 | 用途 |
| --- | --- |
| `publish_report` | 发布正式报告；支持稳定主题身份和幂等重试 |
| `search_reports` | 检索历史报告；工具结果只返回元数据和摘要 |
| `sync_topic_state` | 查看主题停止请求版本并确认外部任务已经停止 |
| `sync_connection_state` | OAuth 客户端专用的连接断开确认 |

共享 Bearer 模式使用管理员 `ACCESS_TOKEN`，只有服务所有者或受信任的单用户环境适用。当前没有独立 Agent Token、按客户端授权、租户隔离或凭据撤销；不要把管理员密钥当作公共连接码。OAuth MCP Token 不能调用 REST API。

OAuth 客户端可以通过所有者发起的删除请求收到带版本的断开指令。客户端确认前只允许调用 `sync_connection_state`；若客户端离线，所有者可强制删除本地凭据，并需手动清理离线客户端。共享 Bearer 客户端没有独立身份，因此无法逐客户端通知或删除。

客户端配置文件格式因产品而异。凭据应使用客户端自己的安全配置，不要把 Token 放进 URL、共享配置、提示词、报告或日志。Codex CLI 可按客户端能力配置环境变量或使用 OAuth；Grok.com、Claude、Cursor 的具体支持需逐版本验证。当前真实 OAuth 客户端验收范围见 [P8 客户端兼容表](P8_MCP_CLIENT_COMPATIBILITY_MATRIX.md) 和 [P8 验收记录](P8_OAUTH_ACCEPTANCE.md)。

## 可选 OAuth

OAuth 默认关闭。启用前，在服务器配置 `OAUTH_ENABLED=true` 和固定的 `PUBLIC_BASE_URL=https://<canonical-host>`；该 URL 必须是 origin，不支持路径，也不能由 Host 或 `X-Forwarded-*` 请求头推导。启用后，管理员可在「MCP 接入」中登记客户端和精确回调 URI。

实现范围是授权码 + PKCE S256 的受限 profile，不代表完整 OAuth 2.1 合规。当前不支持匿名动态客户端注册和 refresh token；服务仍是单所有者模型。公开启用前，应按产品自己的客户端文档完成 HTTPS 代理、重定向和权限实测。

- [OAuth 架构与配置](P8_OAUTH_ARCHITECTURE.md)
- [OAuth 威胁模型](P8_OAUTH_THREAT_MODEL.md)
- [客户端指南](P8_OAUTH_CLIENTS.md)
- [验收记录与未验收项](P8_OAUTH_ACCEPTANCE.md)

## 其他工作台能力

工作台支持全文搜索、逐篇未读、Markdown 阅读和单报告限时分享。分享链接无需登录即可阅读单份报告，创建后只显示一次；撤销或删除原报告会阻止后续访问。分享链接是持有者凭据，分享前应确认内容适合公开阅读。访问令牌和 OAuth MCP Token 均不能管理分享。部署与代理日志要求见[部署指南](DEPLOYMENT.md)。
