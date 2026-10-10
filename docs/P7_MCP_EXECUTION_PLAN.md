# P7 · MCP 原生接入：技术设计与 Codex 执行清单

> 状态：**待执行，所有验收项初始为 [ ]**。仓库：`Nyakooo/grok-bot-dock`，默认分支 `master`。P7 在 P6 已修复功能基础上增量实现，不能回退 P6。产品当前界面名称为 Bolt Dock（后续品牌更名不属于本任务）。日期：2026-10-10。

## 1. 目标、边界与产品价值

- 在**现有 Go HTTP 服务**增加远程 MCP Server，推荐入口 `/mcp`，采用 **Streamable HTTP**，允许支持远程 MCP 的 AI 客户端发现工具、发布最终报告、查询历史报告。
- **REST API 继续是核心**：`POST /api/v1/reports` 及现有阅读、搜索、删除、认证行为不变。MCP 与 REST 共享同一服务层/存储层、校验、幂等、SSE 事件和 SQLite 数据。
- **不增加独立 MCP 容器、Redis、消息队列或 Node 常驻进程**；继续 Go + Vue + SQLite 单镜像、`/data` 持久化、现有 18765 端口和 HTTPS 反向代理。
- MVP 仅开放 2 个工具：`publish_report`（写）与 `search_reports`（读）。后续扩展 `get_report`、`list_reports`；**不开放删除 Bot、删除报告、修改名称等危险操作**。
- 不承诺所有平台都支持远程 MCP 或自动调用；兼容性以真实客户端测试为准。MCP 不是性能优化，报告正文/工具响应仍消耗客户端模型上下文。

## 2. 先审计再实施（禁止假定接口）

- [ ] 核查 master 当前 Go 路由、认证中间件、报告写入与检索服务、FTS5 查询、SSE、大小限制、日志、Dockerfile、Compose、反向代理说明、现有测试。
- [ ] 记录现有 REST 字段与状态码，特别是 `source` 缺失归类 `unknown`、`stable_task_id` 的确定性 Bot ID 派生、`external_id` 幂等、`bot_name` 回退、`executed_at` RFC3339。
- [ ] 先确认所选 MCP Go SDK 的**官方维护状态、协议版本、许可证和 Go 1.25 兼容性**；优先成熟官方 SDK，避免自行实现协议解析。锁定依赖版本，不凭空宣称支持特定客户端。
- [ ] 在技术设计中写明 MCP 工具参数到既有 REST 服务入参的映射和错误语义；避免双份业务逻辑。

## 3. 协议与传输

- [ ] 在原 Go HTTP mux 注册 `/mcp` Streamable HTTP handler，正确处理 MCP 初始化、协议协商、工具列表和工具调用；遵循所选 SDK/协议版本的 HTTP 方法、`Accept`、`Content-Type`、会话与错误约定。
- [ ] MVP 优先选择 SDK 支持的**无状态 HTTP 模式**，仅在真实客户端兼容性要求下增加服务端会话；不要强行自行模拟 SSE、会话 ID 或 JSON-RPC。
- [ ] 兼容代理后的 HTTPS、请求取消、超时、请求体上限、并发；拒绝未认证访问；响应中不得泄露 Token、数据库路径或内部堆栈。
- [ ] `/healthz` 和原有 REST/SSE 路由行为保持不变；前端静态资源/SPA fallback 不得吞掉 `/mcp`。
- [ ] 协议级测试覆盖初始化、tools/list、tools/call、无效 JSON-RPC、错误 HTTP 方法/Accept、超限请求、连接中断、并发调用；以 SDK 规范为准，避免自创状态码。

## 4. 安全与授权

- [ ] `/mcp` 必须强制认证。MVP 可以复用现有 **Bearer ACCESS_TOKEN**（明确标记为单用户共享密钥模式），绝不能因为 Web 登录 Cookie 有效就自动开放跨站 MCP；不要把 Token 放在 URL、报告内容、示例仓库、日志或前端代码。
- [ ] 为未来多用户/第三方公开接入预留权限边界：本期仅私有自托管，**不宣称符合 OAuth 受保护资源标准**。如果选定客户端或部署要求 OAuth，作为独立后续阶段实现完整 OAuth 资源服务器发现与授权，不要假造半套流程。
- [ ] 校验 HTTP Origin：对浏览器跨源请求采用明确允许策略，拒绝恶意/未知 Origin；无 Origin 的合法服务端客户端可按 Bearer 认证策略处理。不得配置 `Access-Control-Allow-Origin: *` 加凭据。
- [ ] 推荐公网仅 HTTPS、反向代理限流与可选 IP 限制；保护写操作和搜索查询的请求大小、速率及响应条数，避免被刷写和 FTS 大查询耗尽 2 vCPU / 1.6GiB 服务器。
- [ ] 日志仅记录必要元信息和匿名化错误，不打印 Authorization、报告正文或完整搜索内容；测试覆盖未授权访问和跨源拒绝。
- [ ] MCP 工具注释应准确描述读写/副作用；AI 调用策略由客户端决定，**不能保证 Agent 只在最终报告时调用**，服务端依赖鉴权、幂等、校验和限流兜底。

## 5. 工具一：publish_report（P0）

- [ ] 注册 `publish_report`，提供明确名称、描述、JSON Schema、必填字段与长度限制。描述中强调“**仅发布已完成、确认的正式报告**；不要提交草稿、调试日志、中间结果或失败任务”，但不要将自然语言提示视作安全边界。
- [ ] 字段与现有 REST 契约对齐：`executed_at`（RFC3339，必填）、`content`（完整 Markdown，必填）、`source`（可选，默认 unknown）、`bot_id` 或 `stable_task_id`（至少一种可靠稳定身份）、`bot_name`、`task_name`、`title`、`summary`、`tags`、`external_id`、`avatar_url`（按既有 HTTPS 规则）。
- [ ] 同一个 `stable_task_id` 在相同 source 下始终映射同一 Bot；不允许按时间随机生成身份。没有稳定身份返回清晰参数错误，**不能默默生成新 Bot**。
- [ ] `external_id` 同一次正式执行及重试必须一致；沿用 REST 幂等约束，重复提交不新增报告或 SSE 通知。不要为缺失 external_id 的请求擅自制造可能跨执行冲突的 ID。
- [ ] 复用 REST 的服务层校验与数据库事务；成功工具响应结构化返回 `report_id`、`created` / `duplicate`、`source`、`bot_id`、`executed_at` 等安全字段，**不返回整个报告正文**；可同时提供简洁文本供不同客户端读取。
- [ ] 参数错误与业务失败返回 MCP 兼容的工具错误（必要时 isError=true），认证/传输错误按 HTTP/SDK 规范处理；不得把 400/401/500 当成发布成功。

## 6. 工具二：search_reports（P1）

- [ ] 注册只读 `search_reports`，参数建议 `query`（必填非空）、`source`、`bot_id`、`from`、`to`、`limit`、`cursor`；复用现有 FTS5 检索、筛选、分页与安全查询，不另写 SQL 检索引擎。
- [ ] 设合理默认 limit（建议 10）和硬上限（建议 20），返回匹配项的 `report_id`、`source`、`bot_id`、展示名称、标题、摘要、执行时间及分页游标。默认不返回完整 Markdown，以控制 Token/响应体积。
- [ ] 日期、游标、空结果、特殊字符、中文、跨来源同名 Bot、权限与限流有测试；查询不能修改已读状态或其他数据。
- [ ] 检索无结果时返回空数组，不虚构报告；展示名称沿用 SQLite display_name 优先规则。

## 7. 后续工具（本阶段不作为上线阻塞）

- [ ] **设计但暂不默认开放** `get_report(report_id)`：返回指定报告的完整 Markdown，增加长度限制/截断说明，防止超大内容占满模型上下文。
- [ ] **设计但暂不默认开放** `list_reports`：按 source/Bot/日期分页列出历史报告，限制每页条数。
- [ ] 不实现 delete、rename、管理凭据、任意 SQL、任意 URL 抓取或远程命令执行工具。

## 8. 部署与使用文档

- [ ] README 新增“远程 MCP 接入”章节：`https://YOUR_DOMAIN/mcp`、Streamable HTTP、Bearer Token 在客户端安全配置、HTTPS/反向代理前提、与 REST 的差异。
- [ ] 提供至少一个**真实测试过**的 Codex MCP 客户端配置/接入步骤（按当前版本 CLI/桌面能力核实，不编造配置键名）；另选一个支持远程 MCP 的客户端验证；无法验证则标注限制。
- [ ] 给出 MCP 工具示例：一份最终报告发布和一次中文历史检索；仅使用占位符，不包含真实 ACCESS_TOKEN；说明 MCP 客户端能发现工具不等于自动按时调用，定时推送仍推荐 REST。
- [ ] 更新 `docs/TECHNICAL_DESIGN.md`、`docs/EXECUTION_PLAN.md` 和必要的运维/反向代理说明；保持 Compose 单容器部署、GHCR 镜像路径和 `/data` 不变。
- [ ] 明确默认公网暴露的安全影响；如引入 `MCP_ENABLED` 开关，记录默认值、兼容性和健康检查行为，避免旧部署意外开放额外入口。

## 9. 验收矩阵（不得跳过）

- [ ] A. 无凭据/错误 Token 请求 MCP 均拒绝；有效 Token 可初始化、列出工具；跨源恶意 Origin 拒绝。
- [ ] B. MCP 发布新报告 → 原有 Web UI 出现 Bot 与报告 → SSE 正常更新 → REST 可读取。
- [ ] C. 使用相同 `external_id` 重试 → 不重复写入、不重复通知；使用相同 stable_task_id 连续两次不同执行 → 同一 Bot 下两份报告。
- [ ] D. REST 发布 → MCP 搜索能查到；MCP 发布 → REST 搜索能查到；中文、日期、来源和跨 Bot 查询正确。
- [ ] E. 工具参数错误、空查询、超大正文、超限 limit、并发、断线、非法请求都安全失败，日志无敏感数据。
- [ ] F. Go 单元/集成测试、`make test`、`make build`、Docker 构建、容器 `/healthz`、SQLite 数据持久化均通过；记录资源占用的真实测量。
- [ ] G. 真实 Codex MCP 客户端完成 tools/list 与 publish_report、search_reports；第二个客户端如未验证必须说明，不得声称兼容。
- [ ] H. P6 核心回归：网页登录刷新、详情无抖动/清晰、字号与最大化偏好、Bot 重命名、REST 推送和 GHCR 发布流程不退化。
- [ ] I. 在 `docs/P7_MCP_ACCEPTANCE.md` 记录每项结果、测试命令、代码文件、真实客户端版本、截图/日志路径、CI run、镜像 digest、已知限制。未测即 [ ]。

## 10. 推荐执行顺序与提交策略

1. **Phase 0 审计/设计**：检查当前仓库、选定 SDK 与协议，确认共享服务边界，提交技术设计。
2. **Phase 1 安全传输**：Go 内嵌 `/mcp`，初始化/tools/list、认证、Origin、限流与协议测试。
3. **Phase 2 发布工具**：`publish_report` 复用 REST 写入/幂等，Web/SSE 双向验证。
4. **Phase 3 检索工具**：`search_reports` 复用现有查询层，验证中文与分页。
5. **Phase 4 客户端与交付**：真实 Codex 连接、另一客户端可用性测试、文档、Docker/CI、P6 回归和验收报告。

按仓库保护规则通过 PR 合并至 `master`；不要直接在生产数据库上跑破坏性测试。若遇到需要用户凭据、外网部署或授权的步骤，记录阻塞并继续完成其他可独立验证的工作。**不能因为代码可编译就宣布 P7 完成。**
