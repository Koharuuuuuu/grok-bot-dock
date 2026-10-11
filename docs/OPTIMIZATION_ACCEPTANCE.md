# P5 优化与验收报告

日期：2026-10-10
分支：`fix/p5-session-responsive-ci`
基线：`cf27853`（拉取后的 master）

## P5.1 登录会话稳定性

- 根因：访问密钥仅保存在 `App.vue` 的组件内存中；刷新重建应用后没有认证恢复流程，API 也只识别 Bearer Token。
- 修复：增加服务端随机会话 ID、12 小时服务端过期、HttpOnly/SameSite=Lax 会话 Cookie、HTTPS Secure 属性、登录限流、同源写请求校验、会话探测及登出撤销。网页不持有 Bearer Token；外部推送仍用 Bearer Token。
- 验证：`go test ./...` 覆盖有效/无效/过期会话、HTTPS 代理 Secure、Origin 校验、注销撤销、Cookie 清除和 Bearer 兼容；`TestSSEStopsImmediatelyAfterSessionLogout` 验证登出后已打开的 SSE 不再推送报告事件。Playwright 登录后刷新、第二标签访问成功，注销后另一标签刷新回到登录页。浏览器检查结果为 HttpOnly=true、SameSite=Lax、Path=/、无持久 Expires，`document.cookie`、localStorage、sessionStorage 均无会话凭据。
- 限制：服务进程重启会清空内存会话并要求重新登录。会话 Cookie 不设置持久期限，但浏览器会话恢复可能保留它；网页无法可靠侦测整个浏览器进程关闭，不承诺关闭浏览器即登出。HTTPS Secure 依赖 HTTPS 请求或可信反向代理覆盖 `X-Forwarded-Proto`。

## P5.2 / P5.4 详情抖动与悬浮阅读器

- 根因：阅读器原先位于内容 flex 布局内，打开时缩窄主内容；开关又改变滚动条和布局尺寸。
- 修复：阅读器变成独立的 dialog overlay，保持卡片网格尺寸；打开时锁定背景滚动并补偿滚动条宽度，退出恢复滚动位置和原焦点。支持遮罩关闭、关闭按钮、Esc、焦点循环、沉浸放大；阅读器内部独立滚动，表格/代码块局部横向滚动。
- 验证：真实浏览器在 320px 连续开关详情 3 次；每次底层主列保持 300px，页面 `scrollWidth=320`，背景锁定且焦点进入关闭按钮，Esc 关闭后主列恢复 300px、页面滚动位置为 0。长 Markdown、表格、代码块和删除控件均在详情中渲染。
- 证据：`output/playwright/p5-reader-320-open.png`、`output/playwright/p5-reader-768-open.png`、`output/playwright/p5-reader-desktop.png`。

## P5.3 移动端响应式

- 修复：窄屏头部改为品牌、全宽搜索、筛选/统计和操作分行；320px 收起非必要统计标签及复制按钮文字；卡片单列；移动端详情近全屏并将历史区与正文纵向排列。
- 验证：Playwright 在 320/375/390/430/768px 逐档检查，页面 `scrollWidth` 与 viewport 宽度相等，搜索、来源、统计、退出、卡片与详情均未横向越界；代码块局部滚动。桌面 1440px 阅读器测得宽 1280px，卡片主列尺寸不受打开状态影响。
- 复制指令弹窗在 320/375/768px 均未溢出，字段都在视口内；证据：`output/playwright/p5-prompt-320.png`。
- 证据：`output/playwright/p5-mobile-320.png`、`output/playwright/p5-mobile-375.png`、`output/playwright/p5-reader-320-open.png`。

## P5.5 Logo 与 favicon

- 修复：新增原创本地 SVG `frontend/public/brand/bolt-dock.svg`，用于登录页、顶栏及 README；页面 favicon 指向同一资源，标题改为 `Bolt Dock · Personal Report Space`。
- 验证：Go embed 生产构建包含 `/brand/bolt-dock.svg`；本机服务成功加载品牌图，浏览器标题正确，无第三方图片或字体依赖。

## P5.6 GitHub Actions 与镜像发布

- 基线：master push 由 `ci.yml` 与 `docker-publish.yml` 各运行一套 Go/前端验证；PR 也重复验证；master push 只执行一次双架构 build-push。
- 修复：移除重复发布工作流，将验证和依赖验证的发布 Job 合并至 `ci.yml`。master push 比较完整 `before..sha` 文件差异；`scripts/release-paths.sh` 将 Go、前端、Dockerfile、构建脚本和嵌入资源标记为镜像输入。用 `--no-renames` 保留删除和重命名前路径。PR 不做路径过滤，tag 与 workflow_dispatch 不依赖路径。一次 Buildx 调用构建单个 amd64/arm64 manifest；只有发布 Job 有 `packages: write`。同分支旧运行可取消，版本 tag 运行不可取消。
- 本地验证：`bash scripts/release-paths.sh --test` 通过，覆盖 Go/前端/Dockerfile/Workflow 发布路径及 README/docs/验收截图不发布的规则；完整差异检测使用 `git diff --name-only --no-renames`。首次全零 before 发布；发布失败可重跑失败 Job，后续 docs-only push 可手动 dispatch 失败 ref 补发。
- GitHub 验证：PR [#8](https://github.com/Nyakooo/grok-bot-dock/pull/8) 的 CI run `38014989716` 中 `validate` 通过、`publish` 按 PR 事件跳过；合并提交 `02cfdb671b8b4cd06abda9364a99fae27bd48bc5` 的 master run [38015039335](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38015039335) 中验证与发布均成功。无登录配置匿名读取 GHCR `master` manifest 与拉取镜像成功，索引 digest 为 `sha256:5a1228990ee43aa32824ec5b24fb9cbbf56c2bcf5178ef4731518cb39bdab160`，平台 manifest 为 amd64 `sha256:53f766141f57f9fd5dbe7e2adfb167191a957a6db7e27c870eedf402fe2dcc0c`、arm64 `sha256:842f4535232a9b8f6860419b39d3c6efcb573cad9e81200dce97dfb0a146f11a`。匿名拉取证明 GHCR 包公开；源码仓库仍为 Private。拉取的 GHCR 镜像运行后 `/healthz` 返回 ok，容器达到 healthy，运行 UID 为 `10001:10001`。
- 限制：未创建真实 semver tag，因此 tag 事件触发发布的 GitHub 运行未实测；workflow 配置和本地路径测试已覆盖该入口。

## P5.7 集成验证

- `make test`：Go 全包通过，前端 6 项通过；`make build`：前端 Vite 生产构建与 Go 二进制构建通过。`npm ci` audit 报告 0 vulnerabilities。
- 本地 `docker build -t bolt-dock:p5-final .`：通过；BuildKit 使用 Dockerfile 的单次前端构建和 Go arm64 交叉编译。
- 最终镜像容器验证：`/healthz` 返回 ok，Bearer 报告推送成功，容器健康状态为 healthy、UID 为 `10001:10001`；容器重启后 SQLite 报告仍可读。仅使用 `/tmp/bolt-dock-p5-final`，未操作部署 Compose 命名卷。
- SQLite 数据卷未改动 Compose 定义；API Bearer 推送协议和现有删除、搜索、通知实现保持兼容。PR #8 已合并 master，提交 `02cfdb6`；GitHub Actions 验证/发布通过，匿名 GHCR 双架构读取、拉取和运行健康检查通过，详见 P5.6。

## P6 集中修复复验（2026-10-10）

- 本轮以当前 master `a97abba40076a1a876fda30aae56c503c56572b5` 为基线，逐项重验登录、悬浮阅读器、响应式布局、阅读偏好、推送指令、Bot 展示名和品牌。完整根因/修改文件、Go/前端/Docker 命令、真实浏览器尺寸矩阵、截图路径及未验证边界见 [P6 验收报告](./P6_ACCEPTANCE_REPORT.md)。
- 本地 `go test -race ./... && go vet ./...`、前端 Vitest 8/8 + 构建、`docker build -t bolt-dock:p6 .` 和隔离容器健康检查/SQLite 报告及 display_name 重启持久性通过；报告、Bot 删除 UI 确认通过本地浏览器实测。
- P6 PR [#10](https://github.com/Nyakooo/grok-bot-dock/pull/10) 的 validate run `38029771656` 通过、publish 跳过；合并提交 [`cd89031c617a4ec12b0cf90b773912e336125303`](https://github.com/Nyakooo/grok-bot-dock/commit/cd89031c617a4ec12b0cf90b773912e336125303) 的 master run [`38029816646`](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38029816646) 中 validate/publish 均通过。匿名 Docker 配置拉取 `ghcr.io/nyakooo/grok-bot-dock:master` 的 amd64/arm64 成功，OCI index digest 为 `sha256:a3afc5cb8da33089659d831706debd6560225e10557cee95d1a08f2fabbbda8a`；用该 GHCR 镜像启动隔离容器后 `/healthz` 和报告 API 通过，容器健康且 UID `10001:10001`。
- 生产 URL/主机未提供，生产部署版本、缓存和数据库未验证；不得将本地或公开镜像结果视作生产验收。未触发 semver tag。

## 未读报告计数联动修复（2026-10-11）

- `/api/v1/sources` 为每个来源额外返回 `unread_count`，按 Bot 阅读游标汇总；原 `report_count` 仍保留给总量统计。
- 阅读报告后，前端重新加载 Bot 与来源聚合，卡片未读徽标和来源筛选项随服务端阅读状态同步；新报告经 SSE 刷新后会再次增加对应未读数。「全部来源」显示所有来源未读数之和。
- 回归覆盖：Grok/Codex 初始各 1 条未读；标记 Grok 报告已读后，Grok 来源归零而 Codex 保持 1；同一 Bot 收到新报告后，Grok 来源未读数重新变为 1，且总报告数独立更新。
- 真实浏览器复验发现并修复一个 SSE 边界：阅读面板打开时收到新报告，列表刷新会自动选中它并误标已读。现改为保留当前选中报告，不触发新报告的阅读请求；本地浏览器观察到旧报告仍打开、新报告卡片显示「未读 1 份」、来源选项和总未读同步增加。若另一客户端删除当前阅读报告，刷新会清除失效选择；Bot 删除后原有关闭面板行为保持不变。
- 本地验收：`make test`、`go test -race ./...`、`go vet ./...`、`make build` 和 `git diff --check` 均通过；Vitest 15/15。隔离容器健康检查及 API 计数联动通过。上述浏览器和容器均使用本地临时数据，未连接生产服务。
- 初版 master（尚未包含上述 SSE 边界修复）GitHub CI run [38105934042](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38105934042) 的 validate/publish 均通过，匿名方式拉取 GHCR `master` 成功，OCI index digest 为 `sha256:8d96aab43aa64174f62e54f9a7d1c81aedb66a1d97a4f09926ada2443cacc1fe`；arm64 隔离容器完成 health 和读/未读 API 联测。最终合并版本的 CI、镜像 digest 与拉取验收待本次修复合并后补录。
