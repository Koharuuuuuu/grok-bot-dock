# P8 · 主流 AI Agent MCP 客户端兼容性策略与验收矩阵

> 状态：待执行 / 待实测（2026-10-10）。这是 Bot Dock 的产品级目标：**尽可能兼容主流支持自定义远程 MCP 的 AI Agent**，而非绑定 Codex 或 Grok。该文件是 P8 必须遵循的验收标准。不能将“符合协议”直接等同于“某个客户端已验证”。

## 1. 目标与边界

- 服务端始终提供独立、标准的 HTTPS Streamable HTTP MCP 入口 `/mcp`，保留 `publish_report`、`search_reports`，兼容现有 REST 与 Legacy Bearer。
- 认证并存：**Legacy Bearer** 供已有可信客户端继续使用；**OAuth 授权码 + PKCE S256** 供需要交互式授权的客户端使用。客户端如何发现资源元数据、注册与获取 Token 按当期 MCP 授权规范实现。
- 优先级：**Grok、Codex、Cursor、Claude**；第二梯队：其他通用 MCP 客户端与 MCP Inspector。具体版本、产品形态、套餐、OAuth 功能差异都需实际记录。
- 目标不是宣称“所有 AI 都支持”。只有具备**自定义远程 MCP 服务器连接 + 工具调用**能力的客户端才能直接接入；不具备者继续通过 REST/Webhook/中间适配器。
- AI 是否自动调用 `publish_report`、是否允许定时/后台执行，属于**客户端产品能力**，不由 Bot Dock MCP Server 保证。

## 2. 客户端兼容矩阵（每行必须留证据）

| 客户端 | 接入路径优先级 | 必须核实 | 验收状态 |
| --- | --- | --- | --- |
| Grok（指定具体产品形态） | OAuth 预注册优先；按实际支持考虑 CIMD/DCR | 是否能填自定义 MCP URL、Client ID/Secret、Redirect URI、OAuth 发现、工具调用、定时任务是否可用 | **未实测** |
| Codex CLI / App | 先确保已有 Bearer 不回归，再验证 OAuth（如客户端支持） | 具体版本、远程 Streamable HTTP、凭据设置、tools/list、读写工具 | **P7 已有特定版本 Bearer 验收；P8 OAuth 未实测** |
| Cursor | 远程 MCP + Bearer/OAuth，按具体版本支持情况选择 | 客户端配置、OAuth 浏览器回调、工具调用、Token 续期 | **未实测** |
| Claude（具体产品/计划） | 远程 MCP + OAuth | 是否开放自定义远程 MCP、授权回调、工具权限、刷新与撤销 | **未实测** |
| MCP Inspector / 独立测试客户端 | 标准协议及 OAuth 双路径 | 初始化、发现、工具列表、发布、查询、撤销、错误路径 | **P8 待实测** |
| 其他 AI Agent | 能力探测后选择 Bearer/OAuth | 远程 MCP 支持、鉴权机制、工具调用权限 | **未实测** |

> 表中现状是文档层面的保守标记；实施时必须用真实日志、截图或自动化测试结果更新。不要伪造兼容性徽章或宣称“已支持”。

## 3. 协议层强制兼容清单

- [ ] Streamable HTTP MCP 初始化、协议版本协商、tools/list、tools/call、HTTP Accept/Content-Type、JSON-RPC 错误和请求取消符合 SDK 与当期规范。
- [ ] MCP Protected Resource Metadata（RFC 9728）及正确 `WWW-Authenticate` discovery，Authorization Server Metadata（RFC 8414），固定可信公网 HTTPS issuer/resource，防代理 Host 伪造。
- [ ] OAuth Authorization Code + PKCE S256、标准 OAuth 错误、精确 redirect URI、资源绑定（RFC 8707）、scope 权限、Token 过期与撤销。
- [ ] 预注册 OAuth Client ID 为第一优先；支持公开/机密客户端各自安全规则；CIMD/DCR 仅在规范要求与客户端需求成立且安全评审后启用。
- [ ] Legacy Bearer 与 OAuth 双栈并存，绝不互认、越权或将 OAuth Token 用于 REST 管理 API；OAuth 只读/只写权限真正由服务端工具调用强制执行。
- [ ] 客户端配置示例必须明确标注**客户端特有**，通用接入页仅显示 MCP URL、认证方式、工具和通用步骤；不要求 Codex 或本地 CLI 存在。
- [ ] 网络/反代兼容：HTTPS、正确 well-known 路由、`WWW-Authenticate` 透传、重定向和 CORS/Origin 规则、超时/断连与响应大小限制。
- [ ] 报告写入共享业务层、stable_task_id、external_id 幂等、FTS 查询、SQLite/SSE、旧 REST 路由保持兼容。

## 4. 每个客户端统一执行的验收用例

1. **发现**：从公开 `/mcp` URL 完成 MCP/OAuth discovery；记录服务端元数据、客户端实际读取路径与响应。
2. **授权**：按客户端支持方式完成 Bearer 或 OAuth；OAuth 验证预注册、PKCE、用户 consent、Token 有效期和权限。
3. **发现工具**：列出 `publish_report`、`search_reports`，工具 Schema 可被客户端正确理解。
4. **发布**：仅在隔离测试环境提交一份固定 `external_id` 的报告，确认 Web/REST 可见；重复提交不重复入库。
5. **检索**：查询中文报告，确认返回内容与权限一致。
6. **拒绝与撤销**：无 Token、错误 Token、缺少 scope、OAuth 授权撤销后必须失败；旧 Bearer 仍有效。
7. **稳定性**：断线、重连、Token 过期/续期（如支持）、客户端重启、反代转发后再次调用。
8. **证据**：记录产品/版本/日期/认证模式、配置项、测试环境、日志/截图、成功或失败、限制与回退方式。
9. **自动化能力单独验证**：仅当客户端明确支持定时或后台调用远程 MCP 时，才可标注“可自动推送”；不把手动 tools/call 成功冒充定时成功。

## 5. 优先级与失败处理

- P0：独立 MCP 标准客户端双认证端到端 + 原有 Codex Bearer 回归。
- P1：Grok OAuth 预注册和真实连接（需用户授权时 BLOCKED），Cursor OAuth 或 Bearer 实测。
- P2：Claude OAuth、其他客户端与长连接/续期兼容。
- 如果某客户端不支持远程 MCP 或其 OAuth 注册机制与当前服务器不兼容，记录**明确的客户端能力限制**；先评估符合标准的兼容方案，不能为一个平台破坏 MCP 规范或旧客户端。
- 无法取得真实账号/授权时，必须继续完成协议级测试和其他客户端测试，但不得将该客户端标记通过。

## 6. 交付

- [ ] 更新 `docs/P8_OAUTH_CLIENTS.md` 与 README 的“客户端兼容性”章节，逐个给出经验证的步骤和限制。
- [ ] 在 `docs/P8_OAUTH_ACCEPTANCE.md` 加入按客户端的证据矩阵，明确“协议通过”和“真实平台联调通过”的区别。
- [ ] 前端「MCP 接入」保持客户端无关，显示双认证与通用配置；可选客户端示例不得假设所有客户端配置格式相同。
- [ ] 继续遵守 P8 执行计划的授权边界：**未经额外明确授权，Codex 不得自动 push、创建 PR、合并或部署生产**。
