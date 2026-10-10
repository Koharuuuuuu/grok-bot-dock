# Bolt Dock P7 MCP 验收报告

日期：2026-10-10（Asia/Shanghai）
仓库：`Nyakooo/grok-bot-dock`（Private）
验收基线：原 MCP 服务发布基线 `641e4f5`；本补充纠偏基于拉取的 master `cdb8f39`。通用接入 UI、动态能力清单、文档与回归证据记录在本节；PR/CI/镜像交付结果在合并后补录。
验收范围：MCP 独立服务定位、真实工具能力接入 UI、非 Codex MCP Inspector 客户端与隔离 Docker/浏览器测试。未连接或修改用户生产服务及生产数据库；测试容器使用独立临时密钥和数据卷并在验收后清理。

## 结论

原 P7 服务的两项 MCP 工具已通过协议、存储、REST、SSE、Codex CLI、本地 Docker 和已发布 GHCR 镜像验证。本次纠偏将工作台主入口改为通用 MCP 接入，动态读取工具清单并提供 URL/说明复制；Codex CLI 仅保留为可选示例。新版本的隔离 Docker 浏览器、MCP Inspector、完整构建与回归结果见下文新增验收记录。

MCP Inspector 作为第二个独立客户端已完成发布镜像上的工具发现和搜索。P6 全量设备矩阵和生产部署仍未验证，因此不宣称这些项目通过或声称兼容所有 MCP 客户端。

## 功能与代码映射

| 功能 | 实现 | 验收证据 |
|---|---|---|
| `/mcp` 无状态 Streamable HTTP | `internal/api/mcp.go`、`internal/api/server.go` | 官方 Go MCP SDK `v1.8.0`；初始化、协议协商、tools/list、tools/call 的 SDK 集成测试；新协议和 2025-11-25 协商测试 |
| Bearer、Origin、方法边界 | `internal/api/mcp.go` | 缺失/错误 Token、Cookie 不可代替 Bearer、跨源拒绝、错误方法和路径测试 |
| `publish_report` | `internal/api/mcp.go`、`internal/api/server.go` | 复用 REST 规范化校验、Store 事务、限流和 SSE；外部执行 ID 重试幂等；stable_task_id 稳定分组；MCP 响应不含完整正文 |
| `search_reports` | `internal/api/mcp.go` | 复用 SQLite FTS5/游标；默认 10、最大 20；日期、中文、空结果、来源/同名 Bot 隔离；仅返回元数据和摘要 |
| 通用 MCP 接入 | `frontend/src/App.vue`、`frontend/src/ui-model.js`、`internal/api/server.go` | 登录保护的能力接口驱动实际工具、字段及示例；URL 与通用说明单独复制；可选 Codex CLI 示例折叠显示 |
| 嵌入资源和完整文档 | `internal/webui/dist/`、`README.md`、`docs/TECHNICAL_DESIGN.md`、`docs/EXECUTION_PLAN.md` | README/设计说明记录独立服务、连接步骤、工具示例、HTTPS/Origin 代理及共享密钥限制；本次截图和结果在下方补录 |

MCP 工具只提供发布及读取；没有增加删除、改名、任意 SQL、URL 抓取或命令执行工具。Bearer Token 是单用户共享密钥；本期不实现 OAuth，也不宣称多租户隔离。

## 验收矩阵

| 项目 | 结果 | 证据与限制 |
|---|---|---|
| A. 鉴权、工具发现、Origin | **通过** | 无凭据和错误 Bearer 返回 401；有效 Bearer 完成初始化与工具发现；Web Cookie 被拒绝；恶意跨源 Origin 返回 403。 |
| B. MCP 写入、SSE、REST/页面读取 | **通过** | Go MCP SDK 客户端集成测试确认 MCP 写入产生原 `report.created` SSE；REST 搜索读取 MCP 报告，MCP 搜索读取 REST 报告；隔离浏览器读取本地 REST 推送的 Bot/详情。 |
| C. 幂等和稳定 Bot | **通过** | 相同 external_id 只写一条且只发一次 SSE；相同 stable_task_id 的两次新执行进入同一 Bot。 |
| D. 双向搜索 | **通过** | 中文全文检索、日期、来源、Bot、游标和空结果；grok/codex 两个来源使用相同 bot_id 时互相隔离。 |
| E. 参数、请求边界、并发、日志 | **通过** | 覆盖非法工具参数、日期/游标、limit、1 MiB 默认体积上限、无效 JSON-RPC/Accept/方法、请求取消、并发发布；另有测试确认 MCP 写入超限返回 429 且不会落库。请求日志只打印方法、路径和耗时，不输出参数或授权头。 |
| F. Go、前端、Docker | **通过** | `make test`、`make build`、`go test -race ./...`、`go vet ./...`、`docker build -t bolt-dock:p7-local .` 全部通过。容器 health 为 `healthy`、用户 `10001:10001`；替换容器后复用同一个隔离命名卷，REST 搜索仍能读回旧报告。 |
| G. 真实 Codex 客户端 | **通过（本机与 GHCR 隔离容器）** | Codex CLI `0.162.0-alpha.2` 使用 `--ignore-user-config --ephemeral` 和临时 `mcp_servers` 覆盖连接本地服务，以及匿名拉取的 GHCR master arm64 容器。两次均完成 search/publish/search；发布镜像初次搜索为空，发布成功后再次搜索命中报告 ID 1。没有写入常驻 Codex MCP 配置。 |
| G2. 第二 MCP 客户端 | **通过（MCP Inspector CLI）** | 官方 MCP Inspector CLI 使用 Streamable HTTP 连接 GHCR master 容器；`tools/list` 返回两个工具，`tools/call search_reports` 命中报告 ID 1。Schema 检查为 0 errors、13 portability warnings，警告提示可空字段的 JSON Schema `type` 数组在部分单类型方言中可移植性较低，不等于工具调用失败，也不构成对所有客户端的兼容性证明。 |
| H. 页面回归 | **P7 页面项通过；P6 全量仍部分验证** | 本地 Docker 构建和最终 GHCR master 镜像分别检查 320、375、390、430、768、1280、1440px，页面均无水平溢出；320px 下 MCP 一键复制显示成功反馈、阅读面板不超出视口；1280px 阅读面板边界在视口内。修复前 1280px 曾测得文档宽 1285px；PR #15 缩小 1121–1320px 顶栏间距后复验通过。P6 全部核心回归与设备矩阵仍未全部复验；生产部署未知。 |
| I. 交付状态 | **P7 代码与镜像交付通过** | PR [#13](https://github.com/Nyakooo/grok-bot-dock/pull/13)、[#14](https://github.com/Nyakooo/grok-bot-dock/pull/14)、[#15](https://github.com/Nyakooo/grok-bot-dock/pull/15) 已合并；#15 合并提交 `641e4f55b68eb6034a2848cc6ac6b17bb7aa01f5`。PR CI 与 master 校验通过，GHCR 多架构发布及匿名拉取结果见下文。生产 URL/主机没有提供。 |

## 命令与环境测量

| 验证 | 结果 |
|---|---|
| `make test` | Go 全部包通过；Vitest 9/9 通过。 |
| `make build` | `npm ci`、Vite production build、`go build ./cmd/server` 通过。 |
| `go test -race ./...` | 全部 Go 包通过。 |
| `go vet ./...` | 通过。 |
| `docker build -t bolt-dock:p7-local .` | 通过，Linux/arm64 镜像大小 33,432,555 bytes，本地镜像 index manifest `sha256:72f5bd0a694a4794491b10857789e1e30f3ffeedd70aec8d759199e8c2e85116`。该 digest 是本地镜像构建结果，不是 GHCR digest。 |
| GitHub PR CI | 通过，PR #13 workflow run [38037715814](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38037715814)；Go 和前端测试/构建均成功。 |
| PR #15 master CI 与 GHCR 发布 | workflow run [38038810142](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38038810142)；`validate` 和 `publish` 均成功。 |
| GHCR 匿名镜像核验 | `ghcr.io/nyakooo/grok-bot-dock:master` 可匿名读取；最终 OCI index digest `sha256:bdf2bf7d259fcbb0e5844bd0a615869dcb6d239dff450cc7e43b122f67835d70`。linux/amd64 manifest `sha256:d2ee2fbe0fc32b5f17e4403afdb9d1a14855c2d7f4d289603b3d8131ec0f8c38`、linux/arm64 manifest `sha256:630efe2e7fb21b25adbd55e48fbcd2bce40e844da3515972dfc5a1fd85910e34`；匿名读取并校验两平台各 5 个配置/镜像 blob 的 SHA-256 与大小。匿名 Docker 配置成功拉取 arm64，Docker 返回的 master digest 一致，镜像大小 33,433,175 bytes。 |
| 隔离 Docker 运行 | 使用 `--cpus=2 --memory=1600m`，绑定 `127.0.0.1:18767`，命名卷 `bolt-dock-p7-acceptance`，独立临时 Token；`/healthz` 返回 healthy，发布验收报告后替换容器并复查 FTS 搜索可读回原记录。空闲采样 `docker stats --no-stream`：CPU 0.00%，7.199 MiB / 1.562 GiB。此单次空闲数据不是负载基准。 |
| GHCR 发布镜像运行 | 在隔离容器 `127.0.0.1:18768` 使用独立临时 Token；health 为 healthy。真实 Codex CLI 工具发现、初次空搜索、发布与再次搜索均成功，搜索返回 `report_id: 1`。容器与命名卷为临时验收资源，结束后清理。 |
| Playwright | 最终 GHCR master 镜像浏览器复测：320、375、390、430、768、1280、1440px 的 `documentElement.scrollWidth` 均等于 viewport；MCP 复制按钮在 320px 可见，点击后提示“Codex MCP 配置已复制”；320px 阅读面板为 320px 且位于视口内，1280px 阅读面板范围为 x=24–1256（宽 1232px）。原 MCP 配置反馈截图见 [`mcp-config-copied.png`](../output/playwright/p7/mcp-config-copied.png)；PR #15 的 1280px 顶栏截图见 [`p7-header-fixed-1280.png`](../output/playwright/p7/p7-header-fixed-1280.png)。浏览器命令和快照存于本机临时 `.playwright-cli/` 目录，不作为提交文件。 |
| 真实 Codex CLI | `codex-cli 0.162.0-alpha.2`，临时 HTTP 本地连接；本地服务检索/发布/再检索返回 `report_id: 2`，发布后的 GHCR master arm64 容器检索/发布/再检索返回 `report_id: 1`。未验证远程 HTTPS 代理、Codex 托管环境可达性或桌面端进程继承 Token。 |
| MCP Inspector CLI | 独立客户端对 GHCR master arm64 容器执行 `tools/list` 与 `search_reports`；返回两项工具及验收报告 ID 1。Schema portability 检查 0 errors、13 warnings。 |

## PR、CI、镜像与生产环境

| 项目 | 状态 |
|---|---|
| PR | [#13](https://github.com/Nyakooo/grok-bot-dock/pull/13) MCP 实现；[#14](https://github.com/Nyakooo/grok-bot-dock/pull/14) 首轮文档；[#15](https://github.com/Nyakooo/grok-bot-dock/pull/15) 1280px 顶栏修复；均已合并至 master。最新 merge commit `641e4f55b68eb6034a2848cc6ac6b17bb7aa01f5`。 |
| GitHub PR Actions | [run 38037715814](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38037715814)；validate 成功。 |
| PR #14 文档 Actions | [run 38038416275](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38038416275)；validate 成功。合并后文档工作流 [run 38038571583](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38038571583) 成功，镜像发布按文档变更规则跳过。 |
| PR #15 CI | [run 38038767597](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38038767597)；validate 成功。 |
| 最新 master Actions / GHCR 发布 | [run 38038810142](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38038810142)；validate/publish 均成功。GHCR master index digest `sha256:bdf2bf7d259fcbb0e5844bd0a615869dcb6d239dff450cc7e43b122f67835d70`，amd64/arm64 manifests 与匿名 blob 校验结果见上。 |
| 一键复制与响应式布局 | 最终发布镜像已在 320/375/390/430/768/1280/1440px 检查无水平溢出；320px MCP 复制提示成功，阅读面板铺满且不超出视口；1280px 阅读面板边界 x=24–1256。修复前 1280px 文档宽 1285px，PR #15 修正顶栏间距后复验。截图见上。 |
| 第二 MCP 客户端 | MCP Inspector CLI 的工具发现与搜索通过；Schema 检查含 13 项 portability warnings，不代表已验证所有客户端。 |
| 生产 HTTPS、反向代理、部署镜像及 SQLite | 未验证；未提供生产 URL/主机权限。 |

## 代码审查和已知限制

- MCP 使用官方 `github.com/modelcontextprotocol/go-sdk` `v1.8.0`。Codex 配置文档和 CLI 参数已核实；真实 Codex CLI 对本机服务与已发布 GHCR 镜像均通过。
- 两个工具共用 REST 数据与业务语义，但 MCP 搜索是元数据投影；完整 Markdown 不会随搜索结果返回。
- `external_id` 为空时保留 REST 原有语义，不承诺跨请求幂等。
- 真实 Codex 运行环境能否通过 HTTPS 访问用户部署、反向代理对 SSE/取消的配置、第二客户端支持范围及生产资源情况都没有本地测试替代品。
- 独立 MCP Inspector CLI 已完成工具发现和只读搜索；其 Schema portability 检查提示 13 项可空字段方言警告，需在接入会拒绝该 Schema 方言的客户端时进一步适配。
- 本轮代码审查发现并修复：REST 写入限流重新置于 JSON 解码之前；MCP 发布/搜索字段长度写入工具 JSON Schema；MCP 写入限流共享并通过超限不落库测试；补充非法 JSON-RPC、客户端取消与跨来源同名 Bot 测试。

## P7 通用 MCP 接入定位与 UI/UX 补充验收（2026-10-10）

### 改动与截图

- 默认入口、标题和复制反馈使用「MCP 接入」「MCP 服务接入」；MCP 服务不依赖 Codex。客户端示例折叠在可选区域，`BOLT_DOCK_TOKEN` 仅在展开 Codex CLI 示例时出现。
- 新增登录保护的 `GET /api/v1/mcp/capabilities`；UI 从实际服务端清单读取协议、共享 Bearer 认证、工具字段和无密钥示例参数，不根据页面打开状态声称服务在线。
- 工具字段摘要和示例参数由 MCP 输入结构的 JSON Schema 生成；能力接口测试逐项核对 schema 字段数及示例字段，新增输入字段时页面清单会同步更新。发布工具完整列出来源、Bot/任务名称、标题、摘要、标签、头像地址及幂等字段。
- 支持当前页面 origin/path 推导 `/mcp`，以及仅保存在当前浏览器的反向代理公开地址覆盖。localhost/内网地址显示网络可达性提示；未把私有地址称为公网可达。
- 提供服务 URL、通用接入说明独立复制、三步接入指引、连接排查、密钥边界和 REST API 推送入口。共享 `ACCESS_TOKEN` 仅适合受信任的单用户私有接入；没有独立 Agent Token 管理。
- 修改前截图：[`mcp-config-copied.png`](../output/playwright/p7/mcp-config-copied.png)。修改后浅色桌面：[`mcp-general-access-light-1440.png`](../output/playwright/p7/mcp-general-access-light-1440.png)；深色桌面：[`mcp-general-access-dark-1440.png`](../output/playwright/p7/mcp-general-access-dark-1440.png)；深色移动端：[`mcp-general-access-dark-375.png`](../output/playwright/p7/mcp-general-access-dark-375.png)。截图中的 URL 为本机隔离服务地址，不代表生产公开地址。

### 非 Codex MCP 客户端与端到端数据

在隔离镜像 `bolt-dock:p7-general-access`、`127.0.0.1:18771` 和独立命名卷 `bolt-dock-p7-general-access` 上，用临时 Token 运行 MCP Inspector CLI `2.10.1`（不是 Codex CLI）：

```sh
node /tmp/p7-mcp-inspector-cache/_npx/5a9d879542beca3a/node_modules/@modelcontextprotocol/inspector/clients/launcher/build/index.js \
  --cli --transport http --server-url http://127.0.0.1:18771/mcp \
  --header 'Authorization: Bearer <隔离验收密钥>' --method tools/list
```

`tools/list` 返回 `publish_report` 与 `search_reports`。对隔离测试报告调用 `publish_report` 首次结果为 `report_id: 1, created: true, duplicate: false`；相同 `external_id` 重试返回相同 ID 且 `duplicate: true`；`search_reports` 按中文关键词检索命中同一报告。登录工作台后可见对应 Bot 卡片和报告标题。MCP Inspector 对 Schema 报告 0 errors、13 portability warnings；警告与历史记录一致，表示部分 JSON Schema 方言对可空 `type` 数组的可移植性提示。

这验证了独立 MCP 客户端库对本地服务的协议连接、发现、发布、幂等和搜索，不证明 Claude、Grok 或任意其他产品的具体版本已支持相同连接方式。

### 浏览器、复制和布局

Playwright CLI 使用真实 Chromium 登录隔离容器；验证地址与通用说明实际剪贴板文本和成功提示一致。通用说明包含正确 endpoint、Streamable HTTP、Bearer 头、工具列表、示例参数和安全边界；不包含测试密钥值或 `BOLT_DOCK_TOKEN`。自定义 `https://reports.example.com/gateway` 后得到 `/gateway/mcp`，本地存储仅包含地址覆盖；恢复页面地址后删除该覆盖。

MCP 接入对话框打开时焦点落在关闭按钮，Tab/Shift+Tab 在对话框内循环，Esc 关闭后焦点返回入口；REST 推送入口仍可打开且标题为「REST API 推送指令」。登录后刷新会话仍有效，退出后回到登录界面。浅色和深色主题均通过截图检查。

| 视口宽度 | `documentElement.scrollWidth` | MCP 对话框边界 | 结果 |
|---:|---:|---|---|
| 1440 | 1440 | x=330–1110 | 通过 |
| 1280 | 1280 | x=250–1030 | 通过 |
| 430 | 430 | x=12–418 | 通过 |
| 390 | 390 | x=12–378 | 通过 |
| 375 | 375 | x=12–363 | 通过 |
| 320 | 320 | x=12–308 | 通过 |

在 375px 发现旋转退出按钮使页面多出 2px 横向溢出，已收紧小屏按钮偏移；最终所有列出的视口均无水平溢出。

### 本次命令结果和未验证边界

| 检查 | 结果 |
|---|---|
| `make test` | 通过；Go 包通过，Vitest 13/13 通过。 |
| `make build` | 通过；`npm ci`、Vite production build、`go build ./cmd/server` 成功。 |
| `go test -race ./...` | 通过。 |
| `go vet ./...` | 通过。 |
| `docker build -t bolt-dock:p7-general-access .` | 通过；隔离容器 health 为 `{"status":"ok"}`。 |
| Claude、Grok 与其他产品客户端 | 未验证具体产品和版本；不宣称兼容。 |
| 用户生产 URL、HTTPS 反代、真实公网可达性及生产数据 | 未验证；未提供目标地址或部署主机。 |
| PR #17 | [feat: make MCP onboarding client-agnostic](https://github.com/Nyakooo/grok-bot-dock/pull/17)，提交 `17659428` 加 `15dfd4a`（Schema 字段/示例同步修复）；尚待合并。 |
| PR GitHub Actions | [run 38042295065](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38042295065)；`validate` 通过。GHCR master 发布需在合并后核验。 |

验收结束后移除了本轮独立 Docker 容器与命名卷；Playwright 临时会话目录 `.playwright-cli/` 保留为本机未跟踪用户数据。
