# P8 · OAuth 2.1 MCP 升级：Codex 严格执行计划与 TODO

> 状态：**规划已冻结，尚未实现**。目标：为已上线 P7 Streamable HTTP MCP 增加独立 OAuth 授权，完整保留共享 Bearer、REST、SQLite 报告数据和前端工作台。关联 [设计](./P8_OAUTH_ARCHITECTURE.md)、[迁移](./P8_OAUTH_MIGRATION.md)、[客户端](./P8_OAUTH_CLIENTS.md)。执行时同步更新这些文档和本清单，**每项验证前不得打勾**。

## Codex 启动指令

读取 `README.md`、`docs/TECHNICAL_DESIGN.md`、`docs/P7_MCP_EXECUTION_PLAN.md`、`docs/P7_MCP_ACCEPTANCE.md` 和全部 P8 文档，从 `master` 的最新提交开始。你是主 Agent，允许在不冲突的设计/测试/前端子任务中使用副 Agent；核心认证和数据库迁移由主 Agent 统一审查整合。**先审计真实代码再定设计，不准按文档假定不存在的表和用户体系**。持续推进到验收报告完成；凡需生产密钥、外部平台登录或生产部署的任务标记 blocked，不要伪造通过。

## P8 范围和放行标准

**必须**：MCP OAuth 发现、规范登录/Consent、Authorization Code + PKCE S256、受控 Client 预注册、资源绑定、最小 scope、Token 过期和撤销、旧 Bearer 兼容、单用户授权管理 UI、SQLite 可逆发布方案、安全与回归测试、第三方连接操作手册。

**条件范围**：CIMD（如客户端支持，应作为优先兼容能力），DCR（仅当真实客户端要求且经安全评估），Refresh Token（必要时加入安全轮换）；不需 JWT/JWKS、额外容器或多租户注册。禁止为追求“OAuth 2.1”标签发明标准、无条件开放注册或把共享管理员 Token 充作新 Client Secret。

**完成条件**：可验证的 OAuth 发现与 PKCE 授权、独立凭据下 `tools/list` 与读写、拒绝缺 scope/错误资源、撤销即时生效、现有 P7/REST 全量回归、可迁移可回滚、文档明确实测客户端范围。Grok 外部实测若受账户/授权限制，必须标记“待外部验收”；不因此宣称已经兼容。

## Phase 0：审计与冻结基线（禁止跳过）

- [ ] 检查 `internal/api` 的 Bearer、Origin、Cookie/CSRF、MCP 路由、SDK handler 和 shared service boundary。
- [ ] 检查 `internal/store` schema/migrations、SQLite 连接/事务/备份、SSE、报告来源与身份模型。
- [ ] 检查 `frontend` 现有“推送指令”和“MCP 接入”弹窗与设置风格。
- [ ] 记录 `go.mod` SDK v1.8.0 能否实现必要 OAuth 流程；确认 MCP 当期授权规范以及必要升级/升级回归。
- [ ] 基线：`go test ./...`、`cd frontend && npm ci && npm test && npm run build`；记 SHA 与可复现实测输出。
- [ ] 更新 P8 设计，明确当前系统无普通用户账号，仅存在管理员 ACCESS_TOKEN/浏览器会话，确定“单所有者授权”的 subject 模型。

## Phase 1：协议发现与受控客户端注册

- [ ] 明确稳定 `PUBLIC_BASE_URL` / issuer / MCP resource 配置和受信任反代规则，不由任意 Host 推断。
- [ ] Protected Resource Metadata：符合 RFC 9728 的路径发现（含路径型资源的规则），正确 HTTPS 资源标识/AS 列表。
- [ ] AS Metadata：符合 RFC 8414 的 issuer、authorize/token/revoke 等字段，宣告 PKCE S256。
- [ ] 401 `WWW-Authenticate` 按 MCP OAuth 标准携带 resource metadata 指引；现有 MCP 处理器及旧 Bearer 不回归。
- [ ] 手动预注册可信 OAuth Client，回调 URI 精确比对、client type/secret 存储保护；提供客户端信息展示与注册撤销流程。
- [ ] 确认 CIMD 的处理与第三方实际支持；如需 DCR，先安全评估并增加保护策略，禁止无限开放写入。

## Phase 2：数据库 + 授权码与 Token

- [ ] 编写 additive schema-version 迁移：OAuth clients/grants/codes/access tokens/audit（按需 refresh）；索引/UTC/哈希存储。
- [ ] 登录后授权页面显示真实 client、scope、资源以及当前是全库读/写的安全边界，支持明确允许/拒绝。
- [ ] 授权端严格校验 response_type、registered redirect_uri、code_challenge_method=S256、scope、resource、登录状态与 anti-CSRF。
- [ ] 授权码短有效期、原子一次使用；Token endpoint 校验 PKCE、client/redirect/resource 绑定并返回标准错误。
- [ ] 不透明 access token 足够随机，只保存摘要，短生命周期，可撤销；如支持 refresh，必须 rotation+family 重放检测。
- [ ] 建立审计/限流/过期清理，日志/报错绝不输出凭据及报告全文。

## Phase 3：双认证与细粒度工具权限

- [ ] `/mcp` 接受原 Legacy Bearer 或新 OAuth access token，二者独立验证/不会相互升级权限；Cookie 始终无效。
- [ ] 形成明确 Principal/Scope/Resource context；`search_reports` 要求 `reports:read`；`publish_report` 要求 `reports:write`。
- [ ] OAuth Token 只能调用 MCP 工具，不得绕过 REST 写入、浏览器管理、删除 API；旧 REST 不改行为。
- [ ] source/bot identity、stable_task_id、external_id 幂等、SQLite/FTS/SSE 共用原服务，无复刻写入/检索业务。
- [ ] 401/403/工具权限错误一致；MCP SDK 的初始化和 tools/list 协议兼容。

## Phase 4：前端及客户端体验

- [ ] 沿用 Vue 工作台 UI 增加授权确认、已连接客户端/授权记录、按客户端撤销、错误/取消/过期交互。
- [ ] “MCP 接入”面板展示准确双认证能力，OAuth 未启用时不得出现可用的 OAuth 接入指导。
- [ ] README、TECHNICAL_DESIGN、客户端/安全/迁移文档与实际接口同步，不覆盖 P7 已验收说明。
- [ ] 仅测试环境或经过允许的外部客户端真实验证 Grok 与 Claude 的 OAuth 连接。Cookie/session 不可被 AI Client 复用。
- [ ] 验证 Codex CLI 既有 Bearer 配置和其他 P7 Inspector 场景。

## Phase 5：自动化安全回归（所有必须通过）

- [ ] Discovery 正常/坏 Host/代理头污染/HTTPS 假冒；跨源 Origin 和 CSRF；scope/resource/issuer 错误。
- [ ] PKCE 空/错误/plain 禁止、错误 redirect_uri、code 过期/重放、无效 client、用户拒绝。
- [ ] Access Token 过期/撤销、客户端撤销、只读尝试写入、只写尝试读取、OAuth token 不能调用 REST。
- [ ] 若提供 refresh：轮换后旧 token 重放导致 family 撤销；并发消费一致性。
- [ ] 如接入 CIMD/DCR：SSRF、redirect 限制、超时/大小/速率/恶意 metadata、匿名注册泛滥防护。
- [ ] 新旧认证并存、报告重复 external_id、中文搜索/游标、FTS、SSE、网页登录/注销、所有已有 Go/Vue 测试。
- [ ] SQLite schema 从旧版本升级、重复迁移、容器重启、备份恢复演练；不丢历史报告/Bot。
- [ ] `make test`、`make build`、Docker 构建与健康检查、镜像内运行 smoke test、资源实测。

## Phase 6：交付策略和验收记录

- [ ] 按仓库 PR 保护策略分阶段提交，CI 全绿；不得以 docs-only push 误当作 GHCR 新版本已发布。
- [ ] 默认 OAuth 功能关闭；不开启生产 OAuth/不触碰真实生产 DB，除非用户明确授权。
- [ ] 更新 `docs/P8_OAUTH_ACCEPTANCE.md`：逐项日期、提交 SHA、测试命令/结果、真实客户端版本、阻塞、部署风险、回滚演练。
- [ ] 文档 README 仅宣称已经完成且验证的功能；未验证项必须明确标记。
- [ ] 结束时报告变更清单、迁移说明、权限与安全矩阵、Legacy 回归、客户端接入步骤、已知限制和下一步生产发布手册。

## 统一停止/决策策略

如需要真实 OAuth client 注册资料、Grok/Claude 登录、平台外网授权或生产访问，只将该项标注 BLOCKED，完成其他独立任务；**不使用假成功替代真实联调**。不擅自增加多用户账户体系、不改变 `ACCESS_TOKEN`、不删老表、不写测试报告到生产、不把客户凭据写入 repo、不得自动部署生产。执行完成的含义是“代码、文档、隔离验收均完成，外部与生产项逐一明确状态”，不是所有平台自动支持后台定时任务。
