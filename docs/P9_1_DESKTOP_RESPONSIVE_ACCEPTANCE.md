# P9.1 · PC 顶栏响应式回归验收

日期：2026-10-12
基线：`bac9adc`（最新 `master`，执行前同步）
P9 响应式实现基线：`96a563b`
实现分支：`fix/p9-1-desktop-header-layout`

## 结果

已修复 P9 移动端分组后桌面顶栏右侧控件纵向堆叠、统计项被压窄的问题。修复只涉及顶栏 CSS；未改动卡片栅格、报告/分享业务、MCP/OAuth、后端接口或数据库。P9.1 的 Chromium 浏览器仿真和同源 Go 内嵌构建验收通过，已合并并推送到 `master`。GitHub Actions run [38217666501](https://github.com/Nyakooo/grok-bot-dock/actions/runs/38217666501) 在 master `1dac711` 上验证并重建 GHCR 镜像成功；`ghcr.io/nyakooo/grok-bot-dock:master` 索引 digest 为 `sha256:bc227074fd33086acbbefd53b218acbf22de29393deea04fb62989bce51ace48`，匿名镜像清单确认包含 amd64 与 arm64。真实 Chrome 原生缩放倍率、iOS Safari/Android 实机仍未验证；未部署生产。

## 根因与修改

P9 将来源/主题和 REST/MCP/统计分别包进 `.top-primary-controls` 与 `.top-secondary-controls`，但此前只在 `max-width:540px` 为这两个容器定义 `display:flex`。桌面宽度下它们退回块级布局；父级 `.top-actions` 又允许收缩，导致其子内容折行并将顶栏拉高。

修改在桌面断点显式使用两个水平 flex 分组，右侧组保留内容宽度，搜索框承担剩余宽度并允许收缩。来源选择框限制在 132–250px，长名称省略显示并保留完整 `title`。根据测得的控件最小宽度，801–900px 将搜索框独立折到下一行，避免挤压右侧工具组；手机既有 540px 及以下布局规则保留。

浏览器布局数据（CSS px）：

| 场景 | 修复前 | 修复后 |
| --- | --- | --- |
| 1024px 顶栏 | `top-actions` 194×252，`y=-85.5`；次级区宽 38px、高 252px | 顶栏高 82px；操作组高 42px；来源/主题与次级控件水平排列 |
| 1024px 选中长来源 | 未限制，组可收缩 | 操作组 529.25px；来源框 220px，长名称省略且 `title` 完整；整页宽 1024px |
| 1839px 顶栏 | 来源、统计与按钮竖向堆叠 | 操作组 776.98×42px；主/次分组同一水平带；整页宽 1839px |

修复前截图由同一真实浏览器页面临时恢复缺失的桌面默认规则生成，供问题复现对照；修复后截图来自同源 Go 服务的嵌入式生产前端构建，数据均为隔离环境合成样例。

## 自动化与交互验证

`scripts/p9-1-topbar-geometry.sh` 使用 Playwright CLI 对实时页面读取 `getBoundingClientRect()`、计算样式、控件命中目标与整页宽度，并对横向分组、最小统计宽度、来源选择框边界及页面溢出执行断言。另验证 768px 下长来源的完整 `title`，并以隔离 DOM 值替换模拟统计变化。通过命令：

```bash
PLAYWRIGHT_CLI=/path/to/playwright_cli.sh scripts/p9-1-topbar-geometry.sh
```

浏览器应先打开已登录的本地 Bolt Dock 页面。最终嵌入式 Go 服务运行结果：24 个视口全部通过：

- PC：1024、1280、1366、1440、1600、1839、1920px。
- 平板/过渡：640、720、768、800、801、820、853、900、901、960、1023px。
- 手机：320、360、375、390、414、430px。

另外用 1280px 的等效 CSS 视口宽度 1024/853/640 模拟 125%/150%/200% 缩放后的布局宽度，结果均通过。Playwright CLI 无法设置 Chromium 浏览器原生缩放；这些视口仿真不等同于原生缩放验收。已尝试页面键盘缩放快捷键，浏览器 CSS 视口和 DPR 均未变化，因此真实 125%/150%/200% 缩放保留为未验收。

同源 Go 服务上的交互检查：

- 登录、刷新后恢复工作台、退出后返回登录页。
- 在 1024/1839px 选择长来源；来源框 220px、ellipsis 生效、完整名称可从 `title` 读取，页面无横向溢出。
- 在 768px 平板视口选择超长来源；完整 `title` 与省略样式均通过断言，页面无横向溢出。
- 在 1280px Chromium 页面中将三项统计值替换为 `128`、`12,345`、`1.2 GB` 做布局仿真；三项保持可见、在视口内且页面无横向溢出。该操作仅改变隔离浏览器 DOM，不写入应用数据。
- 切换来源后搜索并显示 1 条结果；清除搜索后卡片恢复。
- 主题菜单键盘可达；来源、主题、搜索的 Tab 顺序正确；主题菜单完整落在视口内；明暗主题切换有效。
- 实时同步状态、统计标签和按钮在桌面均可见；平板/手机来源选择及主题控件可命中，手机热区 44×44px。
- 卡片网格截图、报告详情、分享弹窗抽查正常；分享弹窗完整位于视口内（1280px 下 520×410px）。顶栏变更未涉及报告/分享组件。

## 截图

- 修复前问题复现：[baseline-pc-1024-dark.png](../output/playwright/p9-1/baseline-pc-1024-dark.png)
- PC 1024：[浅色](../output/playwright/p9-1/final-pc-1024-light.png)、[深色](../output/playwright/p9-1/final-pc-1024-dark.png)
- PC 1280：[浅色](../output/playwright/p9-1/final-pc-1280-light.png)、[深色](../output/playwright/p9-1/final-pc-1280-dark.png)
- PC 1839：[浅色](../output/playwright/p9-1/final-pc-1839-light.png)、[深色](../output/playwright/p9-1/final-pc-1839-dark.png)
- 平板 820：[浅色](../output/playwright/p9-1/final-tablet-820-light.png)、[深色](../output/playwright/p9-1/final-tablet-820-dark.png)
- 手机 320：[浅色](../output/playwright/p9-1/final-mobile-320-light.png)、[深色](../output/playwright/p9-1/final-mobile-320-dark.png)

## 构建与回归

| 命令 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| `cd frontend && npm test` | 通过：2 个测试文件、17 项 |
| `cd frontend && npm run build` | 通过：Vite 生产构建完成，更新 Go embed 静态文件 |
| `go build -o /tmp/bolt-dock-p9-1 ./cmd/server` | 通过 |
| Playwright 几何断言脚本 | 24/24 视口通过；平板长来源与统计值变化仿真通过 |
| `git diff --check` | 通过 |

## 限制与交付边界

- 原生浏览器缩放 125%/150%/200%、真实 iOS Safari/Android Chrome、触摸旋转和软键盘尚未验收。
- 桌面页面在 1024px 及以下继续按既有样式将 REST/MCP 长标签收纳为图标；这次只修复控件布局和压缩，不调整其产品文案或收纳策略。
- 所有浏览器数据在 `/tmp/bolt-dock-p9-1-review` 中，仅为合成报告；没有使用生产数据或访问生产服务。
- P9.1 代码已合并并推送到 `master`（合并提交 `f8b9de55604877704314700b2d5aecb3c7d9cca7`）。最新镜像由 master `1dac711` 构建，Actions run `38217666501` 验证与镜像发布通过；GHCR 索引 digest 和多架构信息见本报告开头。最终验收记录提交只改文档，不改镜像运行时代码。未部署生产。
- 初始修复代码提交 SHA：`584e762f4db40c15816840d07b5056e3caa8f44e`；最终 master 还包含验收记录与补充覆盖提交。
