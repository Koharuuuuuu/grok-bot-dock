# P8 · OAuth 2.1 MCP 授权升级：架构与安全规范

> 状态：**P8 代码已在本地功能分支实现，默认关闭，尚未发布或真实客户端验收**（2026-10-10）。本文描述代码目标与安全边界；不能据此宣称生产或第三方客户端已启用。
>
> 目标仓库：`Nyakooo/grok-bot-dock`（私有）；默认分支 `master`。关联 [执行清单](./P8_OAUTH_EXECUTION_PLAN.md)、[迁移/回滚](./P8_OAUTH_MIGRATION.md)、[客户端接入](./P8_OAUTH_CLIENTS.md)。

## 1. 已核实基线和非目标

- 技术栈：Go 1.25、官方 `github.com/modelcontextprotocol/go-sdk v1.8.0`、Vue 3、SQLite WAL/FTS5、单镜像；运行环境约束 2 vCPU/1600 MiB。**不得增设 Redis、常驻 Node、独立认证服务器或数据库**，除非 Phase 0 提供不可替代的证据并获得批准。
- P7：精确路由 `/mcp`，Streamable HTTP 无状态模式，`publish_report` 和 `search_reports`；共享 REST 的验证、报告写入/检索、幂等和 SSE。现有 `GET /api/v1/mcp/capabilities` 是已登录用户的 UI 能力接口。
- 现有浏览器登录：管理员 `ACCESS_TOKEN` → 内存 12 小时 HttpOnly Cookie；不是基于普通用户账号的登录系统，**不能假设已有 user_id、多租户或注册/找回密码**。
- 本期必须让受信任的所有者在不向 AI 平台暴露管理员密钥的情况下批准第三方 MCP 客户端；**不能把“单所有者授权”宣传成真正多租户隔离**。
- 不变契约：`POST /api/v1/reports` 现有 Bearer 认证，`/mcp` URL、工具名/原有字段、`(source,bot_id)`、`stable_task_id` 派生、`external_id` 幂等、全部既有报告、部署端口/卷及 SSE。OAuth 不允许凭空新增任意管理、删除或执行工具。
- OAuth 不解决 Agent 自动调用/定时执行；Grok 等实际连接和定时能力须分别实测。

## 2. 协议角色及信任边界

Bolt Dock 同时实现 OAuth **Authorization Server (AS)** 和 MCP **Protected Resource/Resource Server (RS)**，外部 Grok/Claude/Cursor 等属于 OAuth Clients。不要把 `ACCESS_TOKEN` 转化/复制为 OAuth Client Secret，也不要把 AI 平台凭据写入仓库。

```text
第三方 MCP Client
  │ GET /mcp (401 + WWW-Authenticate resource_metadata)
  ├─ GET /.well-known/oauth-protected-resource[...]
  ├─ GET /.well-known/oauth-authorization-server[...]
  └─ 浏览器 GET /oauth/authorize?...（PKCE S256 + state 由客户端处理）
       └─ Bolt Dock 现有管理员会话登录 → 展示客户端/权限/资源 → 显式确认
             └─ 授权码绑定 client_id、redirect_uri、code_challenge、resource、scope
                  └─ POST /oauth/token → 独立短期 OAuth access token
                       └─ /mcp → 身份/权限/resource 校验 → 原 MCP 工具层/Store
```

授权系统应把单所有者身份建模为稳定的内部 subject（如 `owner`），以后扩展多用户可迁移；**本次不引入假的用户表或宣称支持其他用户自助授权**。现有管理员会话只用于授权页身份确认，**绝不能直接作为 MCP OAuth 凭据**。为授权页面的跨站跳转及 CSRF 使用经过审计的同源 session + Origin/CSRF token 保护；回调跳转只允许精确验证的注册 URI。

## 3. 标准发现/端点（路径以 SDK/规范核查结果为准）

- `GET /.well-known/oauth-protected-resource`：给出 `resource`（规范化、稳定的 MCP 公网资源 URI）与 `authorization_servers`；若 MCP URL 含路径，按 RFC 9728 的路径插入规则同时支持所需发现地址，**不能只写一个假定路径**。
- `GET /.well-known/oauth-authorization-server`：Issuer/authorization/token/revocation/registration 能力、`response_types_supported`、`code_challenge_methods_supported:["S256"]`、`scopes_supported` 等。Issuer 与生产公网 URL 必须一致；禁止信任请求的任意 Host/X-Forwarded-* 来生成发行者。
- `GET /oauth/authorize`：支持 `response_type=code`、`client_id`、`redirect_uri`、`code_challenge`、`code_challenge_method=S256`、`scope`、`resource`、`state`。校验客户端、精确回调匹配、授权用户会话和资源；不可无界“自动同意”。
- `POST /oauth/token`：`authorization_code` 与可选 `refresh_token` grant；校验 PKCE、绑定的 redirect URI/client/resource；授权码一次性事务消费；返回 `access_token`、`token_type=Bearer`、`expires_in`、scope，按策略返回 refresh token。OAuth 错误采用标准格式且不暴露敏感内容。
- `POST /oauth/revoke`：按 RFC 7009 及客户端身份/持有权处理，撤销 refresh/access token；对授权链传播失效。
- `GET/POST /oauth/consent`（实际 UI 路由可调整）：明确显示客户端身份、读写权限和账户；拒绝/取消有规范回调；服务器必须防 CSRF。
- **客户端注册优先级：** 1) 受控预注册（可提供 Client ID，必要时 confidential secret）；2) **Client ID Metadata Documents (CIMD)**，严格 HTTPS、元数据获取 SSRF/redirect/大小/超时防护及精确 redirect URI 核验；3) 视客户端实测兼容性和安全评估启用 RFC 7591 DCR。客户端的 credential 输入框并不意味着一定需要 secret，必须按真实平台流程判断。禁止匿名开放 DCR 造成数据库膨胀。
- `GET /oauth/jwks.json` **仅当**采用 JWT 且确实需要外部验签时增加；首选服务端存储摘要的不透明 access token，不暴露无用 JWKS 接口。
- 401 MCP 响应：`WWW-Authenticate: Bearer resource_metadata="https://…/.well-known/oauth-protected-resource", scope="reports:read"`（真实 scope 视资源/调用而定）；不能破坏 SDK 的 MCP 协议错误语义。缺/无效凭据 401；有效但权限不足 403，遵循 OAuth/MCP 规范。

官方参考：
- MCP Authorization: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
- RFC 9728 protected resource metadata: https://www.rfc-editor.org/rfc/rfc9728
- RFC 8414 AS metadata: https://www.rfc-editor.org/rfc/rfc8414
- RFC 7636 PKCE: https://www.rfc-editor.org/rfc/rfc7636
- RFC 8707 resource indicators: https://www.rfc-editor.org/rfc/rfc8707
- RFC 7009 token revocation: https://www.rfc-editor.org/rfc/rfc7009
- RFC 7591 DCR: https://www.rfc-editor.org/rfc/rfc7591

实施时核对**届时 MCP 现行规范**，记录版本、与 P7 Go SDK 的兼容性/升级风险；不要声称仅因为 HTTP 200 就合规。

## 4. 凭据模型与双认证隔离

- Legacy `ACCESS_TOKEN`：原 `/mcp` 与 `POST /api/v1/reports` 继续接受，按现有常量时间比较及权限行为运行；只适合拥有者受信任客户端。
- OAuth token：独立高熵随机不透明值；持久层只保存不可逆安全摘要/随机盐视设计；过期、授权撤销、scope、issuer、resource/audience、client_id、owner subject 均在**每次 MCP 请求及实际工具调用**验证。密钥前缀命名空间区分，两类 token 不得互认，不接受 Cookie 作为 MCP Bearer。
- 推荐 scope：`reports:read` 对应 `search_reports`；`reports:write` 对应 `publish_report`。暂不提供 `reports:manage`、`bots:read` 或 `offline_access`，除非有真实工具/API 需求；refresh grant 是否提供应由客户端兼容性决定，且需明确 consent/限期/轮换。
- **租户边界**：Bolt Dock 当前是单所有者共享报告库。授权允许读时即可能查询所有来源；发布时可能设置 `source`/`bot_id`。将这点写在授权页，不能宣称 OAuth 已自动按来源或 Bot 隔离。若需细粒度来源或 Bot 写入限制，单独设计并纳入授权/存储校验，不能依赖客户端诚实自报的 `source`。
- 不允许通过 OAuth 凭据访问浏览器管理端点、删除接口、管理员会话、`/api/v1/reports` 等；REST 保留旧验证策略。
- 授权时显示明确的读/写区分；`read` token 禁止发布；只写 token 禁止搜索。统一验签/权限中间件只能把具名 Principal 传给 MCP，不得使用“验证成功即管理员”的布尔值。
- 安全响应头、HTTPS、`Cache-Control:no-store`、限流、审计、统一 UTC 和过期清理；禁止日志输出 Authorization、client secret、code、refresh/access token、报告正文、Cookie。公开服务须限制反代信任边界、Origin、DNS rebinding。
- refresh token 采用 rotation+family 记录+重复使用检测，检测复用则撤销授权链；授权码短时（建议 ≤5 分钟）、一次性、原子使用。过期凭据清理不得破坏审计/报告。
- 如果已有 `ACCESS_TOKEN` 曾公开分享，部署前应单独轮换；不可自动轮换导致旧客户端中断。

## 5. 质量门槛和部署

必须完成 discovery、PKCE、redirect URI 精确匹配、state/consent 防 CSRF、code 重放、refresh 重放、scope/audience/issuer、Client Metadata SSRF、代理 Host 伪造、撤销即时生效、旧 Bearer 不回归、跨来源身份和 external_id 幂等测试。集成测试只对本地临时 SQLite、隔离容器运行，不向生产写测试报告。

新增 OAuth 配置默认**关闭或仅回环开发可用**；先做双栈内部测试、手动启用、灰度验证后才对公网开放。必须说明 AS 公网 URL 和反代路径。风险/回滚见 [迁移方案](./P8_OAUTH_MIGRATION.md)。
