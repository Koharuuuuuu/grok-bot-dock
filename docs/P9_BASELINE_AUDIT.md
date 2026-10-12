# P9 基线审计

日期：2026-10-12（Asia/Shanghai）
审计基线：`0e82735`（本地 `master`，与 `origin/master` 同步）
数据：临时本地 SQLite；没有读取或写入生产数据。

## 移动端页面与复现

- 顶栏由 `frontend/src/App.vue` 中品牌、搜索表单、单个 `.top-actions` 组成；来源 select、主题、REST/MCP、统计和退出都共享 `.top-actions`。
- `frontend/src/style.css` 的 540px 断点将顶栏改为三行，但操作控件仍挤在单一不换行 flex 行中；小于 360px 才隐藏部分文案。来源 select 允许原生控件压缩，长来源名和未读数字不保证完整可见，绝对定位的 caret 可能覆盖选中值。
- 320px 本地 Chromium 仿真可复现选中来源被压缩为“全部来源”，主题/推送/接入/统计/退出按钮集中挤在一行，来源长名在卡片也被不必要地多行断开。视口与页面 `scrollWidth` 均为 320px，因此此样例是可读性和分组问题，不是整页横溢；仍需逐尺寸检查详情、弹层和打开菜单时的横溢。
- 复现方式：临时本地服务 `127.0.0.1:18766`，使用仅本地的临时测试凭据登录，创建来源 `a-very-long-source-name-for-mobile-layout` 的一条隔离样例，设置视口 320×850。截图：[`baseline-home-320.png`](../output/playwright/p9/baseline-home-320.png)。页面指标：document width 320 / scrollWidth 320；topbar width 320 / scrollWidth 320。
- 可见问题不只颜色：移动操作缺少主次层级、低频操作未收纳、触控面积偏小；原生 select 的 option popup 无法由普通 DOM 截图完整测量，必须实际打开并选项验证。

## 路由、鉴权与数据

- `internal/api/server.go` 把 `/api/` 统一挂到 `auth()`，同时接受 ACCESS_TOKEN Bearer 和所有者会话 Cookie。面向报告分享的管理接口必须另外要求 `sessionContextKey`，不可让推送/MCP 共享 Bearer 访问。
- 现有 `GET /api/v1/reports/{id}` 只在认证后调用 `Store.GetReport`；匿名分享读取必须是独立的公开路由和按 token 摘要联表的单报告查询，绝不能复用列表/搜索 API。
- SQLite 在 `Open` 启用外键、WAL 和 busy timeout；基础 schema 标为 v1，OAuth 迁移在事务中添加 v2/v3。分享表需新增 additive migration，`report_id` 外键级联删除，摘要列唯一，并按报告、到期时间建立索引。
- 现有 `logRequests` 原样记录 `URL.Path`；因此 `/share/<token>` 和公开 JSON API 路径会泄露可用凭据。必须在应用日志中脱敏路径，并在部署文档说明反代访问日志也需脱敏/排除。
- Markdown 由 markdown-it（禁用原始 HTML）及 DOMPurify 清洗；当前渲染器允许图片。公开分享页需屏蔽 Markdown 图片并设置严格 Referrer、缓存与 CSP 响应头，避免访问者 IP/分享令牌 Referer 外泄。
- SPA 静态回退对非文件路径返回首页。分享 URL 可以使用这一回退，但公开 JSON 路由必须先于 `/api/` 鉴权注册、精确验证单 token 路径，不能被 SPA 回退吞掉。

## 实施路径

1. 先重排移动端顶栏为可读的品牌/搜索/主要筛选主题组与可折行低频操作；保证 select 文本省略但可访问，菜单在 viewport 内。
2. 采用 32-byte 加密随机不透明令牌，数据库只存 SHA-256 摘要；创建、查询、撤销均服务端校验，并在每次匿名读取时检查撤销、UTC 到期和报告存在。
3. 用专用公开报告 DTO、匿名只读端点和独立分享页；分享管理 API 仅接受所有者会话；添加限流、固定错误响应、无缓存/无 Referrer/无索引及日志脱敏。
4. 以有限批次后台清理过期/撤销记录；清理不是失效判据。报告删除通过外键同步令分享失效。
5. 运行 Go、Vitest、build、Docker 隔离和真实浏览器仿真；物理 iOS/Android 设备、用户生产反代和部署不在本地验收能力内。

## OAuth 影响

P8 OAuth 的授权服务器、MCP 路由与访问 token 不需要为分享扩权。分享管理明确仅使用所有者 Cookie；OAuth MCP token 与旧 REST Bearer 都不得创建、列出或撤销分享。分享公开读取是独立报告能力，不返回 OAuth 或 MCP 元数据。
