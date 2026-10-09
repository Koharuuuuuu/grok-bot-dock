# Bolt Dock

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

`POST /api/v1/reports` 必须带 `source`、`bot_id`、`bot_name`、RFC3339 `executed_at` 和非空 `content`。其他字段可选：`title`、`summary`、`tags`、`external_id`、`avatar_url`。`avatar_url` 只接受 HTTPS；不传时保留同一 Bot 已保存的头像。

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

新建返回 HTTP 201；同一 `(source, bot_id, external_id)` 重试返回 HTTP 200 和已有报告 ID。Token 不应写入 AI 提示词或报告正文。服务拒绝没有 `ACCESS_TOKEN` 的启动；阅读 API 和推送 API 均需 Bearer Token。网页登录只在当前页面内存持有 Token；刷新页面后需要重新输入。生产环境应仅经 HTTPS 反向代理公开，并可在代理层增加访问认证和限流。

只读接口：

| 接口 | 用途 |
| --- | --- |
| `GET /healthz` | 容器健康检查，不需要 Token |
| `GET /api/v1/sources` | 已推送来源及计数 |
| `GET /api/v1/bots?source=` | Bot 卡片、未读状态和统计 |
| `GET /api/v1/reports?source=&bot_id=&date=&cursor=` | 报告列表和历史分页 |
| `GET /api/v1/reports/{id}` | 单份报告 |
| `GET /api/v1/search?q=&source=&bot_id=&from=&to=&cursor=` | 全文检索及筛选 |
| `GET /api/v1/events` | 新报告 SSE 事件与心跳 |

默认页面将数据库容量标为“全库”，即使其他统计按来源筛选也不会把物理数据库大小伪装成来源大小。

## 一键复制 AI 推送指令

登录工作台后点「推送指令」，填写部署地址、稳定的来源标识、Bot ID 和名称，再复制完整模板。模板包含最终报告边界、重试幂等、安全占位符及平台不支持 HTTP 时的处理要求。剪贴板不可用时，预览框会自动选中文本，可手动复制。

发送规则：只推送已经完成并通过检查的正式报告。草稿、测试、模拟数据、日志、中间结果、失败和取消任务不得推送。Grok 或其他 AI 平台是否具备定时 HTTPS POST 能力必须在对应平台单独验证；不得把本地联调请求发向正式服务端。

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

## GitHub Actions 与 GHCR

`.github/workflows/ci.yml` 在 PR 和 master 更新时运行 Go 测试及前端构建。`docker-publish.yml` 在校验通过后，为 master 发布 `:master`，版本标签 `vX.Y.Z` 发布 `:X.Y.Z` 和 `:latest`。Workflow 仅需要 `contents: read` 和发布 Job 的 `packages: write`，认证使用短期 `GITHUB_TOKEN`。

GHCR 包的公开可见性由仓库拥有者在 GitHub Packages 页面设置：打开 `grok-bot-dock` 容器包的 **Package settings → Change visibility → Public**。保持源码仓库原有私有设置。修改可见性后，从未登录 Docker 的主机运行 `docker pull ghcr.io/nyakooo/grok-bot-dock:master` 验证匿名访问。

## 测试

```sh
go test ./...
cd frontend && npm ci && npm test && npm run build
```

完整验收及限制见 [docs/ACCEPTANCE_REPORT.md](docs/ACCEPTANCE_REPORT.md)。
