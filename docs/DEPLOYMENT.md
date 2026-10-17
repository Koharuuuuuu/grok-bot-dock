# 部署与运维

本文只覆盖服务安装、运行、升级、反向代理和数据保护。Agent 与 OAuth 的连接配置见 [Agent 接入指南](AGENT_INTEGRATION.md)。

## 生产部署：GHCR 镜像

需要 Docker Engine 和 Compose 插件。`.env` 中必须设置随机 `ACCESS_TOKEN`：

```sh
cp .env.example .env
# 编辑 .env，设置随机 ACCESS_TOKEN
docker compose pull
docker compose up -d
docker compose ps
docker compose logs --tail=100 bolt-dock
```

服务默认绑定宿主机 `127.0.0.1:18765`，数据保存在 `bolt-dock-data` 命名卷。将 HTTPS 反向代理指向 `127.0.0.1:18765` 后再从公网访问。升级时执行 `docker compose pull` 和 `docker compose up -d`；开始前按[备份与恢复](#备份与恢复)保存数据。**不要使用 `docker compose down -v`**，它会删除数据卷。

可在 `.env` 设置 `BOLT_DOCK_PORT` 修改宿主机端口，设置 `BOLT_DOCK_TAG` 选择镜像标签。Compose 限制为 2 CPU、1600 MiB，并为正常停机留出 30 秒。

如果 GHCR 镜像包设为私有，先使用有读取权限的 GitHub 账号执行 `docker login ghcr.io`；公开包可以匿名拉取。

## 本地构建

在代码仓库目录中执行：

```sh
docker compose -f compose.local.yml build
docker compose -f compose.local.yml up -d
docker compose -f compose.local.yml ps
```

数据保存在 `bolt-dock-local-data` 命名卷。此配置的 Compose 服务名仍是 `bolt-dock`，容器名是 `bolt-dock-local`。备份和恢复命令见下文。

## 配置

| 变量 | 必需 | 说明 |
| --- | --- | --- |
| `ACCESS_TOKEN` | 是 | 管理 API、工作台和默认 MCP Bearer 凭据；设置长随机值 |
| `PORT` | 否 | 服务容器内监听端口，默认 `18765` |
| `DATA_DIR` | 否 | SQLite 数据目录，默认 `./data`；容器中为 `/data` |
| `MAX_REQUEST_BYTES` | 否 | 请求 JSON 最大字节数，默认 `1048576` |
| `OAUTH_ENABLED` | 否 | 默认 `false`；只有完成公网配置后才启用 |
| `PUBLIC_BASE_URL` | OAuth 启用时必需 | 固定规范 HTTPS origin，不含路径；不能从请求 Host 推导 |

服务拒绝没有 `ACCESS_TOKEN` 的启动。启用 OAuth 时，`PUBLIC_BASE_URL` 必须是固定 HTTPS origin；只在本机测试时允许 localhost/loopback 使用 HTTP。OAuth 实现范围和未验收的客户端见 [Agent 接入指南](AGENT_INTEGRATION.md)。

## HTTPS 反向代理

最小 Caddy 示例：

```caddyfile
reports.example.com {
    reverse_proxy 127.0.0.1:18765
}
```

如果服务暴露到公网，必须使用 HTTPS，并建议在代理层增加访问认证和限流。反向代理应覆盖 `X-Forwarded-Proto`，不得透传客户端提交的同名请求头。

MCP 使用 `/mcp` 的 Streamable HTTP，代理必须允许 POST、GET、DELETE 和相关 MCP 请求头，并关闭响应缓冲。Nginx 示例：

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

限时分享页与匿名分享 API 无需登录。若启用代理访问日志，应排除 `/share/` 和 `/api/v1/public/shares/` 路径或对完整 URI 中的令牌脱敏。

## 备份与恢复

SQLite 使用 WAL 模式。收到 Docker SIGTERM 后，Bolt Dock 会关闭 SSE 订阅、停止接收新请求、等待正在处理的请求结束，并在单独的超时窗口内执行 `wal_checkpoint(TRUNCATE)`。只有日志明确出现 `shutdown complete; SQLite WAL checkpointed`，才可只复制主 `bolt-dock.db` 文件。

备份前正常停止服务，并确认日志中的 checkpoint 成功信息：

```sh
docker compose stop --timeout 30 bolt-dock
docker compose logs --tail=50 bolt-dock
docker compose cp bolt-dock:/data/bolt-dock.db ./bolt-dock-backup.db
docker compose start bolt-dock
```

若使用本地构建配置，每条 Compose 命令加 `-f compose.local.yml`，服务参数仍是 `bolt-dock`。例如：

```sh
docker compose -f compose.local.yml stop --timeout 30 bolt-dock
docker compose -f compose.local.yml logs --tail=50 bolt-dock
docker compose -f compose.local.yml cp bolt-dock:/data/bolt-dock.db ./bolt-dock-backup.db
docker compose -f compose.local.yml start bolt-dock
```

恢复时先正常停止服务，再把备份复制回数据目录并启动：

```sh
docker compose stop --timeout 30 bolt-dock
docker compose cp ./bolt-dock-backup.db bolt-dock:/data/bolt-dock.db
docker compose start bolt-dock
```

本地构建配置将命令中的 `docker compose` 替换为 `docker compose -f compose.local.yml`。如果容器被强制终止、checkpoint 报错或日志没有成功信息，**不要仅复制或覆盖主 `.db` 文件**；应使用 SQLite 一致性备份，或在服务完全停止时一并保留 `.db`、`-wal` 和 `-shm` 文件。升级迁移和回退限制见[主题重构验收记录](TOPIC_REFACTOR_ACCEPTANCE.md#升级与回退)。

## 健康检查与故障排查

`GET /healthz` 不需要 Token，Compose 会每 30 秒检查一次。先检查状态和日志：

```sh
docker compose ps
docker compose logs --tail=100 bolt-dock
```

- 容器无法启动：检查 `.env` 中 `ACCESS_TOKEN` 是否存在，OAuth 启用时检查 `PUBLIC_BASE_URL`。
- 工作台或推送返回 `401`：确认登录密钥或 Bearer Token 与服务端 `ACCESS_TOKEN` 一致。
- `/mcp` 返回 `404`：检查代理是否将 `/mcp` 原样转发给服务。
- 来源列表为空：服务尚未收到成功的报告推送。
- SQLite 备份不完整：检查正常停机日志中的 WAL checkpoint 结果；不要通过删除数据卷排查。

## CI 与镜像发布

`.github/workflows/ci.yml` 在 Pull Request 和 `master` push 上运行 Go、前端验证；满足发布路径条件后构建并推送 amd64/arm64 GHCR 镜像。版本标签发布对应版本镜像。具体路径判断与工作流权限以仓库中的 CI 文件为准。
