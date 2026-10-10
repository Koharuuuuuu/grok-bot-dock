# P8 · 第三方 MCP 客户端接入与兼容性

> **待实施/待实测。** 现在 Bolt Dock 只有 `Authorization: Bearer <ACCESS_TOKEN>` 的远程 Streamable HTTP MCP，**不是** OAuth 服务。完成 P8 并启用后，才能使用这里的 OAuth 连接流程。

## 两种模式：谁用哪一种？

| 客户端/场景 | 当前可用模式 | P8 目标 | 注意事项 |
| --- | --- | --- | --- |
| Codex CLI | 远程 Streamable HTTP + 环境变量 Bearer；P7 已验收特定 CLI 版本 | 旧模式继续工作；可选 OAuth | 不是 Codex 专属 MCP |
| Cursor | 客户端支持配置远程 MCP；具体 Bearer 配法按客户端当前版本核实 | OAuth 浏览器授权 | 不把平台登录等同于第三方 MCP 授权 |
| Grok | 当前截图客户端要求 OAuth 凭据，尚未完成连通性验证 | OAuth 预注册 + 真实授权测试 | 不确定是否支持 CIMD/DCR、定时执行 MCP；必须实测 |
| Claude | 客户端/计划/版本需验证 | 标准远程 OAuth 连接 | 不承诺任意版本可用 |
| ChatGPT | 账号/功能入口限制需验证 | 在其实际开放的 MCP 客户端支持范围内接入 | 不承诺普通聊天或 Tasks 可调用 |
| 通用 MCP Inspector | P7 共享 Bearer 已验收 | OAuth discovery、登录、工具发现与调用 | 不代替真实平台测试 |

## Legacy Bearer（现已支持）

- URL：`https://grok.nyaco.cn/mcp`（仅为现有对外接入示例；请按实际公开反向代理配置验证）。
- 传输：Streamable HTTP。
- 认证：在**受信任**客户端的安全凭据管理中设置 `Authorization: Bearer <ACCESS_TOKEN>`；不要将其粘贴到聊天、仓库或前端页面。
- Codex CLI 可参考 README 的 `bearer_token_env_var` 示例（本地环境变量名 `BOLT_DOCK_TOKEN`）。
- 管理员共享 Bearer 可读取所有历史并发布任意来源；不适合公开分发。

## OAuth（实现并启用后）

1. 复制 MCP HTTPS URL，客户端读取受保护资源元数据与授权服务器元数据。
2. 依据客户端支持方式：**预注册**，或者 CIMD；必要时安全受控地启用 DCR。对于 Grok 的“输入 OAuth 应用注册凭证”，先核查其究竟需要 client ID、client secret、redirect URI 哪些字段，不要编造预设值或回调 URI。
3. 浏览器跳转 Bolt Dock 授权页面；由已有管理员会话认证所有者；核对展示的客户端身份、资源、`reports:read` / `reports:write` 权限并主动批准或拒绝。
4. 客户端取得单独 OAuth Token 后连接 `/mcp`，调用 `search_reports` / `publish_report`。读权限不能写，写权限不能读。
5. 在 Bolt Dock 已授权应用页面撤销该客户端；重试时旧 Token 应被拒绝。

**必须实测** OAuth Protected Resource Metadata → AS Discovery → 预注册 Client ID → PKCE/授权 → token → `tools/list` → 工具调用 → 授权撤销，以及失败/过期/无权场景。OAuth 支持与定时无人值守是两件事，不能因为 MCP 接入就宣称“每小时自动推送”。

## 凭据与用户提示

- 管理员 `ACCESS_TOKEN` 绝不放入 OAuth client secret 或公开 OAuth discovery。
- 生产环境只公开 HTTPS 和正确的 issuer/resource 元数据。
- 本项目当前只存在一个管理员密钥登录机制；P8 单所有者授权并不等于多用户注册、多租户隔离。
- 未确认第三方客户端权限/政策前不要在文档中声称 Grok/Claude/ChatGPT 已验证。
