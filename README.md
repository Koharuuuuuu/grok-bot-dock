<p align="center"><img src="frontend/public/brand/bolt-dock.svg" width="92" height="92" alt="Bolt Dock Logo"></p>
<h1 align="center">Bolt Dock</h1>
<p align="center">一个轻量、自托管的 AI 报告聚合工作台</p>

Bolt Dock 是自托管的 AI Bot 报告工作台。任意能发起 HTTPS 请求的任务都可以推送 Markdown 报告；报告按来源和 Bot 自动归档，在同一个网页中搜索、阅读和接收实时提醒。产品界面称为 Bolt Dock，GitHub 仓库仍为 `grok-bot-dock`。

## 能力

- Bot 首次推送后自动出现响应式卡片；按来源筛选卡片、搜索结果和 Bot/报告计数。
- 全局搜索支持中文；历史报告提供日期过滤、游标分页及 Markdown 阅读。
- Markdown 支持表格、代码、引用、链接和图片；原始 HTML 禁用，渲染结果由 DOMPurify 消毒。
- SSE 实时更新；用户主动允许后可显示浏览器通知。
- SQLite WAL 持久化、FTS5 trigram 搜索、Go 服务与 Vue 前端嵌入同一容器。
- 多来源使用 `(source, bot_id)` 隔离身份；来源只能由收到的推送自动产生。

## 本地开发

需要 Go 1.25+、Node.js 22.22.2+ 或 24.15.0+ 和 npm。

```sh
cp .env.example .env
# 编辑 .env，设置足够长的随机 ACCESS_TOKEN
make test
make build
set -a; . ./.env; set +a; ./bolt-dock
```

本地服务默认监听 `http://localhost:18765`，数据库写入 `DATA_DIR`。前端开发服务器使用 `cd frontend && npm ci && npm run dev`，并将 `/api` 代理到本地服务。

## 推送 API

`POST /api/v1/reports` 必须带 RFC3339 `executed_at` 和非空 `content`。`source`、`bot_id`、`bot_name` 可省略：空来源归为 `unknown`；省略 `bot_id` 时必须提供稳定的 `stable_task_id`，服务器会从来源和稳定任务 ID 确定性生成 Bot ID；没有可靠稳定身份时返回 400，不会随机创建 Bot。省略 `bot_name` 时使用 `task_name`，再回退到生成的 Bot ID。其他字段可选：`title`、`summary`、`tags`、`external_id`、`avatar_url`。`avatar_url` 只接受 HTTPS；不传时保留同一 Bot 已保存的头像。

```sh
curl -X POST "https://YOUR_DOMAIN/api/v1/reports" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary '{
    "source":"grok",
    "bot_id":"ai-news",
    "bot_name":"AI 行业动态",
    "executed_at":"2026-10-09T20:00:00+08:00",
    "title":"今日 AI 行业动态",
    "summary":"今日重点摘要",
    "tags":["AI","新闻"],
    "external_id":"2026-10-09-ai-news",
    "content":"# 今日 AI 行业动态\n\n- 完整 Markdown 报告"
  }'
```

新建返回 HTTP 201；同一 `(source, bot_id, external_id)` 重试返回 HTTP 200 和已有报告 ID。Token 不应写入 AI 提示词或报告正文。服务拒绝没有 `ACCESS_TOKEN` 的启动；推送 API 继续使用 Bearer Token。网页登录将密钥一次性提交到 `POST /api/v1/auth/login`，服务端签发 12 小时有效、无持久 `Expires`/`Max-Age` 的 HttpOnly、SameSite=Lax 会话 Cookie；HTTPS（含反向代理的 `X-Forwarded-Proto: https`）下设置 Secure。访问密钥不存进浏览器存储；刷新和同源新标签通过服务端会话恢复，主动退出会撤销会话并清除 Cookie。会话存在服务进程内，重启服务会要求重新登录。浏览器关闭后是否丢弃会话 Cookie 取决于浏览器的会话恢复设置；Web 应用无法可靠侦测窗口或浏览器进程关闭。生产环境应仅经 HTTPS 反向代理公开，并可在代理层增加访问认证和限流。

数据接口（阅读工作台支持会话 Cookie；外部推送继续使用 Bearer Token；`GET /healthz` 和会话状态探测除外）：

| 接口 | 用途 |
| --- | --- |
| `GET /healthz` | 容器健康检查，不需要 Token |
| `POST /api/v1/auth/login` | 使用 `ACCESS_TOKEN` 创建浏览器会话 |
| `GET /api/v1/auth/session` | 探测当前浏览器会话，不泄露凭据 |
| `POST /api/v1/auth/logout` | 立即撤销浏览器会话并清除 Cookie |
| `GET /api/v1/sources` | 已推送来源及计数 |
| `GET /api/v1/bots?source=` | Bot 卡片、未读状态和统计 |
| `PATCH /api/v1/bots?source=&bot_id=` | 浏览器会话修改或清除 Bot 展示名称（`display_name`）；Bearer-only 推送客户端不可调用 |
| `GET /api/v1/reports?source=&bot_id=&date=&cursor=` | 报告列表和历史分页 |
| `GET /api/v1/reports/{id}` | 单份报告 |
| `DELETE /api/v1/reports/{id}` | 永久删除单份报告并更新 FTS 搜索索引 |
| `DELETE /api/v1/bots?source=&bot_id=` | 删除 Bot 及其全部报告；来源下已无其他 Bot 时同步移除来源 |
| `GET /api/v1/search?q=&source=&bot_id=&from=&to=&cursor=` | 全文检索及筛选 |
| `GET /api/v1/events` | 新建/删除报告或 Bot 的 SSE 事件与心跳 |

工作台的报告列表、搜索结果和阅读面板均可删除单份报告；Bot 卡片可重命名或删除整个 Bot。展示名称存储在 SQLite `display_name` 中，与上报的 `bot_name` 和稳定 `(source, bot_id)` 分离；新报告不会覆盖自定义名称，清除展示名称可恢复使用上报名称。界面在执行永久删除前显示确认。删除 Bot 会连同其所有报告一起删除，不能恢复；删除最后一个关联 Bot 后，该来源也会从来源筛选中移除。删除后的卡片、统计、历史和搜索结果会刷新，其他打开的工作台通过 SSE 同步。

默认页面将数据库容量标为“全库”，即使其他统计按来源筛选也不会把物理数据库大小伪装成来源大小。

## 一键复制 AI 推送指令

登录工作台后点「推送指令」即可复制完整模板。服务地址默认采用当前站点 origin 和 `/api/v1/reports`，可在高级设置覆盖；来源由 Agent 尽量自动识别，缺失时服务端归类为 `unknown`。稳定任务 ID 由 Agent 从平台稳定 ID 或稳定任务名派生，服务器据此确定性生成 Bot ID；缺少可靠身份时拒绝推送，不按日期或随机值新建 Bot。模板包含执行端 `ACCESS_TOKEN` 安全配置要求、最终报告边界、单次执行幂等、移动端剪贴板失败手动复制提示及平台不支持 HTTP 时的处理要求。剪贴板不可用时，预览框会自动选中文本，可手动复制。

发送规则：只推送已经完成并通过检查的正式报告。草稿、测试、模拟数据、日志、中间结果、失败和取消任务不得推送。Grok 或其他 AI 平台是否具备定时 HTTPS POST 能力必须在对应平台单独验证；不得把本地联调请求发向正式服务端。

## 远程 MCP 接入

Bolt Dock 是独立自托管 MCP Server，不依赖 Codex、本机 Agent 进程或额外容器。现有 Go 服务在 `/mcp` 提供无状态 **Streamable HTTP**；MCP 与 REST 共用报告校验、SQLite、幂等及 SSE，REST API 保持不变。支持自定义远程 MCP、Streamable HTTP 与 Bearer 凭据的客户端可连接服务，并按客户端能力发现和调用工具。具体产品、版本和托管环境的支持情况须单独验证，不能由协议本身推断。

当前工具为 `publish_report`（写入已完成的正式报告）和 `search_reports`（只读历史检索）；不提供删除、改名或管理凭据工具。工作台的「MCP 接入」面板从服务端读取真实工具能力，并显示当前页面地址推导出的 `/mcp` URL。若 HTTPS 反向代理使用了其他公开域名或路径，可在面板覆盖地址；该覆盖仅保存在当前浏览器，不会配置服务器。localhost 或内网地址只对能访问相应网络的客户端可用，不代表公网地址。

客户端连接字段：MCP URL 为 `https://<你的公开域名>/mcp`，传输为 Streamable HTTP，认证头为 `Authorization: Bearer <由服务管理员安全提供>`。协议没有统一的客户端配置文件格式；在客户端自身的安全凭据设置中配置密钥，不要将 `ACCESS_TOKEN` 放进 URL、配置模板、页面、报告、日志或剪贴板。可在工作台复制不含密钥的服务 URL 或通用接入说明。

服务器目前复用管理员配置的 `ACCESS_TOKEN` 作为共享 Bearer 密钥。这只适合服务所有者本人或受信任的单用户私有接入；持有该密钥的客户端可发布和检索报告。本版本没有独立 Agent Token、按客户端权限、撤销、过期或多租户隔离，不能把管理员密钥作为公开连接码分享。若需要开放给其他人使用，应先完成独立的 Agent Token 安全设计与实现。

连接步骤：复制 MCP 服务 URL；在兼容客户端中配置该 URL 和安全保存的 Bearer 凭据；允许客户端调用 `publish_report`，然后在工作台查看报告。客户端发现工具并不保证会自动发布，调用行为取决于客户端能力、授权和任务设置。

常见排查：`401` 时检查 Bearer 密钥；`404` 时检查 HTTPS 反向代理是否转发 `/mcp`；连接失败时确认客户端支持远程 Streamable HTTP，而不是只支持 stdio。服务端允许没有 `Origin` 的已认证服务端客户端和同源请求，会拒绝不允许的跨源 `Origin`；浏览器型客户端需确认其连接模式与 Origin 策略兼容。不要用页面打开或健康检查结果推断 MCP 对特定客户端可达。

### 可选客户端示例：Codex CLI

Codex CLI 示例使用本地环境变量名 `BOLT_DOCK_TOKEN`：

```toml
[mcp_servers.bolt_dock]
url = "https://<你的公开域名>/mcp"
bearer_token_env_var = "BOLT_DOCK_TOKEN"
```

`BOLT_DOCK_TOKEN` 只是该示例在 Codex 运行环境中选择的变量名，不是 MCP 标准字段，也不是服务器必需的环境变量。应在客户端安全设置中将它配置为服务端 `ACCESS_TOKEN` 的值。Codex CLI `0.162.0-alpha.2` 已完成本机及 GHCR 镜像连接测试；此结果不代表 Codex 其他形态或版本均已验证。Claude、Grok 及其他客户端需要分别确认具体产品版本是否支持自定义远程 MCP 与 Bearer 凭据。可参考 [Codex MCP 文档](https://developers.openai.com/codex/mcp) 与 [Streamable HTTP MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)。

`publish_report` 示例参数（只在最终报告完成后调用；每次执行的 `external_id` 必须唯一，重试时原样复用）：

```json
{
  "source": "codex",
  "stable_task_id": "stable-task-slug",
  "bot_name": "任务名称",
  "executed_at": "2026-10-10T16:00:00+08:00",
  "external_id": "execution-unique-id",
  "title": "最终报告标题",
  "summary": "报告摘要",
  "content": "# 最终报告\n\n完整 Markdown 内容"
}
```

`search_reports` 中文检索示例：`{"query":"本周部署验收","source":"codex","from":"2026-10-01","to":"2026-10-10","limit":10}`。检索最多返回 20 条元数据和摘要，完整 Markdown 不会放进工具结果；可用返回的游标读取下一页。

Legacy MCP Bearer 仍使用共享 `ACCESS_TOKEN`；OAuth 另行按 P8 配置启用，二者并存且不互相转换。两种模式都属于单所有者服务，不提供多用户隔离。反向代理如将服务暴露到公网，应配置 HTTPS 和代理侧限流。MCP 能让客户端发现工具，不保证 Agent 只在最终报告时调用，也不提供定时触发保证；定时推送继续使用 REST API。Codex/其他客户端的具体版本兼容性以真实客户端连接测试为准；P7 历史验收见 [docs/P7_MCP_ACCEPTANCE.md](docs/P7_MCP_ACCEPTANCE.md)，P8 实测范围见 [docs/P8_OAUTH_ACCEPTANCE.md](docs/P8_OAUTH_ACCEPTANCE.md)。


## P8 MCP OAuth 2.1 安全配置

P8 本地功能分支为 `/mcp` 增加可选 OAuth Authorization Code + PKCE S256；原共享 Bearer 和 REST API 保持兼容。该代码尚未发布到 master 或 GHCR，OAuth 默认关闭。启用前配置 `OAUTH_ENABLED=true` 与 `PUBLIC_BASE_URL=https://<canonical-public-host>`；该值必须是固定 HTTPS origin，不支持 URL 子路径，不能由请求 `Host` 或 `X-Forwarded-*` 推导。仅 `localhost` / loopback 可用 HTTP 做本地隔离测试。OAuth 启用而 URL 缺失或不合规则会拒绝启动。

服务支持资源元数据 `/.well-known/oauth-protected-resource/mcp`、AS 元数据 `/.well-known/oauth-authorization-server`、授权/同意、Token、撤销端点。管理员在工作台「MCP 接入」中登记客户端名称和精确回调 URI；可选签发一次性展示的 confidential client secret。授权码有效 5 分钟，PKCE 必须为 S256；access token 为不透明随机值，数据库仅存 SHA-256 摘要，有效 15 分钟；授权最长 30 天，可按授权或客户端撤销。权限为 `reports:read`（仅 `search_reports`）与 `reports:write`（仅 `publish_report`），有效但缺少工具 scope 的请求返回 403。OAuth token 不可访问 REST API。尚不支持 refresh token、CIMD 或匿名 DCR；需要这些行为的客户端暂不兼容，应先完成客户端实测和安全评估。

OAuth 使用增量 SQLite v2 迁移，不修改既有报告/Bot/FTS 表；禁用 OAuth 不删授权表。公开启用前须验证实际反向代理原样转发 well-known、`/oauth/*`、`/mcp` 与 `WWW-Authenticate`，并完成数据库备份。**未在真实 Grok、Codex OAuth、Cursor 或 Claude 平台完成授权与工具调用实测；协议测试通过不等于这些客户端已验收。** 服务仍是单所有者模型；`reports:read` 可检索全库，写入者可声明 source/Bot 身份，不构成租户隔离。

接入及验收文档：[P8 架构](docs/P8_OAUTH_ARCHITECTURE.md)、[迁移与回滚](docs/P8_OAUTH_MIGRATION.md)、[客户端说明](docs/P8_OAUTH_CLIENTS.md)、[执行计划](docs/P8_OAUTH_EXECUTION_PLAN.md)、[基线审计](docs/P8_BASELINE_AUDIT.md)、[验收记录](docs/P8_OAUTH_ACCEPTANCE.md)。

## Docker 部署（拉取 GHCR 镜像）

1. 创建 `.env` 并设置随机密钥：`cp .env.example .env`。
2. 确认仓库拥有者已将 GHCR 包 `ghcr.io/nyakooo/grok-bot-dock` 设为 Public；公共包无需登录即可拉取。
3. 启动：

```sh
docker compose pull
docker compose up -d
docker compose ps
# Optional: remove every dangling image on this host (not only Bolt Dock)
docker image prune -f
```

默认端口为 `127.0.0.1:18765`，服务只绑定宿主机回环地址；同机 HTTPS 反向代理可连接本机该端口。可在 `.env` 设置 `BOLT_DOCK_PORT`、`BOLT_DOCK_TAG`；数据保存在 Compose 命名卷 `bolt-dock-data`。容器限制为 2 CPU 和 1600 MiB。更新镜像后执行 `docker compose pull && docker compose up -d`，数据卷会保留。不要运行 `docker compose down -v`，该命令会删除数据库卷。上面 `docker image prune -f` 是可选的宿主机级清理，会移除这台主机所有悬空镜像，不会删除卷；先确认其他容器不依赖悬空镜像。

## Docker 本地构建

使用真实的本地 Dockerfile 构建配置：

```sh
docker compose -f compose.local.yml build
docker compose -f compose.local.yml up -d
docker compose -f compose.local.yml ps
# Optional: removes every dangling image on this host
docker image prune -f
```

数据保存在独立的 `bolt-dock-local-data` 命名卷。更新代码后重复 build 和 up。上述两种安装方式均可选运行 `docker image prune -f` 回收宿主机所有悬空镜像；该命令作用于整台宿主机，不带 `-a` 且不删除数据卷。请先确认其他容器不依赖悬空镜像。

## 备份与恢复

暂停服务后将 SQLite 文件复制到受保护的备份位置，再启动服务。恢复时也先停止服务，替换 `bolt-dock.db` 后启动。停服可保证 WAL 检查点完成；不要在数据库正在写入时仅复制主 `.db` 文件。

```sh
docker compose stop bolt-dock
docker compose cp bolt-dock:/data/bolt-dock.db ./bolt-dock-backup.db
docker compose start bolt-dock
```

本地构建配置将上面的 Compose 文件名改为 `compose.local.yml`、服务容器名改为 `bolt-dock-local`，数据库路径仍为 `/data/bolt-dock.db`。恢复前把容器停下，用 `docker compose cp ./bolt-dock-backup.db bolt-dock:/data/bolt-dock.db` 复制回去，再启动。

## HTTPS 反向代理与故障排查

最小 Caddy 配置示例（把域名替换为已解析到本机且证书可签发的真实域名）：

```caddyfile
reports.example.com {
    reverse_proxy 127.0.0.1:18765
}
```

Bolt Dock 自身仍要求 Bearer Token；可在现有反向代理再加一层访问认证。推送、网页阅读和 SSE 都走同源 HTTPS。排查时先运行 `docker compose ps` 和 `docker compose logs --tail=100 bolt-dock`：健康检查失败时核对 `ACCESS_TOKEN` 与端口；API 401 表示网页或推送方的 Token 不匹配；来源列表为空表示还没有成功推送；不要通过 `docker compose down -v` 排查数据库问题，以免删除持久卷。

MCP 反向代理须保留 `/mcp` 下的 POST、GET、DELETE 和 MCP 请求头；不要缓冲或改写 `text/event-stream` 响应。Caddy 的 `reverse_proxy` 默认支持流式响应。若使用 Nginx，需为 `/mcp` 关闭代理缓冲并延长读取超时，例如：

```nginx
location = /mcp {
    proxy_pass http://127.0.0.1:18765/mcp;
    proxy_http_version 1.1;
    proxy_buffering off;
    proxy_read_timeout 1h;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

## GitHub Actions 与 GHCR

`.github/workflows/ci.yml` 是唯一的验证和发布工作流：PR 始终运行 Go 测试、前端测试和构建，发布 Job 不会在 PR 中运行；master push 仍执行一次验证，只有 Go/前端/镜像运行时构建输入变更才发布 `:master`。版本标签 `vX.Y.Z` 发布 `:X.Y.Z` 和 `:latest`，`workflow_dispatch` 可对选定 ref 手动发布或重试。路径判断比较一次 push 的完整 `before..sha` 文件差异，文档和图片证据不发布镜像；重命名/删除也会匹配原路径。发布 Job 依赖验证成功，并用一次 Buildx 操作推送 amd64/arm64 manifest。验证 Job 仅需要 `contents: read`，发布 Job 另需 `packages: write`，认证使用短期 `GITHUB_TOKEN`。发布失败可在 GitHub Actions 中重跑失败 Job；若其后有 docs-only push，可手动 dispatch 失败版本 ref 补发。

GHCR 包的公开可见性由仓库拥有者在 GitHub Packages 页面设置：打开 `grok-bot-dock` 容器包的 **Package settings → Change visibility → Public**。保持源码仓库原有私有设置。2026-10-10 已对 `ghcr.io/nyakooo/grok-bot-dock:master` 完成匿名多架构 manifest/blob 读取及 arm64 拉取验证；当前 digest 和发布记录见 [P7 验收报告](docs/P7_MCP_ACCEPTANCE.md)。

## 测试

```sh
go test ./...
cd frontend && npm ci && npm test && npm run build
```

完整验收及限制见 [docs/ACCEPTANCE_REPORT.md](docs/ACCEPTANCE_REPORT.md)；P6 本轮逐项证据见 [docs/P6_ACCEPTANCE_REPORT.md](docs/P6_ACCEPTANCE_REPORT.md)。
