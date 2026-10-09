# Bolt Dock V1 验收报告

日期：2026-10-09（Asia/Shanghai）
基线：远端 `master` 最新提交 `ee9c48e`；实现 PR #1，GHCR 多架构修复 PR #2–#3。
结果：实现、所有 PR、master CI、GHCR 公开双架构发布、amd64/arm64 匿名拉取和 arm64 容器健康验收均通过。

## 完成内容

- Go + Vue 3/Vite 单容器服务，嵌入静态页面，支持 SPA fallback、`/healthz`、默认 `18765` 端口及配置化数据路径。
- SQLite 版本记录、WAL、外键、busy timeout、Bot/报告/来源表、FTS5 trigram、索引与事务触发器。来源必填且由首条推送自动注册，Bot 按 `(source, bot_id)` 隔离；`external_id` 在该身份范围幂等。
- Bearer Token 同时保护报告读写 API；缺少 Token 时拒绝启动。报告大小、时间、元数据、来源和 HTTPS 头像均校验；推送按客户端地址每分钟限制 120 次。
- 来源、Bot、报告、详情、日期过滤、游标分页、中文全文搜索、SSE、阅读进度及统计 API。数据库容量统计主库、WAL 与 SHM，并始终标为全库数据。
- Bolt Dock 无侧栏工作台：动态来源下拉、全局搜索及日期筛选、响应式卡片、右侧历史阅读面板、沉浸式阅读、头像回退、Markdown XSS 消毒和用户主动开启的浏览器通知。
- 复制 AI 推送指令弹窗支持配置地址/source/Bot 信息、完整模板预览、剪贴板成功提示和手动复制回退；模板仅包含 Token 占位符，并限定只推送正式报告。
- GitHub CI 和 GHCR 发布 workflow、本地构建与 GHCR Compose、`.env.example`、部署/HTTPS 反代/备份/恢复/更新/排错说明。

## 自动化验证

| 命令 | 结果 |
| --- | --- |
| `go test ./...` | 通过；覆盖 API 校验/鉴权、幂等、来源隔离、中文搜索、并发写入、分页、头像保留、持久化、SSE 和限流。 |
| `go test -race ./...` | 通过。 |
| `go vet ./...` | 通过。 |
| `cd frontend && npm ci && npm test` | 通过；6 项测试覆盖 Markdown/XSS、检索筛选、高亮片段、卡片/SSE 身份更新及通知降级。 |
| `cd frontend && npm run build` | 通过；生产资源成功生成并嵌入 Docker 构建。 |
| `cd frontend && npm audit` | 通过；0 vulnerabilities。 |
| `git diff --check` | 通过。 |

## 本地容器验收

验收使用专属 Compose 项目 `bolt-dock-acceptance-20261009`、端口 `28765`、临时 Token `acceptance-local-only` 和独立命名数据卷；没有向正式服务发送测试报告。

- `docker compose -p bolt-dock-acceptance-20261009 -f compose.local.yml up -d --build`：多阶段镜像构建并启动成功。
- `GET /healthz`：返回 `{"status":"ok"}`；容器以 UID `10001` 运行，限制为 2 CPU / 1600 MiB，Compose 只将端口绑定到 `127.0.0.1`。
- 推送 `grok` 和 `codex` 两份同名 Bot ID 报告：均返回 HTTP 201；来源列表自动出现，两来源计数各自为 1；来源筛选仅显示对应卡片，搜索结果与中文检索正确。
- 使用相同 `(source, bot_id, external_id)` 重试：返回 HTTP 200、原报告 ID，不重复计数。
- 容器 SSE 实测：推送后收到 `event: report.created` 和报告 JSON；Go SSE 单测同时覆盖断线清理。
- 容器重启/Compose 重建后：健康检查恢复，报告及来源记录仍存在。
- Playwright 浏览器实测：登录、动态来源选择、卡片、阅读面板、中文搜索命中片段高亮、推送指令字段更新和复制成功反馈均正常；浏览器控制台无错误。390px 窄屏页面可用。

Docker 本地镜像实测大小为 **30,505,844 bytes（约 29.1 MiB）**，架构 `arm64`。在限制为 2 CPU / 1600 MiB 的本地容器中，空闲/请求后的单次内存读数约 **4.9 MiB**；瞬时 CPU 读数 **0.00%–1.75%**。这是一组轻量本地采样，不代表持续负载峰值或不同宿主机表现。

## 发布验收

PR #1 于 2026-10-09 合并 Bolt Dock V1（`c422ac5`）；PR #2–#3 完成并合并 GHCR 多架构发布（最新 master 提交 `ee9c48e`）。三个 PR 的 CI 全部通过。Master CI 和 `Publish Docker image` workflow run `37936261007` 成功。

用户确认后，GHCR 包 `ghcr.io/nyakooo/grok-bot-dock` 已设为 Public，源码仓库仍为 Private。公开 `master` 镜像 index digest 为 `sha256:4f9c4bf58ac018c481321e90fdba2a6bd236a62d815208c2e7c3da66a2fec9dc`，包含 `linux/amd64` 与 `linux/arm64`。使用不含 registry 凭据的 Docker 配置默认匿名拉取后，本机获得 `linux/arm64` 镜像；容器以 UID `10001` 启动，`GET /healthz` 返回 `{"status":"ok"}`，Docker 健康状态为 `healthy`。

```sh
docker --config /tmp/bolt-dock-anonymous-docker-config \
  --host unix:///Users/nyako/.docker/run/docker.sock \
  pull ghcr.io/nyakooo/grok-bot-dock:master
```

以上命令需使用未配置 registry 凭据的 Docker 配置；本机默认目标为 arm64。Dockerfile 原生交叉编译后的本地 OCI 双架构构建也通过，镜像索引包含两种架构。

最终报告通过 PR #4 合并后，master CI run `37937104497` 和发布 run `37937104513` 均成功。该发布重新生成了相同应用代码的多架构镜像；随后再次以无凭据配置拉取 `master`，得到 index digest `sha256:9cea7890b51b7cedad788950aec5d4870fb17ba79d4ae07d70411c684e430f83`，架构为 `linux/arm64`；启动容器后 `/healthz` 返回正常且 Docker health 为 `healthy`。PR #4 合并提交为 `d2bf913`。

真实域名、生产 HTTPS 代理配置、实际 AI 平台发起外部 POST 的能力，以及正式通知权限都没有配置或声称完成；部署方需在对应环境验证。Grok 等平台是否支持自动 HTTP POST 仍按执行计划保持未验证。
