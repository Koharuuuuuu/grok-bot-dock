# P7 补充纠偏：MCP 是通用服务，不依赖 Codex

> 2026-10-10 用户确认的产品要求。**此文优先于 P7 计划中任何 Codex 专属入口/交互的表述**。本文件是交给 Codex 的执行任务，Codex 仅为开发执行者，而不是产品的必要运行组件。

## 目标
Bot Dock 是独立自托管 MCP Server。任何**支持自定义远程 MCP 且允许工具调用**的 AI Agent（如在具备此能力的情况下的 Grok Bot、Claude、Codex 或第三方客户端）都能通过 HTTPS + MCP 协议调用 `publish_report` 推送报告，或调用 `search_reports` 查询历史。不得暗示 Grok 所有产品形态都已支持自定义 MCP；须按具体产品实测。REST API 始终保留。

## 必须修正的用户体验
- [x] 将默认入口、弹窗和 Toast 改为通用「MCP 接入」；服务端独立于 Codex。
- [x] 默认显示页面推导的服务 URL、Streamable HTTP、Bearer 认证、后端真实工具能力，支持复制 URL 与通用接入说明；反代地址覆盖保存在当前浏览器。
- [x] Codex CLI 仅在可选折叠示例中出现；其他客户端明确要求支持自定义远程 MCP、Streamable HTTP 与 Bearer 凭据，未宣称未经验证的兼容性。
- [x] URL 和通用说明复制均使用对应中立反馈；`BOLT_DOCK_TOKEN` 只出现在可选 Codex CLI 示例。
- [x] 明确说明服务器复用共享 `ACCESS_TOKEN`，仅适用于受信任的单用户接入；没有独立 Agent Token、多租户、按客户端权限或撤销。
- [x] 页面不要求连接 Codex；REST 推送入口保留。

## 接入与安全边界
- [x] 隔离 Docker 实例不依赖 Codex 运行；MCP Inspector CLI 通过 Streamable HTTP 完成 `tools/list`、发布、重试和搜索。已有 Go 集成测试覆盖 Bearer、Origin 和协议边界。
- [x] 非 Codex 标准 MCP 客户端验证版本为 MCP Inspector CLI `2.10.1`；隔离容器的报告在工作台可见，重复 `external_id` 返回同一报告。具体 Claude/Grok 产品形态与版本仍未验证。
- [x] 共享管理员密钥限制、私有单用户定位、无独立 Token 能力已显著展示；独立 Agent Token 保留为后续安全阶段。
- [x] 未修改数据库或既有身份模型；REST 路由和 P6 功能保留。

## 验收与交付
- [x] UI 默认没有 Codex 专属连接引导；实际 URL/通用说明与复制反馈匹配；通用信息包含工具示例与安全边界。
- [x] 非 Codex MCP Inspector 完成工具发现、发布与搜索；相同 `external_id` 重试幂等；工作台读取发布报告。
- [x] README、技术设计、执行计划同步记录独立 MCP、客户端能力前提和共享密钥限制。
- [x] 验收报告记录前后截图、浏览器/协议/客户端证据及未验证的平台与生产部署边界。

> 未验证具体 Claude、Grok 或其他第三方平台产品/版本；不把 MCP Inspector 的协议验证扩大成对这些产品的兼容承诺。
