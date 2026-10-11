# P8 · 第三方 MCP 客户端接入与兼容性

> **OAuth 服务已在本地 P8 分支实现为 opt-in；默认关闭且尚未发布到 master/GHCR。** 协议自动化测试不能证明 Grok、Codex、Cursor、Claude 的真实客户端连接；各产品 OAuth 仍需按具体版本联调。

## 两种模式：谁用哪一种？

| 客户端/场景 | 当前可用模式 | P8 目标 | 注意事项 |
| --- | --- | --- | --- |
| Codex CLI | 远程 Streamable HTTP + 环境变量 Bearer；P7 已验收特定 CLI 版本 | P8 OAuth 可按标准尝试 | OAuth 未实测；不是 Codex 专属 MCP |
| Cursor | 官方文档提到远程 MCP/OAuth；具体 Bearer 配法按版本核实 | OAuth 浏览器授权 | 本地 OAuth 协议未在 Cursor 产品端验证 |
| Grok.com Custom Connector | 官方文档描述：添加 Custom MCP URL 并完成所需认证；服务须公网可达 | 按 Grok 实际提供的注册资料配置客户端；不能预先编造 client_id、redirect URI 或 secret 要求 | 官方说明认证可使用 OAuth 或 API key；具体 OAuth discovery、redirect/client 注册、scope/resource 和该服务的授权回调仍待实测 |
| Grok CLI | 官方文档支持远程 HTTP MCP，OAuth server 首次使用时启动浏览器授权 | 先核实本机 CLI 版本，再从标准发现接入 | 具体 draft-16/MCP 元数据参数及本服务 OAuth 端到端尚未实测 |
| Claude | 官方资料提及远程 MCP/OAuth；具体产品、计划和版本需验证 | 标准远程 OAuth 连接 | 本地 OAuth 协议未在 Claude 产品端验证 |
| ChatGPT | 账号/功能入口限制需验证 | 在其实际开放的 MCP 客户端支持范围内接入 | 不承诺普通聊天或 Tasks 可调用 |
| 通用 MCP Inspector | P7 共享 Bearer 已验收 | OAuth discovery、登录、工具发现与调用 | 不代替真实平台测试 |

## Legacy Bearer（现已支持）

- URL：`https://grok.nyaco.cn/mcp`（仅为现有对外接入示例；请按实际公开反向代理配置验证）。
- 传输：Streamable HTTP。
- 认证：在**受信任**客户端的安全凭据管理中设置 `Authorization: Bearer <ACCESS_TOKEN>`；不要将其粘贴到聊天、仓库或前端页面。
- Codex CLI 可参考 README 的 `bearer_token_env_var` 示例（本地环境变量名 `BOLT_DOCK_TOKEN`）。
- 管理员共享 Bearer 可读取所有历史并发布任意来源；不适合公开分发。

## OAuth（当前代码已实现；需版本包含 P8 且管理员启用）

1. 仅在反向代理和 canonical `PUBLIC_BASE_URL` 已验证后，将 `OAUTH_ENABLED=true` 并配置 `PUBLIC_BASE_URL=https://<origin>`。当前不支持服务 URL 子路径。
2. 工作台 MCP 接入页可为可信 client 登记名称和平台提供的**精确** redirect URI；平台要求 confidential secret 时选择 confidential client，并立即将创建时返回的 secret 安全录入平台。支持 CIMD 的公开客户端也可用其 HTTPS Client ID Metadata Document URL 作为 `client_id`；服务会校验 URL、获取并验证元数据和 redirect URI，再让管理员审批。管理员 `ACCESS_TOKEN` 不能替代 client secret。
3. 客户端复制 MCP URL 后触发发现。浏览器跳到 Bolt Dock；管理员登录并明确勾选所需 scope。登录、同意、授权码/PKCE、resource、client 和 redirect URI 由服务校验。CIMD 客户端名称和 redirect URI 来源于经验证的远端 metadata document。
4. 授权仅发 `reports:read` 和/或 `reports:write`，可用 15 分钟；grant 期限 30 天。没有 refresh token，客户端需再次进行授权流程。
5. 在工作台停用 client 或撤销单条 grant；已发 token 应立即失效。

**官方资料核验（2026-10-10）：** [Grok Connectors](https://docs.x.ai/grok/connectors) 已说明 Custom MCP connector、公开网络可达性及完成所需认证；[Custom MCP Server Tunneling](https://docs.x.ai/grok/connectors/custom-mcp-tunneling) 明确列出 OAuth/API-key 认证仍由 Grok 完成；[Grok CLI MCP 文档](https://docs.x.ai/build/features/mcp-servers) 说明远程 HTTP、自动 OAuth 浏览器流程和本地凭据存储。以上是产品能力的官方文档证据，**不是 Bolt Dock 的真实兼容验收**。目前仍未从真实 Grok 账号获得 client registration、redirect URI 或授权请求参数，也未执行工具调用。具体 OAuth discovery、resource/scope、客户端注册机制、撤销及账号/套餐入口必须在实际联调时记录，不得臆造。

当前支持 CIMD，但尚未完成真实第三方客户端联调，也未声称通过 CIMD 草案全部符合性验证；DCR 与 refresh token 未实现。若目标客户端必须刷新 Token，或拒绝 service metadata/resource 参数，则本实现不能据此标记兼容；先记录实际客户端请求再扩展。

机密客户端可在 OAuth 管理页轮换 Client Secret。新 Secret 只在轮换响应中返回一次，数据库仅保存 SHA-256 摘要；公开客户端不能轮换 Secret。轮换只替换客户端认证凭据，不会重签或自动撤销已签发 access token；已有 Token 仍按各自 TTL、grant 撤销和客户端停用规则生效。客户端 Secret 与 `ACCESS_TOKEN` 相互独立。

**必须实测** OAuth Protected Resource Metadata → AS Discovery → 预注册 Client ID → PKCE/授权 → token → `tools/list` → 工具调用 → 授权撤销，以及失败/过期/无权场景。OAuth 支持与定时无人值守是两件事，不能因为 MCP 接入就宣称“每小时自动推送”。

## 凭据与用户提示

- 管理员 `ACCESS_TOKEN` 绝不放入 OAuth client secret 或公开 OAuth discovery。
- 生产环境只公开 HTTPS 和正确的 issuer/resource 元数据。
- 本项目当前只存在一个管理员密钥登录机制；P8 单所有者授权并不等于多用户注册、多租户隔离。
- 未确认第三方客户端权限/政策前不要在文档中声称 Grok/Claude/ChatGPT 已验证。
