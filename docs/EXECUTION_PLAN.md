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
- [x] 提交所有实现；PR #1 合并 Bolt Dock V1，PR #2–#3 完成 GHCR 多架构发布修复；均通过校验并合入 master，当前提交 `ee9c48e`。

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

## GHCR 发布验收

GHCR 首次发布及后续多架构修复均已验收：PR #1–#3 已合并至 master（最新提交 `ee9c48e`），GHCR 包为 Public，GitHub 源码仓库保持 Private。workflow run `37936261007` 成功发布 digest `sha256:4f9c4bf58ac018c481321e90fdba2a6bd236a62d815208c2e7c3da66a2fec9dc`；镜像索引包含 amd64 和 arm64。空 Docker 配置下默认匿名拉取在本机选择 arm64，容器健康检查通过。
