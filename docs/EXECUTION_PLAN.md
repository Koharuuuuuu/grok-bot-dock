# Codex 执行计划（TODO）

> 目标：交付可直接部署的 grok-bot-dock V1。执行时按顺序完成并把 [ ] 改为 [x]，每阶段必须运行对应验证。不要只生成空架子；有阻塞记录原因和复现步骤。仓库默认分支 master。

## P0 项目初始化
- [x] 创建 Go 服务、Vue 3 + Vite 前端目录、构建脚本和基础路由。
- [x] 配置 Go embed 静态资源、SPA fallback、/healthz、18765 可配置监听。
- [x] 添加 .gitignore、.env.example、README 本地开发说明。
- [x] 初始化 CI：Go test、前端 build；锁定依赖版本。

## P1 数据与推送
- [x] SQLite migration：bots、reports、索引、FTS5；启用 WAL 和合理 busy_timeout。
- [x] 实现 POST /api/v1/reports：参数校验、大小限制、共享 Bearer Token、自动创建 Bot、事务写入、标准错误码。
- [x] 可选 external_id 幂等；重复请求不重复产生报告或通知。
- [x] 实现列表、详情、按日期过滤、稳定游标分页接口。
- [x] 编写 API 单元测试、并发写入和持久化测试。

## P2 页面与 Markdown
- [x] 实现响应式 Bot 卡片平铺；展示名称、执行时间、摘要、未读状态。
- [x] 实现点击展开报告、历史浏览、日期过滤和分页。
- [x] Markdown 支持 GFM 表格、代码、引用、链接、图片；渲染后消毒，验证 XSS。
- [x] 空状态、加载状态、错误提示及窄屏适配。

## P3 检索与通知
- [x] FTS5 全局搜索，验证中文匹配；按 Bot、日期过滤，分页跳转报告。
- [x] SSE /api/v1/events：心跳、重连、并发连接清理。
- [x] 新报告后更新卡片/未读数；经用户主动授权后发浏览器通知，点击跳转。
- [x] 通知拒绝授权、断线、浏览器不支持时优雅降级。

## P4 部署、验收与交付
- [x] 多阶段 Dockerfile、单容器 docker-compose.yml、/data 持久卷、非 root 运行。
- [x] 记录 HTTPS 反向代理、认证、备份/恢复、更新及故障排查步骤。
- [x] 自动化验证：构建 → 启动 → curl 推送 Markdown → 列表/搜索 → SSE → 重启后持久化。
- [x] 在资源受限环境测量容器内存、CPU、镜像大小；记录结果，不虚构。
- [x] 更新 README、技术设计、TODO 完成情况；生成 docs/ACCEPTANCE_REPORT.md（完成项、测试命令、结果、已知限制）。
- [x] 提交所有实现；PR #1 合并 Bolt Dock V1，PR #2–#3 完成 GHCR 多架构发布修复，PR #6 合并报告与 Bot 删除功能；PR #4–#5 合并验收报告更新。实现基线为 `ee9c48e`，删除功能合并提交为 `efa1619`。

## 完成定义
一个标准 API 能让新 Bot 自动出现卡片；报告 Markdown 正确安全渲染；可翻阅历史并跨 Bot 搜索；在线浏览器获得实时事件及授权通知；Docker 单容器可重启持久运行。Grok 端自动调用能力需要另做实测，不以假设充当验收成功。

## P2 补充：Bot 头像
- [x] 推送 API 支持可选 `avatar_url`，校验 HTTPS URL；数据库迁移增加 Bot 头像字段，未传时保留已有值。
- [x] 卡片展示推送头像；无头像或加载失败时使用稳定的内置默认图标；添加兼容性和回退测试。

## P0 补充：通用化与 UI 基线（必须实施）
- [x] 产品界面统一命名 Bolt Dock；仓库名保持 grok-bot-dock。
- [x] 将 source 设为推送必填字段，采用 (source, bot_id) 唯一识别 Bot，external_id 在该身份下幂等；迁移和测试覆盖跨来源相同 bot_id。
- [x] 增加 GET /api/v1/sources，来源仅由推送自动注册；列表、搜索、统计按 source 可选筛选；新增来源后 UI 动态更新。
- [x] API 支持可选 title、summary、tags、external_id、avatar_url，保持单一推送入口。
- [x] UI 移除左侧菜单，顶部保留全局搜索、动态来源下拉和统计；禁止「自定义」或手动配置来源。
- [x] 无选中报告时卡片网格铺满；选中后右侧阅读面板；支持放大沉浸阅读及退出恢复。
- [x] 验收：推送 grok 和 codex 两个同名 bot_id 的报告，验证各自独立、来源列表自动出现、筛选/搜索/计数正确；从未推送的来源不得显示。

## P4 补充：GitHub Actions、公开 GHCR 镜像、Compose
- [x] 新增 `.github/workflows/docker-publish.yml`：配置 master 和版本标签触发，校验通过后发布 GHCR 镜像，使用 packages: write 与最小权限；master 标签发布 amd64/arm64。
- [x] 配置镜像命名 `ghcr.io/nyakooo/grok-bot-dock`；master → :master，vX.Y.Z → 对应版本和 :latest；发布使用 GITHUB_TOKEN，不输出凭据。
- [x] 保持 GitHub 源码仓库 private；master 首次发布已创建 GHCR 包 `grok-bot-dock`，标签 `master`。
- [x] 将 GHCR 包单独设为 Public，并从无凭据 Docker 配置验证 `linux/amd64` 匿名 pull。
- [x] 提供默认拉取 GHCR 的 Compose 与真正可运行的本地构建 Compose 配置，包含持久化 /data、健康检查、重启策略、可配置端口与 token。
- [x] README 写明一键拉取部署及一键本地构建命令，均附 `docker image prune -f`；警告其清理宿主机所有悬空镜像且不删除数据卷。
- [x] GitHub Actions master 校验及 GHCR `master` 镜像发布通过；本地 Docker 构建、Compose 启动、健康检查、持久化及更新已验证。
- [x] GHCR `master` 标签提供 `linux/amd64` 与 `linux/arm64`；Go 在原生 builder 上交叉编译。
- [x] 无凭据 Docker 配置默认拉取 arm64 镜像；验证容器 `/healthz` 及 Docker health 状态。

## P2 补充：网页一键复制 AI 指令
- [x] 在 Web UI 添加「复制 AI 推送指令」按钮，保持无侧边栏 UI 基线。
- [x] 提供可编辑服务地址、source、bot_id、bot_name 的轻量模板预览；复制完整标准 POST JSON 推送提示词，token 只用安全占位符。
- [x] 提示词明确仅最终正式报告可推送，草稿、测试、日志、中间结果、失败任务不推送；external_id 幂等及 Markdown 格式必须完整。
- [x] 提供复制成功反馈和失败时手动复制降级；真实浏览器验证复制反馈，单测覆盖检索模型与提示词边界。

## P2 补充：报告与 Bot 删除
- [x] 单份报告可从 Bot 历史、搜索结果和阅读面板删除；确认永久删除后更新历史、统计和 FTS 搜索索引，并修正 Bot 最新报告时间。
- [x] Bot 可按 `(source, bot_id)` 删除；永久级联删除其全部报告及 FTS 条目；当来源不再关联任何 Bot 时删除空来源。
- [x] 删除 API 与事件受 Bearer Token 保护；报告/Bot 不存在返回 404；成功后发送 SSE 删除事件，其他工作台刷新。
- [x] API 与存储测试覆盖权限、级联、索引清理、来源保留/移除和最新报告回退；浏览器验收删除确认及 UI 状态刷新。

## GHCR 发布验收

GHCR 首次发布及后续多架构修复均已验收：PR #1–#3 已合并至 master，实现基线为 `ee9c48e`；PR #4–#5 合并验收报告更新，PR #6 合并报告与 Bot 删除功能（`efa1619`）。GHCR 包为 Public，GitHub 源码仓库保持 Private。删除功能对应的 master 发布 run `37938768277` 成功，匿名拉取 digest `sha256:f385cad5e01edb93a8a5f44b41316ada38b8c671680fdf64792dc4864d119b22`；镜像索引包含 amd64 和 arm64，arm64 容器健康检查通过。


## P5 专项优化：会话、详情、移动端、品牌与 CI/CD（2026-10-10，已完成）

> 本阶段先复现再修复；已验证的本地实现与浏览器项目打勾，仍须 GitHub Actions/PR 验收的项目继续保留未完成。保持单 Go 容器 + SQLite 持久卷、master 分支、现有 API 协议与轻量架构，不引入 Redis 或额外常驻服务。不要虚构实测结果。

### P5.1 登录会话稳定性（P0）
- [x] 复现登录后刷新页面退出问题；根因为 Token 仅存 Vue 内存且没有会话恢复。检查了 Cookie、SPA 初始化、401 和反向代理属性。
- [x] 服务端 12 小时会话 Cookie：HttpOnly、Path=/、SameSite=Lax，HTTPS 时 Secure；登录限流、同源 Origin 校验、会话探测与登出撤销；现有推送 Bearer 协议不变。
- [x] 文档说明会话存在服务进程内，服务重启会使会话失效；会话 Cookie 可能被浏览器恢复功能保留，网页不能可靠侦测浏览器进程关闭。
- [x] Go 测试和真实浏览器验证：登录→刷新→新标签成功；退出后其他标签刷新回登录；测试无效/过期会话、Cookie 属性、安全存储、SSE 登出即时撤销和 Bearer 兼容。

### P5.2 详情打开时页面抖动（P0）
- [x] 复现 flex 阅读面板挤压主列及滚动条布局变化；确认 body 滚动锁和焦点行为。
- [x] 改用独立 overlay、滚动条宽度补偿、背景滚动锁；关闭时恢复页面滚动与焦点，适配长 Markdown、表格及代码。
- [x] 真实浏览器移动端连续开关 3 次、桌面长文档实测；记录截图和 viewport 指标。

### P5.3 移动端响应式重构（P0）
- [x] 审查 320/375/390/430/768px 顶栏、搜索、来源、统计、卡片、详情和复制指令弹窗；页面无横向溢出或操作越界。
- [x] 小屏采用品牌/搜索/操作分行，统计标签在最窄屏折叠，卡片单列。
- [x] 移动端近全屏详情、内部滚动、历史列表纵向排列；Markdown 表格和代码局部横向滚动。
- [x] Playwright 五档响应式实测及桌面 1440px 回归；截图保存在 `output/playwright/`。

### P5.4 大详情改为悬浮层（P0）
- [x] 详情改为独立 dialog overlay；遮罩与背景锁定，底层卡片尺寸保持不变。
- [x] 桌面最大宽高、移动端全屏；Markdown 独立滚动，支持 Esc/关闭按钮、焦点循环及无障碍 dialog 语义。
- [x] 历史列表、日期、报告删除及原阅读状态保留；避免背景滚动穿透并恢复焦点。

### P5.5 品牌 Logo 与 favicon（P1）
- [x] 新增本地原创 SVG Logo 与 SVG favicon，更新浏览器标题。
- [x] 同一 Logo 用于登录页、Web 顶栏及 README；不加载外部素材。
- [x] 本地浏览器中图标清晰并成功加载；不含第三方商标或图片。

### P5.6 GitHub Actions / Docker 构建优化（P1）
- [x] 审计了两个旧 workflow：master push/PR 各重复跑两套验证；单次 Buildx 已发布双架构 manifest，并非重复发布。
- [x] 单一 CI 工作流做一次验证并依赖成功后发布；完整 push diff + 可复现路径脚本覆盖多提交/删除/重命名、Go/前端/Docker 与 docs。
- [x] 一次 Buildx 多架构推送、缓存、master/semver/latest 标签、tag/手动入口与最小权限均保留。
- [x] PR 验证不受路径过滤影响；首发/Tag/手动重试路径已配置；同分支可取消过期运行且版本 tag 使用独立并发组不被取消。
- [x] PR #8 的 Actions 验证通过、发布按 PR 事件跳过；合并提交 `02cfdb6` 的 master 验证与发布通过；匿名读取和 pull 的 GHCR `master` 索引包含 amd64/arm64（digest `sha256:5a1228990ee43aa32824ec5b24fb9cbbf56c2bcf5178ef4731518cb39bdab160`）。仓库状态为 Private，匿名拉取证实 GHCR 包公开。
- [ ] 未创建真实 semver tag，tag 事件发布仅完成配置和路径脚本验证。

### P5.7 交付与验收（必须）
- [x] 更新 README 与技术设计；新增验收报告记录根因、文件、命令、浏览器截图、workflow 差异和限制。
- [x] Go/前端测试与构建、Docker 构建和临时容器健康/持久化验证、浏览器回归通过；未触碰 Compose 部署卷。
- [x] PR #8 已合并 master（merge commit `02cfdb671b8b4cd06abda9364a99fae27bd48bc5`）；记录 GitHub Actions 和 GHCR 远端验收结果于 `docs/OPTIMIZATION_ACCEPTANCE.md`。


## P5.8 Markdown 阅读字号调节与持久化（2026-10-10，待执行）
- [ ] 在详情窗口顶部工具栏的日期选择器与最大化按钮之间添加字号缩小（−）、当前字号数值、放大（+）、恢复默认控件；保持与现有视觉风格一致。
- [ ] 默认 Markdown 正文字号 16px，可调整范围 12–24px，步长 1px；达到边界禁用对应按钮，当前字号清晰可见。
- [ ] 仅影响 Markdown 阅读正文及其标题、表格、列表、引用、代码块的相对排版，不缩放工具栏、历史侧栏或整个页面；不要通过浏览器整体 zoom 实现。
- [ ] 字号保存在浏览器 localStorage 的专用、版本化键中；首次访问或值损坏时回退 16px；刷新、退出重新登录、关闭浏览器重新打开、切换 Bot/报告后保留设置。此项是设备/浏览器本地偏好，不要求 SQLite 服务端同步。
- [ ] 普通悬浮详情与最大化阅读共享设置；最大化不得模糊正文，字号调整不得造成页面抖动、滚动穿透或水平溢出。
- [ ] 移动端采用紧凑字号菜单/控件，保证 320px 起无按钮拥挤和遮挡。
- [ ] 增加组件/浏览器测试：上下限、重置、刷新持久化、跨报告共享、无效存储回退、移动端与最大化显示；更新专项验收报告。


## P5.9 详情最大化状态持久化与跨报告继承（2026-10-10，待执行）
- [ ] 将详情窗口的普通悬浮 / 最大化阅读模式定义为全局阅读偏好，不与某个 Bot 或 report_id 绑定。
- [ ] 用户点击最大化后，立即记录状态；关闭当前详情并点击任何其他 Bot/报告时，直接以最大化模式打开，不得先普通模式闪现再切换。
- [ ] 用户主动退出最大化时立即保存普通悬浮状态；之后打开任意报告沿用普通模式。切换历史、搜索结果跳转、日期切换同样保持当前模式。
- [ ] 使用 localStorage 专用版本化键保存该偏好，刷新页面、重新登录、关闭浏览器重新打开后仍保留；存储不可用或数据异常时安全回退普通悬浮模式。不写入 SQLite，不引入额外服务。
- [ ] 与 P5.8 阅读字号持久化独立但协同；最大化/还原不重置字号、阅读报告、历史选择或不必要地改变滚动位置。
- [ ] 最大化是覆盖式浮层而非挤压布局；正文保持清晰，不得受到背景 blur/backdrop-filter 影响；禁止打开/切换时抖动、横向溢出和背景滚动穿透。
- [ ] 移动端可以为适配小屏采用全屏呈现，但不得将响应式强制全屏错误写回用户的桌面偏好；明确区分用户主动选择和视口适配。
- [ ] 自动化/浏览器验收：普通→最大化→关闭→打开其他 Bot 仍最大化；刷新→打开仍最大化；主动还原→切换 Bot 仍普通；跨历史/搜索跳转保持；关闭浏览器重开仍继承；异常存储回退；移动端切桌面不污染偏好。更新专项验收报告。


## P5.10 一键复制 AI 推送指令：零配置与自动识别（2026-10-10，待执行）
- [ ] 服务地址默认从当前浏览器 origin 自动读取并拼接 /api/v1/reports；如部署在子路径，依据应用配置的 API base URL 正确拼接。默认不展示 YOUR_DOMAIN；只允许必要时手动覆盖，明确 localhost/内网地址可能无法被外部 AI 访问。不可将带登录凭据的 URL 复制出去。
- [ ] 从复制弹窗移除“稳定 Bot ID”手动输入项；Bot ID 由执行 Agent 根据其可访问的稳定任务 ID、任务名称或稳定 slug 生成。禁止每次按日期/执行时间重新生成 bot_id；同一任务必须稳定复用，不能凭空声称拥有平台任务 ID。
- [ ] 复制提示词要求 Agent 自动识别 source（如 grok/codex/qwen）和 bot_name（任务原始名称）；无法识别 source 时允许空字符串或省略，服务端统一规范化为 unknown；bot_name 不可识别时允许按明确规则使用稳定任务描述，必要时以 bot_id 作为展示名。
- [ ] 同步更新后端 POST /api/v1/reports 校验、OpenAPI/README/技术设计/示例与测试：source 可选且空白归类 unknown；bot_id 可由服务端在缺失时根据稳定、可复现的任务标识推导；若既无 bot_id 又无可靠稳定标识，必须明确返回可理解的 400 错误，不得随机创建 Bot 或按每天生成新 Bot。明确身份冲突处理和跨来源唯一性。
- [ ] 在 AI 指令中规定：仅在最终正式报告完成并确认后 POST 一次；草稿、测试、调试、中间产物、失败任务不得推送；内容为完整 Markdown，保留来源链接、标题、表格；executed_at 用实际 RFC3339 时间；external_id 对同一次执行稳定且重试复用；仅 200/201 为成功，4xx 不盲目重试，网络错误/5xx 有限重试。
- [ ] AI 指令应包含完整 JSON 示例、Content-Type、可选 Bearer token 安全占位符；不得把真实 token 放到剪贴板、公开日志或 URL。若平台不支持直接外部 HTTP POST，必须明确告知而不能伪称成功。若服务端启用了鉴权，复制指令应清晰告知需要在 Agent 执行环境配置 token，不能宣称真正零鉴权。
- [ ] UI 以“一键复制完整指令”为核心；移除不必要的 source/bot_id/bot_name 必填输入，保留必要的高级覆盖入口与可预览文本；按钮复制反馈、剪贴板失败降级和移动端适配。
- [ ] 验收：当前 HTTPS origin 正确生成 API URL；不同 Bot 自动分组且同任务连续两日不分裂；unknown 来源可入库；无稳定标识返回明确错误；最终报告成功且测试/草稿不推送；复制文本与实际 API 契约一致；现有已接入 Bot 不因兼容性改动被重命名或拆分。更新 docs/OPTIMIZATION_ACCEPTANCE.md。


## P5.11 Bot 展示名称支持修改（2026-10-10，待执行）
- [ ] 每张 Bot 卡片增加清晰但克制的“重命名”操作（可在卡片菜单或标题旁编辑入口），桌面及移动端均可用；提供当前名称预填、保存、取消、空白/长度校验、成功与失败反馈。
- [ ] 修改的是用户自定义的**展示名称**，不是稳定 bot_id，也不是 source；禁止因改名导致历史报告丢失、Bot 分裂、幂等失效或推送 API 地址变化。
- [ ] 后端在 bots 表新增 nullable display_name（如现有字段能安全复用也需说明），提供受保护的 PATCH /api/v1/bots/{id}（或等价资源接口）更新展示名称；仅允许已登录 Web 用户操作，校验长度、空白、非法输入，避免 XSS；原始 bot_name 与展示名称分离。
- [ ] UI 优先展示 display_name，未设置时展示原始 bot_name；详情标题、历史、搜索结果及卡片统一遵守此规则。
- [ ] 后续 AI 按同一 (source, bot_id) 推送新的 bot_name 时不得覆盖用户手动设置的 display_name；允许再次修改或清除自定义名称以恢复自动名称。
- [ ] SQLite migration 必须无损，数据持久化于 /data，容器重启及浏览器更换后仍有效；不得仅保存在 localStorage。
- [ ] 回归：修改名称→刷新→新报告推送→切换设备→重启容器，名称均保留；相同 bot_id 历史和搜索仍归属原 Bot；删除与权限校验不退化。更新 docs/OPTIMIZATION_ACCEPTANCE.md。


## P6 遗漏问题集中修复（当前最高优先级，待验收）

> 用户反馈此前 P5 实际仅「最大化弹窗模糊」已修复，其他事项未达到预期。此前 P5 标记 [x] 不构成本轮验收依据。**本轮统一以 [P6 集中修复与验收清单](./P6_CONSOLIDATED_FIX_PLAN.md) 为唯一执行入口**，涵盖登录、抖动、移动端、悬浮详情、阅读模式/字号持久化、AI 指令、Bot 改名、Logo、Actions 和真实环境回归。Codex 必须逐项实测、记录证据并提交 P6 验收报告，不得只修改 TODO。

> 本轮执行和实测记录见 [P6 验收报告](./P6_ACCEPTANCE_REPORT.md)。本地 Go/Vue、真实浏览器、隔离 Docker 与 SQLite 持久性检查已通过；PR/Actions/GHCR 结果在合并后补录。生产部署 URL 未提供，生产版本与数据仍标记未验证。


## P7 MCP 原生接入（2026-10-10，已合并并完成镜像验收）

> [P7 MCP 技术设计与执行验收计划](./P7_MCP_EXECUTION_PLAN.md) 是本阶段唯一权威清单。`/mcp` 提供无状态 Streamable HTTP、`publish_report` 与 `search_reports`，共用 REST 校验、幂等、限流、SSE、SQLite 和 FTS5；README、技术设计及反向代理说明已更新。PR #13 已合并到 master；PR/master Actions、GHCR master 多架构发布、匿名 manifest/blob 拉取、Codex CLI 和 MCP Inspector 客户端验收均已记录在 [P7 验收报告](./P7_MCP_ACCEPTANCE.md)。P6 全量设备矩阵及生产部署未验证，继续保留为未完成项。
