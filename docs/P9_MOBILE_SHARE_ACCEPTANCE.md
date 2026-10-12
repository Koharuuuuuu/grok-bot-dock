# P9 移动端适配与限时分享验收报告

日期：2026-10-12
基线：`0e82735` (`master`, `P9 mobile UI and expiring sharing roadmap`)
实现分支：`feature/p9-mobile-expiring-shares`

## 结果摘要

P9 的移动端顶部工具分组、单报告限时分享、独立匿名阅读页、SQLite 持久化、过期清理、日志脱敏和相关文档已在本地实现并通过下列自动化与隔离验收。代码仍在本地功能分支；尚未推送或合并到 `master`，GHCR 没有本次构建镜像。生产反向代理、真实手机 Safari/Android Chrome 与生产部署未验证。

## 审计与实现

- 基线审计记录在 [P9_BASELINE_AUDIT.md](P9_BASELINE_AUDIT.md)。当前首页将筛选、主题与低优先级操作塞在同一顶栏；窄屏控件互相挤压。分享管理严格要求浏览器所有者会话，Legacy Bearer 和 OAuth MCP 凭据不能管理分享。
- 移动端将来源筛选与主题设置组成首要工具行，将 REST/MCP、统计和退出等操作放在次要行；320px 到 1440px 的模拟视口均无页面或顶栏横向溢出。长来源选项保留完整 title，卡片徽标单行省略并提供完整 title。来源 select 已在浏览器中实际选择长名称，控件尺寸 249×44 CSS px。
- 主题菜单的浅色、深色和跟随系统三项均在 320px 模拟视口实际操作。详情阅读、系统色彩偏好等完整 P9 回归仍需真实设备覆盖。
- 分享令牌为 32 字节密码学随机数、无填充 base64url；SQLite 仅保存 SHA-256 摘要。分享有效期是创建时长，小时 1–168 或天 1–7，默认 1 天，每份报告最多 20 个有效链接。
- 分享表为 additive SQLite schema migration v4，报告外键删除时级联。服务启动时和每小时按最多 200 条分批清理过期/撤销记录；每次公共读取仍独立核验到期、撤销和报告存在性。
- 管理接口只接受网页登录会话并做 Origin 校验；匿名读取只返回白名单报告字段。公共链接无效、过期、撤销或报告已删除均返回统一 404。分享响应设置 no-store、no-referrer、noindex、nosniff 与 CSP；公开 Markdown 不加载远程图片。应用请求日志会将 `/share/*` 和公开分享 API 路径脱敏。

### API 与存储契约

| API | 请求/响应要点 |
| --- | --- |
| `POST /api/v1/reports/{id}/shares` | Owner session；请求 `{"ttl_unit":"hour"|"day","ttl_value":1..168|1..7}`；创建一次性明文令牌，响应 `share_id`、相对 `share_path`、`expires_at`。前端基于当前站点 origin 构造完整链接。 |
| `GET /api/v1/reports/{id}/shares` | Owner session；只返回 `share_id`、创建/过期时间和 active/expired/revoked 状态，不返回 token、报告 ID 或 URL。 |
| `DELETE /api/v1/reports/{id}/shares/{share_id}` | Owner session；成功返回 204；撤销后公共访问立即失败。 |
| `GET /api/v1/public/shares/{token}` | 无需登录；仅返回来源、Bot 展示名、标题、摘要、标签、执行时间、Markdown 正文和过期时间。无效、过期、撤销、报告删除统一 404。 |
| `GET /share/{token}` | SPA 独立匿名阅读页；直接访问和刷新正常，不发起工作台会话探测。 |

SQLite v4 表 `report_shares` 包含 `id`、`report_id`、`token_hash`、`created_at`、`expires_at`、`revoked_at`；`token_hash` 唯一，`report_id` 外键 `ON DELETE CASCADE`，并有报告/到期时间索引。公开令牌从不存入表。

## 自动化与隔离验证

| 检查 | 结果 |
| --- | --- |
| `make test` | 通过；Go 测试通过，Vitest 17/17 |
| `make build` | 通过；Vite 生产构建与 Go 二进制构建通过 |
| `go test -race ./...` | 通过 |
| `go vet ./...` | 通过 |
| `git diff --check` | 通过 |
| `docker build -t bolt-dock:p9-local .` | 通过；本机 Linux/ARM64 Docker 环境 |
| 隔离容器流程 | `/healthz` 200、Bearer 报告推送 201、网页登录 200、同源创建分享 201、无 Cookie 匿名读取 200；容器重启后健康检查 200 且分享仍可读取 200 |
| SQLite | 迁移、并发最多 20 条限制、过期/撤销/删除级联、清理及数据库备份恢复测试通过 |

隔离 Docker 验收使用独立临时容器与命名卷；测试后已删除。未连接、读取或改动生产容器、卷或报告。

## 浏览器与截图证据

使用 Playwright 驱动的 Chromium 进行本地交互验证。视口为 320、360、375、390、414、430、768、1024、1280、1440 CSS px；每个尺寸的 `documentElement.scrollWidth` 与顶栏实际宽度均等于视口宽度。实际验证了长来源选项选择、三种主题菜单、登录态分享创建和复制反馈、无登录分享页直接访问、撤销后刷新进入统一不可用页。

- 深色工作台 320px：[final-mobile-320.png](../output/playwright/p9/final-mobile-320.png)
- 浅色工作台 320px：[final-mobile-320-light.png](../output/playwright/p9/final-mobile-320-light.png)
- 匿名分享页深色 320px：[share-anonymous-320-dark.png](../output/playwright/p9/share-anonymous-320-dark.png)
- 分享管理弹窗 320px：[share-modal-320.png](../output/playwright/p9/share-modal-320.png)

视口为 Chromium 仿真，不等同真实 iOS Safari / Android Chrome。当前截图中的报告是本地临时样例，没有用户生产数据。

## 修改范围与文档

- 后端：`internal/api/shares.go`、`internal/store/shares.go` 与路由/日志处理；添加 API 与 SQLite 测试。
- 前端：移动端顶栏分组、分享管理弹窗、独立分享页、公开正文图片策略和安全响应元信息。
- 构建：更新 `internal/webui/dist`，确保 Go embed 使用对应前端资源。
- 文档：[README.md](../README.md)、[README.en.md](../README.en.md)、[TECHNICAL_DESIGN.md](TECHNICAL_DESIGN.md) 已补充限时分享的接口、安全边界与部署日志说明。

## 反向代理、生产边界与回滚

生产代理须将 `/share/*` 与 `/api/v1/public/shares/*` 原样转发到应用；访问日志不得保存这些路径里的原始令牌。README 提供 Nginx access log 处理示例。TLS、真实代理日志行为、生产域名和公网匿名链路尚未实测。

当前没有生产发布。若本分支将来获准部署后需要回滚，可切回已知旧镜像并重建应用容器，保留 `/data` 卷；本次分享表是 additive migration，保留它不会影响旧版本读取。不要删除 Docker 数据卷，也不要恢复旧数据库备份覆盖新数据。公共分享端点与入口只有随新版本才可用；切回旧镜像会暂时关闭分享访问。

## 未验证项

- 真实 iPhone/iPad Safari、Android 实机 Chrome、横屏、软键盘、安全区、字体缩放和 200% 浏览器缩放。
- 真实生产反向代理的 access log、TLS、域名路由、匿名互联网访问与部署升级/回滚。
- 人工确认的生产敏感报告分享策略；分享是持有链接即可阅读，已下载/截图的内容不能远程撤回。

因此，本报告证明的是本地实现和隔离环境验收，不代表生产发布或真机验收完成。
