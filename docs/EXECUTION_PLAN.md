# Codex 执行计划（TODO）

> 目标：交付可直接部署的 grok-bot-dock V1。执行时按顺序完成并把 [ ] 改为 [x]，每阶段必须运行对应验证。不要只生成空架子；有阻塞记录原因和复现步骤。仓库默认分支 master。

## P0 项目初始化
- [ ] 创建 Go 服务、Vue 3 + Vite 前端目录、构建脚本和基础路由。
- [ ] 配置 Go embed 静态资源、SPA fallback、/healthz、18765 可配置监听。
- [ ] 添加 .gitignore、.env.example、README 本地开发说明。
- [ ] 初始化 CI：Go test、前端 build；锁定依赖版本。

## P1 数据与推送
- [ ] SQLite migration：bots、reports、索引、FTS5；启用 WAL 和合理 busy_timeout。
- [ ] 实现 POST /api/v1/reports：参数校验、大小限制、共享 Bearer Token、自动创建 Bot、事务写入、标准错误码。
- [ ] 可选 external_id 幂等；重复请求不重复产生报告或通知。
- [ ] 实现列表、详情、按日期过滤、稳定游标分页接口。
- [ ] 编写 API 单元测试、并发写入和持久化测试。

## P2 页面与 Markdown
- [ ] 实现响应式 Bot 卡片平铺；展示名称、执行时间、摘要、未读状态。
- [ ] 实现点击展开报告、历史浏览、日期过滤和分页。
- [ ] Markdown 支持 GFM 表格、代码、引用、链接、图片；渲染后消毒，验证 XSS。
- [ ] 空状态、加载状态、错误提示及窄屏适配。

## P3 检索与通知
- [ ] FTS5 全局搜索，验证中文匹配；按 Bot、日期过滤，分页跳转报告。
- [ ] SSE /api/v1/events：心跳、重连、并发连接清理。
- [ ] 新报告后更新卡片/未读数；经用户主动授权后发浏览器通知，点击跳转。
- [ ] 通知拒绝授权、断线、浏览器不支持时优雅降级。

## P4 部署、验收与交付
- [ ] 多阶段 Dockerfile、单容器 docker-compose.yml、/data 持久卷、非 root 运行。
- [ ] 记录 HTTPS 反向代理、认证、备份/恢复、更新及故障排查步骤。
- [ ] 自动化验证：构建 → 启动 → curl 推送 Markdown → 列表/搜索 → SSE → 重启后持久化。
- [ ] 在资源受限环境测量容器内存、CPU、镜像大小；记录结果，不虚构。
- [ ] 更新 README、技术设计、TODO 完成情况；生成 docs/ACCEPTANCE_REPORT.md（完成项、测试命令、结果、已知限制）。
- [ ] 提交所有实现；优先通过 PR 合并 master，按仓库现有保护规则操作。

## 完成定义
一个标准 API 能让新 Bot 自动出现卡片；报告 Markdown 正确安全渲染；可翻阅历史并跨 Bot 搜索；在线浏览器获得实时事件及授权通知；Docker 单容器可重启持久运行。Grok 端自动调用能力需要另做实测，不以假设充当验收成功。
