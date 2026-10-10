# P8 · OAuth 2.1 MCP 升级：Codex 严格执行计划与 TODO

> 状态（2026-10-10）：**P8 本地分支已实现受限 OAuth 授权配置；CIMD 支持及隔离测试现已加入，仍未达到完整 OAuth 2.1 符合性验收。生产部署与真实第三方 OAuth 联调仍待外部验收。** 当前证据和边界见 [验收记录](./P8_OAUTH_ACCEPTANCE.md)。实现尚未推送、创建 PR 或合并。目标：为已上线 P7 Streamable HTTP MCP 增加独立 OAuth 授权，完整保留共享 Bearer、REST、SQLite 报告数据和前端工作台。关联 [设计](./P8_OAUTH_ARCHITECTURE.md)、[迁移](./P8_OAUTH_MIGRATION.md)、[客户端](./P8_OAUTH_CLIENTS.md)。**每项验证前不得打勾**。

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

- [x] 检查 `internal/api` 的 Bearer、Origin、Cookie/CSRF、MCP 路由、SDK handler 和 shared service boundary。
- [x] 检查 `internal/store` schema/migrations、SQLite 连接/事务/备份、SSE、报告来源与身份模型。
- [x] 检查 `frontend` 现有“推送指令”和“MCP 接入”弹窗与设置风格。
- [x] 记录 `go.mod` SDK v1.8.0 能否实现必要 OAuth 流程；确认 MCP 当期授权规范以及必要升级/升级回归。
- [x] 基线：`go test ./...`、`cd frontend && npm ci && npm test && npm run build`；记 SHA 与可复现实测输出。
- [x] 更新 P8 设计，明确当前系统无普通用户账号，仅存在管理员 ACCESS_TOKEN/浏览器会话，确定“单所有者授权”的 subject 模型。

- [x] 输出 `docs/P8_BASELINE_AUDIT.md`：现有代码调用图、认证边界、SDK/规范版本、数据库 schema 版本、实际部署限制与兼容性风险；核对 OAuth 2.1 draft、MCP 当期授权规范及 RFC 9728/8414/7636/8707/7009 的具体适用要求。
- [x] 对 Grok 做“需求证据表”：远程 MCP 地址、OAuth 客户端凭据输入项、支持的注册模式（预注册/CIMD/DCR）、精确 redirect URI、scope/resource 传参、是否支持刷新与定时任务；无法确认则标未知并保留人工联调步骤，不阻塞通用标准实现。
- [x] 审计 checkout 的反向代理限制与信任边界；记录实际生产反代配置不可见，避免 Host、X-Forwarded-Proto、路径前缀推断；明确启用所需的公开 HTTPS origin。

## Phase 1：协议发现与受控客户端注册

- [ ] 明确稳定 `PUBLIC_BASE_URL` / issuer / MCP resource 配置和受信任反代规则，不由任意 Host 推断。
- [x] Protected Resource Metadata：符合 RFC 9728 的路径发现（含路径型资源的规则），正确 HTTPS 资源标识/AS 列表。
- [x] AS Metadata：符合 RFC 8414 的 issuer、authorize/token/revoke 等字段，宣告 PKCE S256。
- [x] 401 `WWW-Authenticate` 按 MCP OAuth 标准携带 resource metadata 指引；现有 MCP 处理器及旧 Bearer 不回归。
- [x] 手动预注册可信 OAuth Client，回调 URI 精确比对、client type/secret 存储保护；提供客户端信息展示与注册撤销流程。
- [x] 实现并隔离测试 CIMD 客户端元数据处理；真实第三方客户端对 CIMD 的支持仍待联调。如需 DCR，先安全评估并增加保护策略，禁止无限开放写入。

- [x] 定义并测试 OAuth 授权服务器与资源服务器的**精确 URI 规则**：issuer、resource identifier、well-known 路径插入、redirect URI、trailing slash、URL 编码和 HTTPS；客户端使用标准发现而非硬编码私有端点。
- [x] Client ID 及 Client Secret 的发放、展示、轮换、禁用分开设计；公开客户端不可强制要求保密的 secret，机密客户端应验证 secret；client secret 不可等同 ACCESS_TOKEN。
- [x] CIMD 的 client_id URL、元数据 client_id、redirect URI、受限缓存和重新验证策略已实现并由隔离测试覆盖；当期草案的独立符合性审查和第三方互操作仍未验收。DCR 必须先通过滥用防护评审再开放，不得把任意外部 URL 当成可信元数据源。

## Phase 2：数据库 + 授权码与 Token

- [x] 编写 additive schema-version 迁移：OAuth clients/grants/codes/access tokens/audit（按需 refresh）；索引/UTC/哈希存储。
- [x] 登录后授权页面显示真实 client、scope、资源以及当前是全库读/写的安全边界，支持明确允许/拒绝。
- [x] 授权端严格校验 response_type、registered redirect_uri、code_challenge_method=S256、scope、resource、登录状态与 anti-CSRF。
- [x] 授权码短有效期、原子一次使用；Token endpoint 校验 PKCE、client/redirect/resource 绑定并返回标准错误。
- [x] 不透明 access token 足够随机，只保存摘要，短生命周期，可撤销；本期不提供 refresh token。
- [x] 建立审计/限流/过期清理，日志/报错绝不输出凭据及报告全文。

- [x] 明确浏览器授权安全模型：OAuth 客户端负责 state/回调关联校验；Bolt Dock 负责自身登录会话、授权同意操作的 CSRF 防护、精确 redirect URI 及 PKCE 校验；不得把客户端 state 校验错误地当作服务端可替代的 CSRF 防护。
- [x] 授权码交换在 SQLite 事务中原子消费；并发双重兑换只能成功一次，错误 PKCE 不得导致可用授权码被不当消耗（按规范与安全策略确定），拒绝授权不得生成 token。
- [x] Access token 生命周期、15 分钟 TTL、撤销传播、重启后持久性和授权时清理已记录并测试；本期不提供 refresh token。
- [x] 客户端禁用会立即使相关授权及 access token 无效；覆盖索引与事务测试。
- [x] 迁移只新增 OAuth 结构，验证旧数据 count、FTS、Bot 身份、幂等索引不变；记录升级前备份及兼容式回滚方案，禁止自动恢复生产数据库。

## Phase 3：双认证与细粒度工具权限

- [x] `/mcp` 接受原 Legacy Bearer 或新 OAuth access token，二者独立验证/不会相互升级权限；Cookie 始终无效。
- [x] 形成明确 Principal/Scope/Resource context；`search_reports` 要求 `reports:read`；`publish_report` 要求 `reports:write`。
- [x] OAuth Token 只能调用 MCP 工具，不得绕过 REST 写入、浏览器管理、删除 API；旧 REST 不改行为。
- [x] source/bot identity、stable_task_id、external_id 幂等、SQLite/FTS/SSE 共用原服务，无复刻写入/检索业务。
- [x] 401/403/工具权限错误一致；MCP SDK 的初始化和 tools/list 协议兼容。

- [x] 形成并测试 MCP 工具权限矩阵：Legacy ACCESS_TOKEN、OAuth read、OAuth write、OAuth read+write、浏览器 Cookie、匿名/无效 Bearer；结合 REST 认证矩阵验证 OAuth token 不会被 REST 接受。
- [x] 明确 MCP `tools/list` 可见性与调用权限策略，确保工具描述不会误导客户端；**真正权限在服务端 tools/call 时强制校验**，不可只靠隐藏按钮或工具描述。
- [x] 检查 token confusion、authorization code 注入、重放、scope 升级、resource/audience 不匹配、登录会话固定及 SSRF，形成独立威胁清单。剩余撤销/调用竞态和部署边界列入威胁清单。

## Phase 4：前端及客户端体验

- [x] 沿用 Vue 工作台 UI 增加授权确认、已连接客户端/授权记录、按客户端撤销、错误/取消/过期交互。
- [x] “MCP 接入”面板展示准确双认证能力，OAuth 未启用时不得出现可用的 OAuth 接入指导。
- [x] README、TECHNICAL_DESIGN、客户端/安全/迁移文档与实际接口同步，不覆盖 P7 已验收说明。
- [ ] 仅测试环境或经过允许的外部客户端真实验证 Grok 与 Claude 的 OAuth 连接。Cookie/session 不可被 AI Client 复用。
- [ ] 验证 Codex CLI 既有 Bearer 配置和其他 P7 Inspector 场景。

- [x] 授权页必须显著展示：客户端名称、准确 redirect 域名、请求的 read/write 权限、读权限可访问**整个单所有者报告库**、写权限可声明 source/bot_id；明确允许/拒绝，不默认勾选或自动授权。
- [x] 本地 Chromium 覆盖桌面与 390×844 窄视口、浅色/深色/跟随系统；授权同意页可用 Tab/Space 完成 scope 选择并到达允许/拒绝，客户端停用有确认并即时更新列表状态。
- [ ] 实际手机设备与完整管理页键盘操作仍需补验；过期/失败提示不得泄露 token 的自动化覆盖继续按 Phase 5 验收。
- [x] 客户端接入说明优先 Grok，但只使用实际验证的回调 URI 和配置字段；若平台要求 OAuth client secret，明确安全发放方式；未知字段不编造。真实平台回调/字段仍标为待外部联调，没有声称已验证。

## Phase 5：自动化安全回归（所有必须通过）

- [ ] Discovery 正常/坏 Host/代理头污染/HTTPS 假冒；跨源 Origin 和 CSRF；scope/resource/issuer 错误。
- [x] PKCE 空/错误/plain 禁止、错误 redirect_uri、code 过期/重放、无效 client、用户拒绝。
- [x] Access Token 过期/撤销、客户端撤销、只读尝试写入、只写尝试读取、OAuth token 不能调用 REST。
- [x] 若提供 refresh：不适用，本期不签发 refresh token。
- [x] CIMD 元数据获取与授权路径已覆盖 SSRF、redirect、大小/类型、client_id 和 redirect URI 校验；DCR 未实现且未开放匿名注册。
- [x] 新旧认证并存、报告重复 external_id、中文搜索/游标、FTS、SSE、网页登录/注销、所有已有 Go/Vue 测试。
- [ ] SQLite schema 从旧版本升级、重复迁移、容器重启、备份恢复演练；不丢历史报告/Bot。
- [x] `make test`、`make build`、Docker 构建与健康检查、镜像内运行 smoke test、资源实测。

- [x] 增加独立 OAuth 协议测试客户端（不是 Codex 专属），完成 discovery→authorize→consent→token→tools/list→publish/search→revoke 全链路，使用本地测试数据库，不依赖真实第三方账户。
- [ ] 测试断电/重启、SQLite busy/锁竞争、并发兑换同一 code、并发 refresh 轮换、撤销与调用竞争；记录事务一致性和错误语义。
  - [x] 容器重启后确认持久卷中的报告仍可读取；实际断电未模拟。
  - [x] 外部 SQLite writer lock 阻塞写入、释放锁后写入恢复；并发双重兑换仅成功一次。
  - [x] 并发 Token 查验/撤销竞态完成后，后续查验拒绝被撤销 Token；已签发的 in-flight 工具调用不会被主动取消。
  - [x] Refresh rotation 不适用：本期不签发 refresh token。
- [ ] 验证 Token/Client Secret 不会出现在前端静态资源、URL 查询日志、重定向泄露、错误栈、CI artifacts 或数据库明文字段中；必要时扫描提交内容。
- [x] 将“真正多用户隔离”明确列为 P8 范围外：本期单所有者，不能将多用户隔离测试标为通过；本次不把多个客户端的存在宣称为多租户数据隔离。

## Phase 6：交付策略和验收记录

- [ ] 按仓库 PR 保护策略准备本地分阶段提交与 CI 验证方案；**未经用户明确授权，不得推送远程提交、创建 PR 或合并**。获授权后执行相应远程步骤；不得以 docs-only push 误当作 GHCR 新版本已发布。
- [x] 默认 OAuth 功能关闭；未开启生产 OAuth/未触碰真实生产 DB。
- [x] 更新 `docs/P8_OAUTH_ACCEPTANCE.md`：记录日期、基线 SHA、实现提交 SHA、测试命令/结果、客户端阻塞、部署风险和隔离备份恢复演练。
- [x] 文档 README 仅宣称已经完成且验证的功能；未验证项明确标记。
- [x] 结束时报告变更清单、迁移说明、权限与安全矩阵、Legacy 回归、客户端接入步骤、已知限制和下一步生产发布手册（见验收文档及本次交付摘要）。

## 统一停止/决策策略

如需要真实 OAuth client 注册资料、Grok/Claude 登录、平台外网授权或生产访问，只将该项标注 BLOCKED，完成其他独立任务；**不使用假成功替代真实联调**。不擅自增加多用户账户体系、不改变 `ACCESS_TOKEN`、不删老表、不写测试报告到生产、不把客户凭据写入 repo、不得自动部署生产。执行完成的含义是“代码、文档、隔离验收均完成，外部与生产项逐一明确状态”，不是所有平台自动支持后台定时任务。
