# P7 · MCP 原生接入：技术设计与 Codex 执行清单

> 状态：**P7 实现及页面 Codex 一键复制、PR 文档维护、布局回归修复、PR/master Actions、GHCR 多架构匿名拉取、本机 Codex 与 MCP Inspector 验收已完成；P6 全量设备矩阵和生产部署未验证**。逐项结果见 [P7 验收报告](./P7_MCP_ACCEPTANCE.md)。仓库：`Nyakooo/grok-bot-dock`，默认分支 `master`。P7 在 P6 已修复功能基础上增量实现，不能回退 P6。产品当前界面名称为 Bolt Dock（后续品牌更名不属于本任务）。日期：2026-10-10。

## 1. 目标、边界与产品价值

- 在**现有 Go HTTP 服务**增加远程 MCP Server，推荐入口 `/mcp`，采用 **Streamable HTTP**，允许支持远程 MCP 的 AI 客户端发现工具、发布最终报告、查询历史报告。
- **REST API 继续是核心**：`POST /api/v1/reports` 及现有阅读、搜索、删除、认证行为不变。MCP 与 REST 共享同一服务层/存储层、校验、幂等、SSE 事件和 SQLite 数据。
- **不增加独立 MCP 容器、Redis、消息队列或 Node 常驻进程**；继续 Go + Vue + SQLite 单镜像、`/data` 持久化、现有 18765 端口和 HTTPS 反向代理。
- MVP 仅开放 2 个工具：`publish_report`（写）与 `search_reports`（读）。后续扩展 `get_report`、`list_reports`；**不开放删除 Bot、删除报告、修改名称等危险操作**。
- 不承诺所有平台都支持远程 MCP 或自动调用；兼容性以真实客户端测试为准。MCP 不是性能优化，报告正文/工具响应仍消耗客户端模型上下文。

## 2. 先审计再实施（禁止假定接口）

- [x] 核查 master 当前 Go 路由、认证中间件、报告写入与检索服务、FTS5 查询、SSE、大小限制、日志、Dockerfile、Compose、反向代理说明、现有测试。
- [x] 记录现有 REST 字段与状态码，特别是 `source` 缺失归类 `unknown`、`stable_task_id` 的确定性 Bot ID 派生、`external_id` 幂等、`bot_name` 回退、`executed_at` RFC3339。
- [x] 确认所选官方 MCP Go SDK 的维护状态、协议版本、许可证和 Go 1.25 兼容性；锁定依赖 `v1.8.0`，未宣称未经实测的客户端兼容性。
- [x] 在技术设计中记录 MCP 工具参数到既有 REST 服务入参的映射和错误语义，复用写入与搜索逻辑。

## 3. 协议与传输

- [x] 在原 Go HTTP mux 注册 `/mcp` Streamable HTTP handler；SDK 处理初始化、协议协商、工具列表/调用、HTTP 方法、Accept/Content-Type 与传输错误。
- [x] 使用 SDK 无状态 HTTP 模式，不自行模拟 SSE、会话 ID 或 JSON-RPC。
- [x] 启用请求取消传播、`MAX_REQUEST_BYTES` 上限和并发；强制 Bearer 鉴权；响应不含 Token、数据库路径或内部堆栈。
- [x] 验证 `/healthz`、REST、SSE 路由和浏览器 SPA 未被 `/mcp` 路由影响。
- [x] 协议测试覆盖初始化、tools/list、tools/call、无效 JSON-RPC、错误 HTTP 方法/Accept、超限请求、连接取消和并发调用。

## 4. 安全与授权

- [x] `/mcp` 仅接受 Bearer `ACCESS_TOKEN`，拒绝 Web Cookie；文档说明单用户共享密钥模式并明确 Token 不进入 URL、剪贴板、仓库或日志。
- [x] 本期不实现 OAuth，也不宣称符合 OAuth 受保护资源标准；后续 OAuth 属独立工作。
- [x] 无 Origin 服务端客户端按 Bearer 认证；同源 Origin 允许、跨源 Origin 拒绝，无宽泛 CORS。
- [x] 写入每 IP 120 次/分钟、搜索每 IP 60 次/分钟；请求体及搜索条目受限；HTTPS 与反向代理限流已写入 README。
- [x] 日志仅记录方法、路径和耗时；测试覆盖缺失/错误 Token、Cookie 拒绝和跨源拒绝。
- [x] 工具注释描述读写、副作用和最终报告约束；文档说明客户端策略不是安全边界。

## 5. 工具一：publish_report（P0）

- [x] 注册 `publish_report`，提供明确描述、Typed JSON Schema、必填字段和长度限制，并说明只发布已完成正式报告。
- [x] 字段对齐现有 REST 契约；`executed_at`、`content` 必填；source 缺省为 `unknown`；Bot 稳定身份至少提供 bot_id/stable_task_id 之一。
- [x] 同 source 的相同 stable_task_id 稳定派生同一 Bot；缺少身份时返回清晰工具参数错误。
- [x] 复用 REST external_id 幂等；相同执行重试不新增报告/SSE；缺失 external_id 时不自造执行 ID。
- [x] 复用 REST 校验、Store 事务和 SSE；结构化成功响应只返回安全元数据，不含完整正文。
- [x] 参数错误与业务失败返回 MCP 工具错误，HTTP 鉴权/传输错误按 SDK 约定处理。

## 6. 工具二：search_reports（P1）

- [x] 注册只读 `search_reports`，参数覆盖 query/source/Bot/date/limit/cursor，复用现有 FTS5、筛选和游标分页。
- [x] 默认 limit 10、硬上限 20；只返回安全元数据、摘要和展示名，不返回完整 Markdown。
- [x] 覆盖日期、游标、空结果、中文、跨来源同名 Bot、鉴权及并发；查询不写入数据。
- [x] 无匹配返回空数组；展示名遵循 SQLite `display_name` 优先规则。

## 7. 后续工具（本阶段不作为上线阻塞）

- [x] 明确 `get_report(report_id)` 与 `list_reports` 属于后续设计，不在本 MVP 开放。
- [x] MCP 不实现 delete、rename、管理凭据、任意 SQL、URL 抓取或远程命令执行工具。

## 8. 部署与使用文档

- [x] README 写明 MCP 地址、Streamable HTTP、Bearer 环境变量、HTTPS/反向代理和 REST 差异。
- [x] 用真实 Codex CLI `0.162.0-alpha.2` 在隔离本地服务及匿名拉取的 GHCR master arm64 镜像上验证工具发现、发布和检索；接入配置步骤与安全变量名已写入 README。
- [x] 用独立 MCP Inspector CLI 在匿名拉取的 GHCR master 镜像上验证工具发现和 `search_reports`；Schema portability 检查 0 errors、13 warnings，报告注明不据此宣称兼容所有客户端。
- [x] README 提供正式报告发布 JSON 和中文搜索示例，不含实际 Token；说明 MCP 不保证定时调用、定时推送沿用 REST。
- [x] 工作台顶栏提供「MCP 配置」一键复制入口，使用当前站点 origin 拼接 `/mcp`，复制 Codex TOML 与 `BOLT_DOCK_TOKEN` 环境变量名，不接触实际 Token；剪贴板失败时提供手动选择复制降级。复制说明、配置步骤、安全边界及链接已写入 README。
- [x] 已更新技术设计、执行路线图、README 反向代理说明；Compose 单容器、镜像路径和 `/data` 不变。
- [x] 执行及验收文档随实现 PR、首轮交付文档 PR、布局修复 PR 与最终 GHCR 发布持续维护；回归发现的问题和未验证边界均按实测状态记录。
- [x] 已说明共享密钥公网暴露影响；本期未增加 `MCP_ENABLED` 开关，MCP 路由受现有 `ACCESS_TOKEN` 保护。

## 9. 验收矩阵（不得跳过）

- [x] A. 无凭据/错误 Token 被拒绝；有效 Token 完成初始化和 tools/list；跨源 Origin 被拒绝。
- [x] B. SDK 集成测试验证 MCP 发布→Store/SSE/REST；浏览器本地登录、页面阅读验证静态资源与 API 正常。
- [x] C. 重复 external_id 不重复写入或发事件；相同 stable_task_id 的不同执行落在同一 Bot。
- [x] D. REST 与 MCP 双向检索通过，含中文、日期、来源、同名 Bot 隔离及游标分页。
- [x] E. 工具校验、空结果/空查询、请求体限制、limit、并发、客户端取消、非法 JSON-RPC 和日志字段边界有测试。
- [x] F. `make test`、`make build`、Docker 构建、容器 `/healthz`、SQLite 重启持久化通过；资源实测数据与是否套用 Compose 限额见验收报告。
- [x] G. 真实 Codex CLI 对本机服务及发布后的 GHCR master 镜像完成工具发现、publish_report、search_reports；MCP Inspector CLI 完成第二客户端工具发现与搜索。
- [ ] H. P7 页面复制与响应式回归已在 320/375/390/430/768/1280/1440px 检查；P6 全量核心回归和完整设备矩阵仍未全部复验，生产部署也未提供。详情及截图见验收报告。
- [x] I. `docs/P7_MCP_ACCEPTANCE.md` 已创建并在合并/发布后更新，按证据记录实现、命令、PR、Actions、匿名 GHCR digest、客户端与部署限制；生产项保持未验收。

## 10. 推荐执行顺序与提交策略

1. **Phase 0 审计/设计**：检查当前仓库、选定 SDK 与协议，确认共享服务边界，提交技术设计。
2. **Phase 1 安全传输**：Go 内嵌 `/mcp`，初始化/tools/list、认证、Origin、限流与协议测试。
3. **Phase 2 发布工具**：`publish_report` 复用 REST 写入/幂等，Web/SSE 双向验证。
4. **Phase 3 检索工具**：`search_reports` 复用现有查询层，验证中文与分页。
5. **Phase 4 客户端与交付**：真实 Codex 连接、另一客户端可用性测试、文档、Docker/CI、P6 回归和验收报告。

按仓库保护规则通过 PR 合并至 `master`；不要直接在生产数据库上跑破坏性测试。若遇到需要用户凭据、外网部署或授权的步骤，记录阻塞并继续完成其他可独立验证的工作。**不能因为代码可编译就宣布 P7 完成。**
