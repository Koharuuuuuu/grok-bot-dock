# P8 · OAuth 2.1 MCP 升级：Codex 严格执行计划与 TODO

> 状态（2026-10-11）：**P8 受限 OAuth 授权已实现，PR #19 已合入 `master`（`76cc117`），CI 及 GHCR 多架构发布均通过；镜像仍默认关闭 OAuth。** 本地/CI/镜像隔离验收完成；真实第三方客户端和生产环境验证仍待外部验收。OAuth 2.1 draft-16 与 MCP Authorization 的完整符合性不作声明；共享 `ACCESS_TOKEN` 继续被 `/mcp` 接受是明确的 P7 兼容例外。证据见 [验收记录](./P8_OAUTH_ACCEPTANCE.md)。目标：为已上线 P7 Streamable HTTP MCP 增加独立 OAuth 授权，保留旧 Bearer、REST、SQLite 报告数据和前端工作台。关联 [设计](./P8_OAUTH_ARCHITECTURE.md)、[迁移](./P8_OAUTH_MIGRATION.md)、[客户端](./P8_OAUTH_CLIENTS.md)。**每项验证前不得打勾**。

## 执行授权边界（优先于本文其他交付措辞）

- 用户于 2026-10-11 明确要求完成全部剩余工作并在可部署时通知；本次据此 push P8、创建并合并 PR #19、等待 CI 与 GHCR 发布。该授权不包含操作用户的生产服务器；生产部署由用户发起。
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
- [x] 对 Grok 做“需求证据表”：2026-10-10 官方文档现已确认 Grok.com Custom MCP、公网 URL、OAuth/API-key 认证以及 Grok CLI 远程 HTTP/浏览器 OAuth；Client registration、redirect URI、scope/resource、DCR/CIMD 和续期细节仍未知，保留真实客户端联调步骤，不编造配置字段。
- [x] 审计 checkout 的反向代理限制与信任边界；记录实际生产反代配置不可见，避免 Host、X-Forwarded-Proto、路径前缀推断；明确启用所需的公开 HTTPS origin。

## Phase 1：协议发现与受控客户端注册

- [x] 本地明确并测试固定 `PUBLIC_BASE_URL`、issuer、MCP resource 和 Host/forwarded-header 隔离；真实生产反代拓扑仍需部署前验证（见 G4）。
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
- [x] 授权端严格校验 response_type、registered redirect_uri（唯一注册回调允许省略并推断；多回调必须精确选择）、code_challenge_method=S256、scope、resource、登录状态与 anti-CSRF。
- [x] 授权码短有效期、原子一次使用；Token endpoint 校验 PKCE、client/redirect/resource 绑定并返回标准错误。
- [x] 不透明 access token 足够随机，只保存摘要，短生命周期，可撤销；本期不提供 refresh token。
- [x] 建立审计/限流/过期清理，日志/报错绝不输出凭据及报告全文。

- [x] 明确浏览器授权安全模型：OAuth 客户端负责 state/回调关联校验；Bolt Dock 负责自身登录会话、授权同意操作的 CSRF 防护、精确 redirect URI 及 PKCE 校验；不得把客户端 state 校验错误地当作服务端可替代的 CSRF 防护。
- [x] 授权码交换在 SQLite 事务中原子消费；并发双重兑换只能成功一次，错误 PKCE 不得导致可用授权码被不当消耗（按规范与安全策略确定），拒绝授权不得生成 token。
- [x] OAuth 2.1 draft-16 边界审计：唯一回调下授权及 Token 请求省略/空值 `redirect_uri` 按省略处理；若 Token 请求提供则仍精确比对授权码绑定值；定义参数重复拒绝、未知扩展参数忽略、Basic 凭据表单解码与匹配认证挑战；授权响应及 Token endpoint 错误码区分缺参/不支持值，Resource Indicator 使用 `invalid_target`；state 若出现（包括空值）在成功/错误回调中原样返回，超长 state 不反射。
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
- [x] Codex CLI 既有 Bearer 验收见 [P7 验收报告](./P7_MCP_ACCEPTANCE.md)；MCP Inspector CLI 2.10.1 已连接当前 P8 分支的隔离服务，完成 tools/list 与只读 search_reports 调用（见 P8 验收记录）。Codex OAuth 仍待真实客户端联调。

- [x] 授权页必须显著展示：客户端名称、准确 redirect 域名、请求的 read/write 权限、读权限可访问**整个单所有者报告库**、写权限可声明 source/bot_id；明确允许/拒绝，不默认勾选或自动授权。
- [x] 本地 Chromium 覆盖桌面与 390×844 窄视口、浅色/深色/跟随系统；授权同意页可用 Tab/Space 完成 scope 选择并到达允许/拒绝，客户端停用有确认并即时更新列表状态。
- [ ] 实际手机设备仍需补验；本机无 `simctl` 或 `adb` 可用设备。
- [x] 过期/失败提示不泄露凭据：登录提示仅基于 HTTP 状态；前端 Vitest 覆盖登录失败/会话过期消息，OAuth API 回归断言认证失败不回显 client secret、授权码或 verifier。
- [x] 本地 Chromium 完整遍历 MCP 管理面板的可交互键盘焦点；键盘登记隔离客户端、触发停用确认并核实停用状态，证据见验收报告。实体移动设备仍未验证。
- [x] 客户端接入说明优先 Grok，记录官方已证实的自定义 MCP/OAuth 能力；不编造未公开的回调 URI、client registration 字段或 secret 规则。真实平台回调/字段仍标为待外部联调，没有声称已验证。

## Phase 5：自动化安全回归（所有必须通过）

- [x] 自动化覆盖正常 discovery、坏 Host/代理头、跨源 Origin/CSRF、scope/resource/issuer 错误；固定公网 HTTPS origin 与真实证书/反代假冒仍待部署验收。
- [x] PKCE 空/错误/plain 禁止、错误 redirect_uri、code 过期/重放、无效 client、用户拒绝。
- [x] Access Token 过期/撤销、客户端撤销、只读尝试写入、只写尝试读取、OAuth token 不能调用 REST。
- [x] 若提供 refresh：不适用，本期不签发 refresh token。
- [x] CIMD 元数据获取与授权路径已覆盖 SSRF、redirect、大小/类型、client_id 和 redirect URI 校验；DCR 未实现且未开放匿名注册。
- [x] 新旧认证并存、报告重复 external_id、中文搜索/游标、FTS、SSE、网页登录/注销、所有已有 Go/Vue 测试。
- [x] 隔离 SQLite 覆盖 v1→当前、v2→v3、重复启动迁移、历史数据与备份恢复；临时 Docker 命名卷在 graceful restart 与 SIGKILL 后报告均可读，容器/卷随后删除。
- [x] `make test`、`make build`、Docker 构建与健康检查、镜像内运行 smoke test、资源实测。

- [x] 增加独立 OAuth 协议测试客户端（不是 Codex 专属），完成 discovery→authorize→consent→token→tools/list→publish/search→revoke 全链路，使用本地测试数据库，不依赖真实第三方账户。
- [x] 本地验证重启/强制进程终止、SQLite busy/锁竞争、同一 code 并发兑换和撤销竞态，并记录事务与错误语义；真实主机断电未模拟，已开始执行的 MCP 工具调用不会被撤销主动中断。
  - [x] 容器重启后确认持久卷中的报告仍可读取；实际断电未模拟。
  - [x] 外部 SQLite writer lock 阻塞写入、释放锁后写入恢复；并发双重兑换仅成功一次。
  - [x] 并发 Token 查验/撤销竞态完成后，后续查验拒绝被撤销 Token；已签发的 in-flight 工具调用不会被主动取消。
  - [x] Refresh rotation 不适用：本期不签发 refresh token。
- [x] 本地检查嵌入静态资源和容器日志不含临时 token/报告正文标记；HTTP logger 只写 URL path，Store 测试断言 token/secret 仅存摘要。
- [x] 当前 P8 commit 的 PR CI、合并后 `master` CI 和 GHCR 多架构镜像发布已通过；匿名 registry manifest 可读取，arm64 镜像已拉取并做隔离 OAuth discovery/MCP challenge smoke。生产日志须在部署后由部署环境检查。
- [x] 将“真正多用户隔离”明确列为 P8 范围外：本期单所有者，不能将多用户隔离测试标为通过；本次不把多个客户端的存在宣称为多租户数据隔离。

## Phase 6：交付策略和验收记录

- [x] P8 功能经 PR #19 合并至 `master`；合并后 CI 和多架构 GHCR 发布通过。生产部署尚未执行，也不把镜像发布称为生产验收。
- [x] 默认 OAuth 功能关闭；未开启生产 OAuth/未触碰真实生产 DB。
- [x] 更新 `docs/P8_OAUTH_ACCEPTANCE.md`：记录日期、基线 SHA、实现提交 SHA、测试命令/结果、客户端阻塞、部署风险和隔离备份恢复演练。
- [x] 文档 README 仅宣称已经完成且验证的功能；未验证项明确标记。
- [x] 结束时报告变更清单、迁移说明、权限与安全矩阵、Legacy 回归、客户端接入步骤、已知限制和下一步生产发布手册（见验收文档及本次交付摘要）。

## 统一停止/决策策略

如需要真实 OAuth client 注册资料、Grok/Claude 登录、平台外网授权或生产访问，只将该项标注 BLOCKED，完成其他独立任务；**不使用假成功替代真实联调**。不擅自增加多用户账户体系、不改变 `ACCESS_TOKEN`、不删老表、不写测试报告到生产、不把客户凭据写入 repo、不得自动部署生产。执行完成的含义是“代码、文档、隔离验收均完成，外部与生产项逐一明确状态”，不是所有平台自动支持后台定时任务。
