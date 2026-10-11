# 技术设计

## 1. 架构
Grok Bot（或任何 HTTP 客户端） → HTTPS 反向代理 → Go HTTP 服务 → SQLite。Vue 3 + Vite 构建为静态文件，由 Go 同进程提供；SSE 长连接向在线浏览器广播新报告事件。服务默认监听 `18765`（环境变量可改），公网 HTTPS 由已有反向代理终止。

## 2. 技术栈与资源约束
Go 标准库优先（路由可用标准库）；Vue 3、Vite；SQLite + FTS5；Markdown 渲染库和 HTML 消毒库。不得引入 Redis、ES、队列、单独 Node 运行时。多阶段 Docker 构建，非 root 运行，SQLite 存放 /data；WAL 模式、busy_timeout、合理连接池限制；提供 /healthz。2 vCPU / 1.6 GiB 环境为目标，实际峰值内存、启动时间及容器镜像大小需测量，不能无依据承诺具体数值。

## 3. 数据模型
sources: source(TEXT PRIMARY KEY), created_at。只在首次接收该来源推送时注册。
bots: id(INTEGER PRIMARY KEY), source(FK), bot_id, bot_name（发送方原始名称）, display_name（可空的用户自定义展示名）, avatar_url, created_at, updated_at, last_report_at, last_read_report_id。唯一键 (source, bot_id)；重命名不改变稳定身份，后续上报只更新 bot_name，不覆盖 display_name。
reports: id, bot_pk(FK), external_id(NULLABLE), executed_at(固定精度 UTC ISO8601), received_at, content_md, title, summary, tags_json。索引 (bot_pk, executed_at DESC, id DESC)，部分唯一索引 (bot_pk, external_id) 用于幂等。FTS5 trigram 虚表索引 source、bot_id、title、summary、content、tags，报告写入/删除触发器保持索引同步；少于三个字符的查询退化为转义后的 LIKE。SQLite schema 使用版本记录；V1 首次实施时直接采用复合来源模型。

## 4. HTTP API
`POST /api/v1/reports`：JSON，必填 executed_at、content；source 缺失或空白归一化为 `unknown`。bot_id 可直接提供，或通过稳定 `stable_task_id` 对 source+任务身份做确定性 SHA-256 派生；两者均缺失时返回明确 400，不随机生成。bot_name 缺失时回退 task_name，再回退 bot_id。external_id 可选且在 Bot 身份范围幂等。校验 RFC3339 时间、长度、报文上限，拒绝空白内容及非法字段；事务内 upsert Bot 并插入报告。成功返回 201；重复 external_id 返回 200；400 校验失败、401 鉴权失败、413 超限、429 限流、500 服务错误。避免在日志中打印 token 和报告正文。

接口：`POST /api/v1/auth/login`（用 ACCESS_TOKEN 建立浏览器会话），`GET /api/v1/auth/session`（只返回当前 Cookie 是否有效），`POST /api/v1/auth/logout`（立即撤销当前会话并关闭关联 SSE），`GET /api/v1/sources`（已出现来源及计数），`GET /api/v1/bots?source=`（卡片与统计），`PATCH /api/v1/bots?source=&bot_id=`（仅浏览器会话可更新/清除 display_name），`GET /api/v1/bots/{bot_id}/reports?source=&cursor=&date=`，`GET /api/v1/reports?source=&bot_id=&date=&cursor=`，`GET /api/v1/reports/{id}`，`DELETE /api/v1/reports/{id}`（删除单份报告），`DELETE /api/v1/bots?source=&bot_id=`（删除 Bot 及其报告），`POST /api/v1/bots/read`（记录阅读进度），`GET /api/v1/search?q=&source=&bot_id=&from=&to=&cursor=`，`GET /api/v1/events`（SSE），`GET /healthz`。工作台 API 接受服务端会话 Cookie，外部推送 API 保留 Bearer Token；健康检查与只返回会话状态布尔值的 session probe 不要求认证。历史按时间倒序稳定游标分页，默认 20 条；from/to 是含首尾日期，搜索结果有摘要、来源、时间、高亮与跳转目标。数据库统计把主库和 WAL/SHM 文件容量相加，始终作为全库指标显示。

删除逻辑：删除报告使用事务删除报告记录，由 SQLite 删除触发器同步移除 FTS 行；Bot 保留，`last_report_at` 回退到剩余报告中的最新时间；报告不存在返回 404。删除 Bot 按 `(source, bot_id)` 定位，事务内级联删除全部报告及其 FTS 行；同一来源仍有 Bot 时保留来源，否则同时删除空来源；Bot 不存在返回 404。成功删除分别广播 `report.deleted`（报告 ID、来源、Bot ID）和 `bot.deleted`（来源、Bot ID、删除报告数）SSE 事件。前端删除控件执行前确认永久删除，成功后刷新卡片、报告历史、统计和搜索；Bot 删除确认明确报告级联范围。

## 5. 安全与通知
缺少 `ACCESS_TOKEN` 时服务拒绝启动；外部推送保留 Bearer Token 常量时间比较。网页登录把密钥只提交到登录接口，服务端生成 256-bit 随机会话 ID，在进程内保存 12 小时到期时间，仅经 `HttpOnly; Path=/; SameSite=Lax` Cookie 交给浏览器；不设置持久期限。OAuth 已启用且 `PUBLIC_BASE_URL` 有效时，浏览器会话写请求的 Origin 必须匹配该固定公网 origin，Cookie 的 `Secure` 属性也由其 HTTPS scheme 决定；这使 TLS 终止反代的内部 Host/HTTP scheme 不会错误拒绝合法请求，也不会依赖客户端可控的转发头放宽来源校验。OAuth 未启用时，Origin 对比请求 Host 与 TLS/`X-Forwarded-Proto`，Cookie Secure 也沿用 TLS/该转发头判断。反向代理须覆盖而非透传用户提供的 X-Forwarded-Proto。注销会撤销会话并用同属性清除 Cookie；会话写请求必须有匹配 Origin。登出还会立即关闭该会话的 SSE；前端通过 BroadcastChannel 同步其他标签退出并清空缓存。进程重启会使活动会话失效。浏览器可能通过会话恢复保留无持久期限的 Cookie，网页无法可靠判断整个浏览器进程已经关闭，因此“关闭浏览器即登出”不作保证。请求体默认上限 1 MiB，可配置；推送接口限制每客户端地址每分钟 120 次，登录另限每客户端地址每分钟 10 次。CORS 默认同源；Markdown 原始 HTML 禁用，DOMPurify 清理输出。浏览器通知需要 HTTPS/localhost、用户授权与浏览器/系统允许。SSE 支持页面内重连、25 秒心跳及断线连接清理；浏览器离线期间不保证收到事件。

## 6. 前端交互
桌面与移动端自适应；首页卡片网格按最后更新时间排序。每张卡展示 Bot 的自定义展示名（若设置）或原始 bot_name、最新报告摘要、执行时间、未读状态。点击卡片打开不参与主页面布局的悬浮 dialog，底层尺寸不变；对话框内滚动 Markdown，历史日期筛选、分页；支持 Esc、焦点管理和关闭后恢复焦点/滚动位置。最大化阅读偏好和 Markdown 字号分别存入版本化 localStorage 键，默认普通模式和 16px，字号范围 12–24px；移动端详情强制全屏但不更改桌面最大化偏好。移动端历史列表与正文纵向排列，表格和代码块局部横向滚动。顶部全局搜索，搜索结果高亮关键词并可直达原文。通知权限由明确的用户操作触发，不在页面初次加载时强制弹窗。卡片列表不加载历史全文，避免性能浪费。

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
`executed_at`、`content` 必填；`source` 缺失或空白归一化为 `unknown`；`bot_id` 可直接提供，或由稳定 `stable_task_id` 确定性推导；缺少两者均返回 400。`bot_name` 缺失时回退到 `task_name`，再回退到稳定 Bot ID。source 是发送方提供的稳定、规范化机器标识（小写字母、数字、连字符，限制长度）；不是从标题猜测。服务端在同一事务中自动登记首次出现的 source，Bot 由 (source, bot_id) 唯一确定，报告必须引用该复合身份或内部 Bot 主键；external_id 的幂等作用域同样为 (source, bot_id)。不允许不同来源同名 bot_id 互相覆盖。GET /api/v1/sources 返回数据库中实际出现的来源及计数，来源无需手动创建；bots、reports、search 查询支持 source 可选过滤。所有筛选选项由 GET sources 动态获取，默认「全部来源」；新来源推送后刷新或 SSE 更新下拉。禁止“自定义”来源选项、手工来源配置页面或硬编码来源枚举。若未来修改已有数据结构，使用可回滚/可验证的迁移并保留旧数据；V1 尚未实施时直接采用新 schema。

UI：无左侧导航；顶部品牌 Bolt Dock、默认跨来源全局搜索、动态来源下拉、Bot 数/报告数/全库数据大小。来源切换同时影响卡片、搜索、相关计数；数据库物理大小始终标记「全库」。未选择卡片时网格占满；点击卡片右侧抽屉显示 Markdown；抽屉放大按钮进入宽屏沉浸式阅读，支持退出还原。头像由 avatar_url 提供，无头像/失败使用默认图标。桌面优先、移动端响应式。UI 实现以本节为最新权威约束，覆盖旧版左侧菜单设计。

## 10. GHCR 发布、Compose 与复制指令（新增强制约束）
- GitHub Actions：单一 `.github/workflows/ci.yml` 在 master push、v* tag、master PR 和手动 dispatch 触发；每次只执行一套 Go 与前端验证。master push 通过完整 `before..sha` diff 和 `scripts/release-paths.sh` 判定运行镜像输入；文档类改动不发布，rename/deletion 通过 `--no-renames` 检查旧、新路径。PR 验证不受路径过滤影响；tag 和手动 dispatch 不依赖路径变化，发布失败可重跑失败 Job 或 dispatch 失败 ref。发布 Job 依赖验证，仅授予 `packages: write`，用一个 Buildx 调用推送 linux/amd64、linux/arm64 manifest。master 发布 `master`；版本 tag 发布对应版本及 `latest`。所有工作流修改路径都能触发发布流程修复的验证与部署；启用缓存和并发取消，但不取消版本 tag 发布。
- GHCR 包必须 Public，源码仓库保持现有私有属性；首次发布后由有权限者在 Package settings 将包可见性设为 Public（若未自动公开）。从未认证环境验证 `docker pull ghcr.io/nyakooo/grok-bot-dock:master`；Actions 通过不等于镜像已公开。
- Compose：默认使用公开 GHCR 镜像；映射 `18765:18765`（建议绑定 127.0.0.1 并通过 HTTPS 反代），挂载命名卷到 /data，设置 restart、healthcheck、必要环境变量。额外提供本地 build 的 compose override/profile 或明确 `docker compose -f ...` 命令，确保确实触发 build。README 写出拉取部署、本地构建、更新及 `docker image prune -f`，注明 prune 影响整台主机的悬空镜像，不删数据卷。
- UI：提供「复制 AI 推送指令」入口和轻量配置/预览；用 Clipboard API 复制与 README 一致的模板，包含 source、bot_id、bot_name、executed_at、content、external_id；强调仅推正式报告、不得推测试/草稿/日志；拒绝授权或 Clipboard API 不可用时显示可手动选择的文本。不要将真实 token 写入浏览器页面、localStorage 或剪贴板，模板使用安全占位符。UI 需与无侧栏卡片布局兼容。
- 验收：检查 workflow 成功、GHCR 匿名 pull、Compose 启动/健康检查/持久化、复制文本字段完整且包含正式报告过滤规则；在未配置实际域名时不虚构真实部署地址。

当前 Compose 将 HTTP 端口仅绑定到宿主机 `127.0.0.1`，容器限制为 2 CPU、1600 MiB；应通过同机 HTTPS 反向代理提供公网入口。网页登录用服务端内存会话和 HttpOnly Cookie 恢复刷新状态；进程重启会撤销所有浏览器会话。

## 11. P7 远程 MCP（历史基线）

MCP 通过现有 Go mux 的精确路径 `/mcp` 提供 Streamable HTTP，继续使用单 Go 服务、Vue 静态资源和 SQLite。协议层采用官方 [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) 固定版本 `v1.8.0`；其模块要求 Go `1.25.0`，与本仓库 `go.mod` 一致。SDK 支持 MCP `2026-07-28` 和多个旧版协议。锁定版本按贡献代码归属采用 Apache-2.0 或 MIT 许可，详见依赖 LICENSE。P7 使用 SDK 的无状态传输，不自行实现 JSON-RPC、SSE 或会话协议；SDK 限制 MCP 请求体不超过服务 `MAX_REQUEST_BYTES` 配置，默认 1 MiB。

MCP 是独立服务，不依赖 Codex 或特定客户端进程。它仅接受常量时间校验的 `Authorization: Bearer <ACCESS_TOKEN>`，拒绝网页登录 Cookie；Origin 为空时允许已认证的服务端客户端，同源 Origin 可通过，跨源 Origin 拒绝。MCP 不实现 OAuth 或多用户授权。发布继续使用原每客户端地址 120 次/分钟限流；MCP 搜索另限 60 次/分钟。日志仅记录 HTTP 方法、路径和耗时，不记录 Authorization、工具参数或报告正文。部署到公网时仍须使用 HTTPS，并建议反向代理限制来源和请求速率。

登录后的 `GET /api/v1/mcp/capabilities` 返回服务路径、传输、共享 Bearer 认证、实际注册工具及无密钥示例参数，供工作台展示；该接口不报告服务在线状态，也不返回密钥。工作台默认以中立的「MCP 接入」展示当前站点推导的 `/mcp` URL，允许用户在当前浏览器本地覆盖反向代理公开地址，并提供无密钥通用说明复制。它保留独立 REST 推送入口。服务当前复用管理员 `ACCESS_TOKEN` 作为共享密钥，只适用于受信任的单用户私有接入；没有独立 Agent Token、按客户端授权、撤销、过期或多租户能力。

`publish_report` 的参数投影与 REST `POST /api/v1/reports` 共用 `normalizeReportRequest` 与 `publishReport`：`executed_at` 为 RFC3339、`content` 必须非空；`source` 缺失时为 `unknown`；`bot_id` 与 `stable_task_id` 至少提供一个；稳定任务 ID 按既有 source+ID 规则确定性派生 Bot；Bot 名、标题、摘要、标签、外链头像、`external_id` 遵循 REST 长度和 URL 校验。数据库事务仍由 `Store.CreateReport` 处理；新报告广播原 `report.created` SSE，重复 `external_id` 返回旧 `report_id` 并不再次广播。结构化响应只返回 `report_id`、`created`、`duplicate`、source、Bot ID 和执行时间，不包含正文。缺少 external_id 时沿用 REST 行为，不保证跨请求重试幂等。

`search_reports.query` 映射 REST 的 `q`，其余 `source`、`bot_id`、`from`、`to`、`limit`、`cursor` 直接复用 `Store.Search` 的 FTS5、筛选与稳定游标；日期为含首尾 `YYYY-MM-DD`，默认 limit 为 10、最大 20。MCP 搜索结果只投影报告 ID、来源、Bot ID/展示名、标题、摘要、执行时间和标签，不返回完整 Markdown；空结果为 `items: []`。过滤、FTS 查询和分页继续由现有存储层负责，不创建第二套 SQL 检索逻辑。

客户端需支持自定义远程 MCP、Streamable HTTP 和 Bearer 凭据，并将服务端 `ACCESS_TOKEN` 存在自身安全凭据配置中。README 的可选 Codex CLI 示例使用 `BOLT_DOCK_TOKEN` 作为该客户端本地环境变量名；它不是 MCP 标准字段或服务器必需变量。具体产品版本、托管执行环境网络可达性及生产反向代理仍需针对实际部署逐项验证；SDK 集成测试通过不代表所有客户端兼容。

## 12. P8 可选 OAuth MCP

P8 在 P7 `/mcp` 上可选启用 OAuth Authorization Code + PKCE S256。`OAUTH_ENABLED` 默认为 false；开启后要求 `PUBLIC_BASE_URL` 是固定 HTTPS origin（本地隔离测试可用 loopback HTTP），不接受路径、查询参数或从请求 Host/转发头推导。部署前仍需核对实际反向代理转发路径和 `WWW-Authenticate`。

授权服务器端点：`GET /.well-known/oauth-protected-resource/mcp`、`GET /.well-known/oauth-authorization-server`、`GET /oauth/authorize`、`POST /oauth/consent`、`POST /oauth/token`、`POST /oauth/revoke`。授权管理 API 使用网页登录会话：`GET/POST /api/v1/oauth/clients`、`DELETE /api/v1/oauth/clients/{client_id}`、`GET /api/v1/oauth/grants`、`DELETE /api/v1/oauth/grants/{grant_id}`。公开客户端可不设 secret；机密客户端使用只展示一次的随机 secret，SQLite 仅保存 SHA-256 摘要。只支持所有者手工预注册的回调 URI，精确字符串匹配；HTTP redirect 仅允许 localhost/loopback。

授权码绑定 client、redirect URI、PKCE S256 challenge、resource、scope，5 分钟过期且事务一次性兑换。Access token 是随机不透明值，摘要存储、15 分钟过期；每个 MCP 请求检查 token、grant、client 状态及 resource。授权有效期 30 天；撤销 grant/client 即时撤销现存 access token。`reports:read` 只允许 `search_reports`，`reports:write` 只允许 `publish_report`，MCP HTTP 层对缺 scope 的调用返回 403；OAuth token 不通过 REST Bearer 鉴权。未实现 refresh token、CIMD、DCR、JWKS。

SQLite schema v3 包含 `oauth_clients`、`oauth_grants`、`oauth_authorization_codes`、`oauth_access_tokens`、`oauth_audit_events`；v3 为 OAuth client 增加注册来源字段，以区分手动预注册与 CIMD。启动迁移不依赖 feature flag，关闭功能不删除表。持久授权主体固定为单所有者 `owner`；读 scope 可访问整个报告库，写 scope 可声明 source/Bot 标识，不构成多租户或来源隔离。生产启用、实际 TLS/反代、数据库备份、镜像发布与 Grok/Claude/Cursor OAuth 联调不在本地代码验收范围内，状态见 P8 验收报告。
