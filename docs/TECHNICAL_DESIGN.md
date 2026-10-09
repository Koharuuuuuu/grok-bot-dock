# 技术设计

## 1. 架构
Grok Bot（或任何 HTTP 客户端） → HTTPS 反向代理 → Go HTTP 服务 → SQLite。Vue 3 + Vite 构建为静态文件，由 Go 同进程提供；SSE 长连接向在线浏览器广播新报告事件。服务默认监听 `18765`（环境变量可改），公网 HTTPS 由已有反向代理终止。

## 2. 技术栈与资源约束
Go 标准库优先（路由可用标准库）；Vue 3、Vite；SQLite + FTS5；Markdown 渲染库和 HTML 消毒库。不得引入 Redis、ES、队列、单独 Node 运行时。多阶段 Docker 构建，非 root 运行，SQLite 存放 /data；WAL 模式、busy_timeout、合理连接池限制；提供 /healthz。2 vCPU / 1.6 GiB 环境为目标，实际峰值内存、启动时间及容器镜像大小需测量，不能无依据承诺具体数值。

## 3. 数据模型
sources: source(TEXT PRIMARY KEY), created_at。只在首次接收该来源推送时注册。
bots: id(INTEGER PRIMARY KEY), source(FK), bot_id, bot_name, avatar_url, created_at, updated_at, last_report_at, last_read_report_id。唯一键 (source, bot_id)。
reports: id, bot_pk(FK), external_id(NULLABLE), executed_at(固定精度 UTC ISO8601), received_at, content_md, title, summary, tags_json。索引 (bot_pk, executed_at DESC, id DESC)，部分唯一索引 (bot_pk, external_id) 用于幂等。FTS5 trigram 虚表索引 source、bot_id、title、summary、content、tags，报告写入/删除触发器保持索引同步；少于三个字符的查询退化为转义后的 LIKE。SQLite schema 使用版本记录；V1 首次实施时直接采用复合来源模型。

## 4. HTTP API
`POST /api/v1/reports`：JSON，必填 bot_id、bot_name、executed_at、content；可选 external_id。校验 RFC3339 时间、长度、报文上限（建议 1 MiB，可配置），拒绝空白内容及非法字段；首次写入自动 upsert Bot；事务内插入报告；成功返回 201 + report_id、bot_id；重复 external_id 返回 200 + 已有 report_id；400 校验失败、401 鉴权失败、413 超限、429 限流、500 服务错误。避免在日志中打印 token 和报告正文。

只读接口：`GET /api/v1/sources`（已出现来源及计数），`GET /api/v1/bots?source=`（卡片与统计），`GET /api/v1/bots/{bot_id}/reports?source=&cursor=&date=`，`GET /api/v1/reports?source=&bot_id=&date=&cursor=`，`GET /api/v1/reports/{id}`，`POST /api/v1/bots/read`（记录阅读进度），`GET /api/v1/search?q=&source=&bot_id=&from=&to=&cursor=`，`GET /api/v1/events`（SSE），`GET /healthz`。历史按时间倒序稳定游标分页，默认 20 条；from/to 是含首尾日期，搜索结果有摘要、来源、时间、高亮与跳转目标。数据库统计把主库和 WAL/SHM 文件容量相加，始终作为全库指标显示。

## 5. 安全与通知
所有 `/api/` 读写接口共享必填 `ACCESS_TOKEN` Bearer Token，采用常量时间比较；缺少 token 时拒绝启动。网页登录通过密码输入并仅在当前页面内存持有 token，不写入 localStorage、sessionStorage、提示词或剪贴板。请求体默认上限 1 MiB，可配置；推送接口限制每客户端地址每分钟 120 次。CORS 默认同源；Markdown 原始 HTML 禁用，DOMPurify 清理输出。浏览器通知需要 HTTPS/localhost、用户授权与浏览器/系统允许。SSE 支持页面内重连、25 秒心跳及断线连接清理；浏览器离线期间不保证收到事件。

## 6. 前端交互
桌面优先、移动端自适应；首页卡片网格按最后更新时间排序。每张卡展示 Bot 名称、最新报告摘要、执行时间、未读状态。点击卡片在抽屉/详情视图阅读完整 Markdown（标题、列表、表格、代码、链接、图片）；历史日期筛选、分页；顶部全局搜索，搜索结果高亮关键词并可直达原文。通知权限由明确的用户操作触发，不在页面初次加载时强制弹窗。卡片列表不加载历史全文，避免性能浪费。

## 7. 质量保障
Go 单测覆盖验证、幂等、并发写入、FTS 同步、分页、鉴权；前端测试覆盖 Markdown XSS、卡片更新、检索与通知降级；端到端测试使用 curl POST → SQLite → Web/SSE。Docker 重启后数据不丢失。提供 curl 示例、.env.example、docker-compose.yml、Makefile、部署与备份说明；CI 运行 Go 测试和前端构建。默认 master 分支，提交前检查现有内容并避免破坏性覆盖。

## 8. Bot 头像扩展
`POST /api/v1/reports` 增加可选 `avatar_url` 字段（HTTPS URL，限制长度并验证格式），`bots` 表增加 nullable `avatar_url` 列；有值时更新该 Bot 的头像地址，缺省时保留原值。卡片优先展示 Bot 提供的头像；未提供或加载失败时显示本地内置默认图标（固定、稳定、无需第三方服务）。前端图片加载失败须回退默认图标。若未来后端抓取或代理头像，必须防范 SSRF；V1 不主动下载远程图片。不得将头像字段设为必填，也不得影响原有客户端兼容性。

## 9. Bolt Dock 通用来源协议及 UI（覆盖旧约定）
产品显示名称：**Bolt Dock**；仓库名仍为 grok-bot-dock。POST /api/v1/reports 的规范请求：
```json
{
  "source": "grok",
  "bot_id": "ai-news",
  "bot_name": "AI 行业动态",
  "executed_at": "2026-10-09T20:00:00+08:00",
  "title": "今日 AI 行业动态",
  "summary": "今日重点摘要",
  "tags": ["AI", "新闻"],
  "external_id": "2026-10-09-ai-news",
  "avatar_url": "https://example.com/avatar.png",
  "content": "# 今日 AI 行业动态\\n\\n报告正文"
}
```
必填 source、bot_id、bot_name、executed_at、content；其余可选。source 是由发送方给出的稳定、规范化的机器标识（建议小写字母、数字、连字符，限制长度）；不是从标题猜测。服务端在同一事务中自动登记首次出现的 source，Bot 由 (source, bot_id) 唯一确定，报告必须引用该复合身份或内部 Bot 主键；external_id 的幂等作用域同样为 (source, bot_id)。不允许不同来源同名 bot_id 互相覆盖。GET /api/v1/sources 返回数据库中实际出现的来源及计数，来源无需手动创建；bots、reports、search 查询支持 source 可选过滤。所有筛选选项由 GET sources 动态获取，默认「全部来源」；新来源推送后刷新或 SSE 更新下拉。禁止“自定义”来源选项、手工来源配置页面或硬编码来源枚举。若未来修改已有数据结构，使用可回滚/可验证的迁移并保留旧数据；V1 尚未实施时直接采用新 schema。

UI：无左侧导航；顶部品牌 Bolt Dock、默认跨来源全局搜索、动态来源下拉、Bot 数/报告数/全库数据大小。来源切换同时影响卡片、搜索、相关计数；数据库物理大小始终标记「全库」。未选择卡片时网格占满；点击卡片右侧抽屉显示 Markdown；抽屉放大按钮进入宽屏沉浸式阅读，支持退出还原。头像由 avatar_url 提供，无头像/失败使用默认图标。桌面优先、移动端响应式。UI 实现以本节为最新权威约束，覆盖旧版左侧菜单设计。

## 10. GHCR 发布、Compose 与复制指令（新增强制约束）
- GitHub Actions：push master 和 push v* tag 触发；先执行 Go 测试、前端构建及镜像构建检查，成功后使用 GitHub Actions `GITHUB_TOKEN`、`packages: write` 登录 GHCR，通过 docker/build-push-action 推送多阶段构建镜像。master 发布 `master` 标签；版本 tag 发布对应版本及 `latest`，不得在未经验证的 master 提交上自动覆盖稳定 latest。启用构建缓存，避免重复构建；权限最小化。镜像支持 linux/amd64，linux/arm64 如成本允许则支持并测试。
- GHCR 包必须 Public，源码仓库保持现有私有属性；首次发布后由有权限者在 Package settings 将包可见性设为 Public（若未自动公开）。从未认证环境验证 `docker pull ghcr.io/nyakooo/grok-bot-dock:master`；Actions 通过不等于镜像已公开。
- Compose：默认使用公开 GHCR 镜像；映射 `18765:18765`（建议绑定 127.0.0.1 并通过 HTTPS 反代），挂载命名卷到 /data，设置 restart、healthcheck、必要环境变量。额外提供本地 build 的 compose override/profile 或明确 `docker compose -f ...` 命令，确保确实触发 build。README 写出拉取部署、本地构建、更新及 `docker image prune -f`，注明 prune 影响整台主机的悬空镜像，不删数据卷。
- UI：提供「复制 AI 推送指令」入口和轻量配置/预览；用 Clipboard API 复制与 README 一致的模板，包含 source、bot_id、bot_name、executed_at、content、external_id；强调仅推正式报告、不得推测试/草稿/日志；拒绝授权或 Clipboard API 不可用时显示可手动选择的文本。不要将真实 token 写入浏览器页面、localStorage 或剪贴板，模板使用安全占位符。UI 需与无侧栏卡片布局兼容。
- 验收：检查 workflow 成功、GHCR 匿名 pull、Compose 启动/健康检查/持久化、复制文本字段完整且包含正式报告过滤规则；在未配置实际域名时不虚构真实部署地址。

当前 Compose 将 HTTP 端口仅绑定到宿主机 `127.0.0.1`，容器限制为 2 CPU、1600 MiB；应通过同机 HTTPS 反向代理提供公网入口。网页登录 token 只保存在当前页面内存，页面刷新后需重新输入。
