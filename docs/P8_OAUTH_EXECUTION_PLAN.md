# P8 · OAuth 2.1 MCP 升级：Codex 严格执行计划与 TODO

> 状态：**规划已优化，尚未实现**。目标：为已上线 P7 Streamable HTTP MCP 增加独立 OAuth 授权，完整保留共享 Bearer、REST、SQLite 报告数据和前端工作台。关联 [设计](./P8_OAUTH_ARCHITECTURE.md)、[迁移](./P8_OAUTH_MIGRATION.md)、[客户端](./P8_OAUTH_CLIENTS.md)。执行时同步更新这些文档和本清单，**每项验证前不得打勾**。

## 执行授权边界（优先于本文其他交付措辞）

- 用户已授权**本次文档优化提交到 master**；这不等于授权 Codex 后续自动创建、推送或合并任何代码提交。后续实现允许在本地工作树与本地分支持续推进，**远程 push、PR 创建、PR 合并、生产部署须另获明确授权**。如果已有独立有效授权，以最新明确指令为准。
- 主 Agent 对认证中间件、数据库迁移、token 生命周期、跨模块集成负最终责任；子 Agent 可并行开展规范研究、只读代码审计、前端独立组件、测试设计，但不能并行修改同一核心认证文件。
- 不使用真实 Grok/Claude 凭据或向生产发送测试报告。外部客户端验证需要用户登录/授权时，只标记 BLOCKED，不阻断隔离环境可继续的工作。
- 所有结论注明证据来源、日期、规范版本和实际测试结果。文档里的“OAuth 2.1”指目标安全基线，**不得将仍处于草案状态的内容宣称为已发布 RFC**。

## Codex 启动指令

读取 `README.md`、`docs/TECHNICAL_DESIGN.md`、`docs/P7_MCP_EXECUTION_PLAN.md`、`docs/P7_MCP_ACCEPTANCE.md` 和全部 P8 文档，从 `master` 的最新提交开始。你是主 Agent，允许在不冲突的设计/测试/前端子任务中使用副 Agent；核心认证和数据库迁移由主 Agent 统一审查整合。**先审计真实代码再定设计，不准按文档假定不存在的表和用户体系**。持续推进到验收报告完成；凡需生产密钥、外部平台登录或生产部署的任务标记 blocked，不要伪造通过。

## 主流 AI Agent 兼容性（新增强制目标）

> 执行前必须阅读 [P8 主流 AI Agent MCP 兼容性矩阵](./P8_MCP_CLIENT_COMPATIBILITY_MATRIX.md)。产品目标是让 Bot Dock 作为**独立通用 MCP Server**尽可能兼容 Grok、Codex、Cursor、Claude 等支持自定义远程 MCP 的客户端；不是 Codex 专用服务。标准协议通过与真实客户端联调通过必须分别验收。优先 Grok，随后 Codex、Cursor、Claude；不得因第三方平台能力限制虚报兼容。真实账号/授权受阻时记录 BLOCKED 并继续独立测试。

## P8 范围和放行标准

**必须**：MCP OAuth 发现、规范登录/Consent、Authorization Code + PKCE S256、受控 Client 预注册、资源绑定、最小 scope、Token 过期和撤销、旧 Bearer 兼容、单用户授权管理 UI、SQLite 可逆发布方案、安全与回归测试、第三方连接操作手册。

**条件范围**：CIMD（如客户端支持，应作为优先兼容能力），DCR（仅当真实客户端要求且经安全评估），Refresh Token（必要时加入安全轮换）；不需 JWT/JWKS、额外容器或多租户注册。禁止为追求“OAuth 2.1”标签发明标准、无条件开放注册或把共享管理员 Token 充作新 Client Secret。

**完成条件**：可验证的 OAuth 发现与 PKCE 授权、独立凭据下 `tools/list` 与读写、拒绝缺 scope/错误资源、撤销即时生效、现有 P7/REST 全量回归、可迁移可回滚、文档明确实测客户端范围。Grok 外部实测若受账户/授权限制，必须标记“待外部验收”；不因此宣称已经兼容。

## 阶段放行机制与证据要求

每阶段完成后在 `docs/P8_OAUTH_ACCEPTANCE.md` 记录：基线 SHA、实现文件、执行命令、测试输出摘要、失败项、风险、待用户决策、是否允许进入下一阶段。每个测试使用独立临时数据库/本地 HTTP 实例；未经验证不得勾选。

- **G0（审计）**：产出实际路由/认证/数据库/反代信任边界图，确认 SDK 与当前规范版本；现有测试基线可复现。
- **G1（协议设计）**：形成可运行的端点契约和客户端兼容性矩阵；Grok 接入要求分成“已核实”“未知需联调”，不能靠推测决定注册方案。
- **G2（认证核心）**：隔离环境 PKCE、授权码一次性消费、资源绑定、scope、Token 撤销及数据库重启持久性通过。
- **G3（集成 UI）**：OAuth 与旧 Bearer/REST 回归通过，授权确认、拒绝、撤销与浏览器安全验证通过。
- **G4（发布就绪）**：构建、Docker、迁移/回滚演练、攻击性测试、文档和限制全部有证据；外部 Grok 实测未完成时只能标注“待外部验收”，不能宣称已兼容 Grok。

## Phase 0：审计与冻结基线（禁止跳过）

- [ ] 检查 `internal/api` 的 Bearer、Origin、Cookie/CSRF、MCP 路由、SDK handler 和 shared service boundary。
- [ ] 检查 `internal/store` schema/migrations、SQLite 连接/事务/备份、SSE、报告来源与身份模型。
- [ ] 检查 `frontend` 现有“推送指令”和“MCP 接入”弹窗与设置风格。
- [ ] 记录 `go.mod` SDK v1.8.0 能否实现必要 OAuth 流程；确认 MCP 当期授权规范以及必要升级/升级回归。
- [ ] 基线：`go test ./...`、`cd frontend && npm ci && npm test && npm run build`；记 SHA 与可复现实测输出。
- [ ] 更新 P8 设计，明确当前系统无普通用户账号，仅存在管理员 ACCESS_TOKEN/浏览器会话，确定“单所有者授权”的 subject 模型。

- [ ] 输出 `docs/P8_BASELINE_AUDIT.md`：现有代码调用图、认证边界、SDK/规范版本、数据库 schema 版本、实际部署限制与兼容性风险；核对 OAuth 2.1 draft、MCP 当期授权规范及 RFC 9728/8414/7636/8707/7009 的具体适用要求。
- [ ] 对 Grok 做“需求证据表”：远程 MCP 地址、OAuth 客户端凭据输入项、支持的注册模式（预注册/CIMD/DCR）、精确 redirect URI、scope/resource 传参、是否支持刷新与定时任务；无法确认则标未知并保留人工联调步骤，不阻塞通用标准实现。
- [ ] 审计现有反向代理能否正确转发 well-known、/oauth/*、/mcp 及 WWW-Authenticate，避免 Host、X-Forwarded-Proto、路径前缀伪造；明确部署所需的公开 HTTPS origin。

## Phase 1：协议发现与受控客户端注册

- [ ] 明确稳定 `PUBLIC_BASE_URL` / issuer / MCP resource 配置和受信任反代规则，不由任意 Host 推断。
- [ ] Protected Resource Metadata：符合 RFC 9728 的路径发现（含路径型资源的规则），正确 HTTPS 资源标识/AS 列表。
- [ ] AS Metadata：符合 RFC 8414 的 issuer、authorize/token/revoke 等字段，宣告 PKCE S256。
- [ ] 401 `WWW-Authenticate` 按 MCP OAuth 标准携带 resource metadata 指引；现有 MCP 处理器及旧 Bearer 不回归。
- [ ] 手动预注册可信 OAuth Client，回调 URI 精确比对、client type/secret 存储保护；提供客户端信息展示与注册撤销流程。
- [ ] 确认 CIMD 的处理与第三方实际支持；如需 DCR，先安全评估并增加保护策略，禁止无限开放写入。

- [ ] 定义并测试 OAuth 授权服务器与资源服务器的**精确 URI 规则**：issuer、resource identifier、well-known 路径插入、redirect URI、trailing slash、URL 编码和 HTTPS；客户端使用标准发现而非硬编码私有端点。
- [ ] Client ID 及 Client Secret 的发放、展示、轮换、禁用分开设计；公开客户端不可强制要求保密的 secret，机密客户端应验证 secret；client secret 不可等同 ACCESS_TOKEN。
- [ ] CIMD 的 client_id URL 与元数据内容/域名绑定、redirect URI 匹配、缓存和重新验证策略须遵循当期规范；DCR 必须先通过滥用防护评审再开放，不得把任意外部 URL 当成可信元数据源。

## Phase 2：数据库 + 授权码与 Token

- [ ] 编写 additive schema-version 迁移：OAuth clients/grants/codes/access tokens/audit（按需 refresh）；索引/UTC/哈希存储。
- [ ] 登录后授权页面显示真实 client、scope、资源以及当前是全库读/写的安全边界，支持明确允许/拒绝。
- [ ] 授权端严格校验 response_type、registered redirect_uri、code_challenge_method=S256、scope、resource、登录状态与 anti-CSRF。
- [ ] 授权码短有效期、原子一次使用；Token endpoint 校验 PKCE、client/redirect/resource 绑定并返回标准错误。
- [ ] 不透明 access token 足够随机，只保存摘要，短生命周期，可撤销；如支持 refresh，必须 rotation+family 重放检测。
- [ ] 建立审计/限流/过期清理，日志/报错绝不输出凭据及报告全文。

- [ ] 明确浏览器授权安全模型：OAuth 客户端负责 state/回调关联校验；Bolt Dock 负责自身登录会话、授权同意操作的 CSRF 防护、精确 redirect URI 及 PKCE 校验；不得把客户端 state 校验错误地当作服务端可替代的 CSRF 防护。
- [ ] 授权码交换在 SQLite 事务中原子消费；并发双重兑换只能成功一次，错误 PKCE 不得导致可用授权码被不当消耗（按规范与安全策略确定），拒绝授权不得生成 token。
- [ ] Access/refresh token 生命周期列出 TTL、撤销传播、重启后持久性、清理时机和并发冲突；如果客户端需要 refresh token，则必须安全轮换与重放检测，禁止无限期无约束凭据。
- [ ] 客户端和授权记录在删除/禁用后应立即使相关 access/refresh token 无效；索引与事务测试确保小内存 SQLite 部署稳定。
- [ ] 迁移只新增 OAuth 结构，验证旧数据 count、FTS、Bot 身份、幂等索引不变；记录升级前备份及兼容式回滚方案，禁止自动恢复生产数据库。

## Phase 3：双认证与细粒度工具权限

- [ ] `/mcp` 接受原 Legacy Bearer 或新 OAuth access token，二者独立验证/不会相互升级权限；Cookie 始终无效。
- [ ] 形成明确 Principal/Scope/Resource context；`search_reports` 要求 `reports:read`；`publish_report` 要求 `reports:write`。
- [ ] OAuth Token 只能调用 MCP 工具，不得绕过 REST 写入、浏览器管理、删除 API；旧 REST 不改行为。
- [ ] source/bot identity、stable_task_id、external_id 幂等、SQLite/FTS/SSE 共用原服务，无复刻写入/检索业务。
- [ ] 401/403/工具权限错误一致；MCP SDK 的初始化和 tools/list 协议兼容。

- [ ] 形成按端点/工具的权限矩阵：Legacy ACCESS_TOKEN、OAuth read、OAuth write、OAuth read+write、浏览器 Cookie、匿名请求，逐项测试 200/401/403/工具错误的规范语义；不得出现 OAuth token 被 REST 管理接口接受的路径。
- [ ] 明确 MCP `tools/list` 可见性与调用权限策略，确保工具描述不会误导客户端；**真正权限在服务端 tools/call 时强制校验**，不可只靠隐藏按钮或工具描述。
- [ ] 检查 token confusion、authorization code 注入、重放、scope 升级、resource/audience 不匹配、登录会话固定及 SSRF，形成独立威胁清单。

## Phase 4：前端及客户端体验

- [ ] 沿用 Vue 工作台 UI 增加授权确认、已连接客户端/授权记录、按客户端撤销、错误/取消/过期交互。
- [ ] “MCP 接入”面板展示准确双认证能力，OAuth 未启用时不得出现可用的 OAuth 接入指导。
- [ ] README、TECHNICAL_DESIGN、客户端/安全/迁移文档与实际接口同步，不覆盖 P7 已验收说明。
- [ ] 仅测试环境或经过允许的外部客户端真实验证 Grok 与 Claude 的 OAuth 连接。Cookie/session 不可被 AI Client 复用。
- [ ] 验证 Codex CLI 既有 Bearer 配置和其他 P7 Inspector 场景。

- [ ] 授权页必须显著展示：客户端名称、准确 redirect 域名、请求的 read/write 权限、读权限可访问**整个单所有者报告库**、写权限可声明 source/bot_id；明确允许/拒绝，不默认勾选或自动授权。
- [ ] 授权页与已连接应用管理在手机/桌面、深浅主题、键盘操作下实测；撤销前确认、撤销后状态即时更新，过期/失败提示不泄露 token。
- [ ] 客户端接入说明优先 Grok，但只使用实际验证的回调 URI 和配置字段；若平台要求 OAuth client secret，明确安全发放方式；未知字段不编造。

## Phase 5：自动化安全回归（所有必须通过）

- [ ] Discovery 正常/坏 Host/代理头污染/HTTPS 假冒；跨源 Origin 和 CSRF；scope/resource/issuer 错误。
- [ ] PKCE 空/错误/plain 禁止、错误 redirect_uri、code 过期/重放、无效 client、用户拒绝。
- [ ] Access Token 过期/撤销、客户端撤销、只读尝试写入、只写尝试读取、OAuth token 不能调用 REST。
- [ ] 若提供 refresh：轮换后旧 token 重放导致 family 撤销；并发消费一致性。
- [ ] 如接入 CIMD/DCR：SSRF、redirect 限制、超时/大小/速率/恶意 metadata、匿名注册泛滥防护。
- [ ] 新旧认证并存、报告重复 external_id、中文搜索/游标、FTS、SSE、网页登录/注销、所有已有 Go/Vue 测试。
- [ ] SQLite schema 从旧版本升级、重复迁移、容器重启、备份恢复演练；不丢历史报告/Bot。
- [ ] `make test`、`make build`、Docker 构建与健康检查、镜像内运行 smoke test、资源实测。

- [ ] 增加独立 OAuth 协议测试客户端（不是 Codex 专属），完成 discovery→authorize→consent→token→tools/list→publish/search→revoke 全链路，使用本地测试数据库，不依赖真实第三方账户。
- [ ] 测试断电/重启、SQLite busy/锁竞争、并发兑换同一 code、并发 refresh 轮换、撤销与调用竞争；记录事务一致性和错误语义。
- [ ] 验证 Token/Client Secret 不会出现在前端静态资源、URL 查询日志、重定向泄露、错误栈、CI artifacts 或数据库明文字段中；必要时扫描提交内容。
- [ ] 将“真正多用户隔离”明确列为 P8 范围外：本期单所有者，不能将多用户隔离测试标为通过；可测试多个 OAuth 客户端彼此的凭据/授权独立性，但它不等于多租户数据隔离。

## Phase 6：交付策略和验收记录

- [ ] 按仓库 PR 保护策略准备本地分阶段提交与 CI 验证方案；**未经用户明确授权，不得推送远程提交、创建 PR 或合并**。获授权后执行相应远程步骤；不得以 docs-only push 误当作 GHCR 新版本已发布。
- [ ] 默认 OAuth 功能关闭；不开启生产 OAuth/不触碰真实生产 DB，除非用户明确授权。
- [ ] 更新 `docs/P8_OAUTH_ACCEPTANCE.md`：逐项日期、提交 SHA、测试命令/结果、真实客户端版本、阻塞、部署风险、回滚演练。
- [ ] 文档 README 仅宣称已经完成且验证的功能；未验证项必须明确标记。
- [ ] 结束时报告变更清单、迁移说明、权限与安全矩阵、Legacy 回归、客户端接入步骤、已知限制和下一步生产发布手册。

## 统一停止/决策策略

如需要真实 OAuth client 注册资料、Grok/Claude 登录、平台外网授权或生产访问，只将该项标注 BLOCKED，完成其他独立任务；**不使用假成功替代真实联调**。不擅自增加多用户账户体系、不改变 `ACCESS_TOKEN`、不删老表、不写测试报告到生产、不把客户凭据写入 repo、不得自动部署生产。执行完成的含义是“代码、文档、隔离验收均完成，外部与生产项逐一明确状态”，不是所有平台自动支持后台定时任务。
