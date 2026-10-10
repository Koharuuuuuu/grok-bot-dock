# Bolt Dock P6 验收报告

日期：2026-10-10；仓库：`Nyakooo/grok-bot-dock`（Private）；目标分支：`master`。
验收基线：`a97abba40076a1a876fda30aae56c503c56572b5`（本轮 fetch 后的 `origin/master`）。
验收环境：隔离本地服务 `127.0.0.1:18766`；Docker 验收容器 `bolt-dock-p6-acceptance`，只挂载 `/tmp/bolt-dock-p6-docker`，未连接或修改生产部署及生产 SQLite 卷。

## 验收结论

代码、API、前端、本地真实浏览器、隔离 Docker、PR/master CI 和匿名 GHCR 发布镜像均完成对应验证。线上部署地址/主机没有出现在仓库或当前环境中，故无法确认用户正在访问的生产镜像、缓存版本或生产数据行为；这些线上状态仍明确标记为**未验证**。未执行 semver tag 发布。

## 验收矩阵

| 项目 | 用户期望 | 当前复现与根因 | 修复路径 | 自动化测试 | 浏览器实测 | 部署版本 | 状态 |
|---|---|---|---|---|---|---|---|
| P0-01 登录刷新/退出 | 刷新、新标签保留登录；退出撤销所有标签 | 服务重启会清空内存会话；浏览器通过服务端 HttpOnly Cookie 恢复认证 | 服务端 Cookie session、同源校验、注销撤销会话并关闭 SSE；前端用 BroadcastChannel 同步退出 | `go test -race ./...`；包含会话/注销/SSE 撤销用例 | 本地登录后刷新、新标签访问；退出后另一标签立即回登录页；Cookie 不可由 `document.cookie` 读取。服务重启需重新登录 | 生产不可达，未验证 | 本地通过；线上未验证 |
| P0-02 详情抖动 | 开关详情不改变卡片宽度或滚动位置 | 滚动条补偿与 `scrollbar-gutter: stable` 同时生效会重复计算页面宽度 | 独立悬浮层；移除重复 body padding 补偿；打开/关闭恢复 body 样式及滚动位置 | 真实浏览器视口矩阵比较开关前后主网格宽度 | 1440/1280/768/430/390/375/320px 下网格开关前后宽度差均为 0；320px 当前位置 `scrollY=69` 连续开关 3 次后仍为 69、body overflow 恢复空值；页面宽度 320 | 生产不可达，未验证 | 本地通过；线上未验证 |
| P0-03 移动布局 | 320–768px 无横向页面溢出、控件遮挡；长表格/代码局部滚动 | 窄屏工具栏与宽 Markdown 内容可能超出视口 | 响应式头部/卡片/全屏阅读器；窄屏控件收缩；表格/代码容器局部滚动 | `npm test`；8/8 | 320/375/390/430/768px 与 1280/1440px 实测；页面宽度等于视口，320px 的宽表格/代码只在内容区横向滚动 | 生产不可达，未验证 | 本地通过；线上未验证 |
| P0-04 悬浮详情 | 详情覆盖主页面；最大化清晰；可关闭与键盘退出 | `.reader-panel.immersive` 低特异性导致位于模糊遮罩之下，遮罩还会截获点击 | 修复层级为 z-index 70；维持 dialog overlay、关闭按钮、Esc、焦点与背景滚动管理 | `npm test`；Go 前端嵌入构建 | 桌面与移动真实浏览器检查网格不缩窄、阅读正文清晰、工具栏可点击、日期历史可用；实测打开时焦点进入关闭按钮，Esc 关闭成功 | 生产不可达，未验证 | 本地通过；线上未验证 |
| P0-05 最大化偏好 | 全局跨 Bot/报告、刷新和重新打开沿用；窄屏不污染偏好 | 阅读模式曾可能按单个报告初始化 | 使用版本化 `bolt-dock:reader-preferences:v1` localStorage；强制窄屏全屏不写入桌面偏好 | `npm test`；偏好解析异常/版本回退及存储不可用回退用例 | 两个 Bot 间切换、服务重启后重新登录仍继承；人为写入损坏值后重开回普通模式；桌面/移动尺寸矩阵验证 | 生产不可达，未验证 | 本地通过；线上未验证 |
| P0-06 Markdown 字号 | 默认 16px，12–24px、步长 1；重置与持久化 | 字号无可见控件及可靠偏好作用域 | 新增独立版本化字号键和缩小/显示/放大/重置控件，仅缩放 Markdown | `npm test`；边界、损坏值、存储不可用和默认回退 | 浏览器实测 12/24px 边界按钮禁用、重置回 16px、异常存储回退 16px；跨 Bot/刷新持久；窄屏没有页面溢出 | 生产不可达，未验证 | 本地通过；线上未验证 |
| P0-07 推送指令 | 自动当前 origin；无必填 Bot ID；认证与稳定身份指引正确 | 原交互要求域名/Bot ID，不能满足零配置复制 | origin 默认 `/api/v1/reports`；高级项只覆盖地址/source；服务端 source 空值归 `unknown`，稳定 task id 可确定性派生 bot_id；缺身份返回 400 | `go test -race ./...` 覆盖 `unknown`、跨日稳定身份及缺身份；`npm test` | 320px 真实浏览器预览和复制：不含 `YOUR_DOMAIN`、无必填 Bot ID，说明 localhost 外部不可达并以安全环境变量配置 Token | 生产不可达，未验证 | 本地通过；线上未验证 |
| P1-08 Bot 改名与删除 | 改展示名不改身份、不被新推送覆盖；报告及 Bot 均可删除 | 原始 bot_name 变化可能覆盖名称；DB 缺用户自定义名称字段 | SQLite `display_name` 无损迁移；浏览器 session 保护 PATCH；卡片、历史、搜索统一展示；保留逐报告/Bot 删除 | `go test -race ./...` 覆盖旧库迁移、持久化、重命名与后续推送；API 权限/输入用例 | 本地 UI 改名后卡片/历史/搜索一致；新 bot_name 推送不覆盖；逐一删除报告和 Bot 的二次确认生效；隔离 Docker 重启后名称仍保留 | 生产不可达，未验证 | 本地通过；线上未验证 |
| P1-09 Logo/README/favicon | 三模块汇入 Dock 的图标统一且小尺寸清晰，无 404 | 登录/Web/文档资源需要保持一致 | README 92×92；Web、favicon、深浅色 SVG 使用一致 Dock 图形；Go embed 随镜像打包 | `npm run build`；镜像静态资源内嵌 | 本地 `/favicon.svg`、`/brand/bolt-dock.svg`、`/brand/bolt-dock-dark.svg` 均 HTTP 200；真实浏览器截图检查。GitHub README 页面渲染未单独访问验证 | 生产不可达，未验证 | 本地通过；GitHub/线上未验证 |
| P1-10 Actions/发布 | PR 只验证；真实运行时代码合并后一次多架构发布；docs-only 不发布 | 旧路径过滤与重复发布需实证核查 | 保留单一 `.github/workflows/ci.yml`；按完整 diff 筛选运行时路径；单次 Buildx multi-arch job | `bash scripts/release-paths.sh --test` 通过；docs 输入 false、Go 输入 true；PR #10 validate 通过（run `38029771656`） | PR publish 按配置跳过；master run `38029816646` 的 validate/publish 均成功且只执行一轮 Buildx；匿名拉取并运行 `master` 镜像，index digest `sha256:a3afc5cb8da33089659d831706debd6560225e10557cee95d1a08f2fabbbda8a`，含 amd64/arm64 | semver tag 未实际触发；生产部署未知 | PR/master/GHCR 验证通过；tag 和生产环境未验证 |

## P2-11 集成验证

| 检查 | 命令 / 证据 | 结果 |
|---|---|---|
| Go 测试与静态检查 | `go test -race ./... && go vet ./...` | 通过 |
| 前端测试与构建 | `cd frontend && npm test && npm run build` | Vitest 8/8 通过；Vite 构建通过 |
| Docker 构建 | `docker build -t bolt-dock:p6 .` | 通过；最终镜像 manifest `sha256:3d11fce5ef52be7b4ac666c81b9096a46b831116925a5eb86a97d99bba6b170d` |
| 隔离 Docker 健康/SQLite | `/healthz`、报告 POST/GET、重启、改名后重启读取；仅使用 `/tmp/bolt-dock-p6-docker` | 通过；容器以 `10001:10001` 运行。确认不触碰生产卷 |
| 已发布 GHCR 镜像 | 无凭据 Docker 配置匿名 pull amd64/arm64；运行镜像并检查 `/healthz` 与报告 API | index `sha256:a3afc5cb8da33089659d831706debd6560225e10557cee95d1a08f2fabbbda8a`；两个平台均 pull 成功；运行 healthy、UID `10001:10001` |
| 真实浏览器矩阵 | `output/playwright/p6/` | 1440/1280/768/430/390/375/320px；首页、阅读器、长 Markdown、表格、代码、搜索改名、移动端推送指令与对话框截图已保存 |
| Logo 资源 | 对本地 favicon/浅色/深色 SVG 发 HEAD 请求 | 三项 HTTP 200、`image/svg+xml` |
| release path | `bash scripts/release-paths.sh --test` 与 docs/source 输入检查 | 通过；文档 false、Go 源码 true |
| PR / CI / GHCR | [PR #10](https://github.com/Nyakooo/grok-bot-dock/pull/10)；[PR validate run 38029771656](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38029771656)；[master validate + publish run 38029816646](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38029816646) | 两个 run 成功；PR 的 publish 跳过，master 运行一次 Buildx 发布 |
| 线上部署、缓存、生产 SQLite | 当前环境无生产 URL/主机权限 | 未验证；不得据此声称线上已更新 |

## 浏览器证据

截图都来自 `output/playwright/p6/`，浏览器本地使用隔离测试数据：

- 桌面/手机主页：`home-1440.png`、`home-1280.png`、`home-768.png`、`home-430.png`、`home-390.png`、`home-375.png`、`home-320.png`
- 阅读器视口：`reader-final-1440.png`、`reader-final-1280.png`、`reader-final-768.png`、`reader-final-430.png`、`reader-final-390.png`、`reader-final-375.png`、`reader-final-320.png`
- 沉浸与长内容：`reader-immersive-font17.png`、`reader-long-320.png`
- Bot 改名/搜索：`renamed-reader-mobile.png`、`search-display-name-320.png`、`rename-mobile-320.png`
- 推送指令：`prompt-mobile-320.png`

初次浏览器回归捕捉到最大化阅读层低于模糊遮罩、320px 工具栏返回文字竖向换行的问题；修复后复测并保留上述最终截图。`reader-immersive-debug.png` 为缺陷复现图，不作为通过证据。

## 修复文件

- API/SQLite/session：`internal/api/server.go`、`internal/api/server_test.go`、`internal/store/store.go`、`internal/store/store_test.go`
- 前端阅读、改名与推送指令：`frontend/src/App.vue`、`frontend/src/style.css`、`frontend/src/reader-preferences.js`、`frontend/src/ui-model.test.js`
- 生产静态资源：`internal/webui/dist/`
- 使用说明与数据契约：`README.md`、`docs/TECHNICAL_DESIGN.md`

## PR、合并、发布

- PR：[PR #10](https://github.com/Nyakooo/grok-bot-dock/pull/10)（标题 `fix: complete P6 consolidated acceptance work`）
- PR validate：[run 38029771656](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38029771656) 成功；`publish` 按 PR 事件跳过
- 合并提交：`cd89031c617a4ec12b0cf90b773912e336125303`（2026-10-10 merge commit）
- master Actions：[run 38029816646](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38029816646)：validate 和 publish 均成功，只有一个多架构 Buildx job
- 匿名 GHCR manifest API 返回 HTTP 200；amd64 与 arm64 均使用无凭据 Docker 配置实际 pull 成功。`ghcr.io/nyakooo/grok-bot-dock:master` OCI index digest：`sha256:a3afc5cb8da33089659d831706debd6560225e10557cee95d1a08f2fabbbda8a`；amd64 `sha256:1f4b0acf0c3ae8f8264dd60bd39f523d49e21bfd5d9f05ef81b5732459c89a6c`；arm64 `sha256:631662a90e0b2bbcc5fb99f4b715a1283149e19234d33970afcfc711018b3502`
- 匿名拉取的 GHCR master 镜像运行后 `/healthz` 返回 `ok`，Bearer 报告 API POST/GET 通过，容器 `healthy` 且 UID `10001:10001`。只挂载新的隔离 `/tmp` 数据目录。
- semver tag Actions：本轮未触发，不能标记已验证

## 未解决/受环境限制

1. 生产部署 URL、反向代理、线上静态资源缓存和当前运行镜像 digest 未提供，均未验证；需要在可访问的真实部署上复验，不把本地结果当作线上验收。
2. GitHub README 的远端渲染未单独打开检查；README 源文件尺寸和本地 SVG 服务已核验。
3. 本轮没有创建 semver tag，因此 tag 发布事件未实际执行。
4. 隔离 Docker 测试数据与浏览器测试数据均为临时验收数据，不属于用户生产数据。
