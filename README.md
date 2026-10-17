<p align="center"><img src="assets/bolt-dock-banner.png" alt="Bolt Dock dashboard illustration" width="100%"></p>
<p align="center"><img src="frontend/public/brand/bolt-dock.svg" width="72" height="72" alt="Bolt Dock logo"></p>
<h1 align="center">Bolt Dock</h1>
<p align="center">一个轻量、自托管的 AI Agent 报告工作台</p>
<p align="center"><a href="README.md">简体中文</a> · <a href="README.en.md">English</a></p>
<p align="center"><a href="https://github.com/Nyakooo/grok-bot-dock/actions/workflows/ci.yml"><img src="https://github.com/Nyakooo/grok-bot-dock/actions/workflows/ci.yml/badge.svg?branch=master" alt="CI"></a> <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green.svg" alt="MIT License"></a></p>

Bolt Dock 将 AI Agent 生成的正式报告收集到你自己托管的工作台中。Agent 通过 REST API 或远程 MCP 发布内容；你按主题整理、检索和阅读，数据保存在本地 SQLite 数据库中。

## 快速开始

需要安装 Docker Engine 和 Compose 插件。先创建访问密钥，再启动服务：

```sh
cp .env.example .env
# 编辑 .env，设置足够长的随机 ACCESS_TOKEN
docker compose pull
docker compose up -d
docker compose ps
```

在服务器本机打开 `http://localhost:18765`，用同一个 `ACCESS_TOKEN` 登录。默认只绑定宿主机回环地址；要从外网使用，请先配置 HTTPS 反向代理。完整部署、升级与备份步骤见[部署指南](docs/DEPLOYMENT.md)。

## 工作方式

Bolt Dock 以**主题**组织内容，而不是按固定模型或 Agent 划分板块。Agent 第一次推送正式报告时会创建主题，后续任务通过稳定身份或显式 `topic_id` 持续贡献。分组、关注、置顶和搜索用于整理主题，不会启动模型调用。

桌面端通过侧栏切换主题视图；窄屏使用固定底部主题导航。接入指令和设置在页面顶部保持唯一、稳定的入口。

停止主题采用请求与确认两步：先在外部平台停止任务，再确认后归档。归档保留历史并拒绝新报告；恢复主题也不会替外部平台重启定时任务。详细行为见[主题重构验收记录](docs/TOPIC_REFACTOR_ACCEPTANCE.md)。

报告支持全文检索、逐篇未读和限时分享；分享链接可撤销，但持有者已复制或保存的内容无法远程收回。

## 接入 Agent

登录后使用「复制接入指令」，把不含管理员密钥的文案交给 Agent。也可以直接使用 REST API 或 `/mcp` Streamable HTTP 服务。默认凭据是共享的 `ACCESS_TOKEN`，只适合所有者本人或受信任的单用户环境；服务不提供多租户隔离。OAuth 默认关闭，真实客户端兼容性需按产品和版本单独验证。

接入步骤、发布字段、工具能力与 OAuth 限制见 [Agent 接入指南](docs/AGENT_INTEGRATION.md)。

## 备份与安全

容器正常停止时会关闭 SSE 连接、等待 HTTP 请求结束并检查点 SQLite WAL。只有日志出现 `shutdown complete; SQLite WAL checkpointed` 后，才可以只复制 `bolt-dock.db`。若停机被强制终止或检查点失败，不能只复制主数据库文件。完整流程见[部署指南的备份与恢复](docs/DEPLOYMENT.md#备份与恢复)。

不要把访问密钥放进提示词、报告正文、URL、日志或共享配置。报告分享链接是持有者凭据；只分享适合公开阅读的内容。更多安全边界见 [SECURITY.md](SECURITY.md)。

## 文档

| 你要做什么 | 从这里开始 |
| --- | --- |
| 安装、升级、反向代理、备份与恢复 | [部署指南](docs/DEPLOYMENT.md) |
| 配置 Agent、REST API、MCP 与 OAuth | [Agent 接入指南](docs/AGENT_INTEGRATION.md) |
| 查找全部设计、验收和历史文档 | [文档索引](docs/README.md) |
| 了解架构与数据模型 | [技术设计](docs/TECHNICAL_DESIGN.md) |

## 本地开发

需要 Go 1.25+、Node.js 22.22.2+ 或 24.15.0+ 和 npm。

```sh
cp .env.example .env
# 设置随机 ACCESS_TOKEN
make test
make build
set -a; . ./.env; set +a; ./bolt-dock
```

前端开发服务器：`cd frontend && npm ci && npm run dev`。完整检查命令见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 贡献与许可

欢迎提交问题和 Pull Request。请先阅读[贡献指南](CONTRIBUTING.md)，漏洞请按[安全政策](SECURITY.md)私下报告。项目采用 [MIT License](LICENSE)。
