# 技术设计

## 1. 架构
Grok Bot（或任何 HTTP 客户端） → HTTPS 反向代理 → Go HTTP 服务 → SQLite。Vue 3 + Vite 构建为静态文件，由 Go 同进程提供；SSE 长连接向在线浏览器广播新报告事件。服务默认监听 `18765`（环境变量可改），公网 HTTPS 由已有反向代理终止。

## 2. 技术栈与资源约束
Go 标准库优先（路由可用标准库）；Vue 3、Vite；SQLite + FTS5；Markdown 渲染库和 HTML 消毒库。不得引入 Redis、ES、队列、单独 Node 运行时。多阶段 Docker 构建，非 root 运行，SQLite 存放 /data；WAL 模式、busy_timeout、合理连接池限制；提供 /healthz。2 vCPU / 1.6 GiB 环境为目标，实际峰值内存、启动时间及容器镜像大小需测量，不能无依据承诺具体数值。

## 3. 数据模型
bots: id(TEXT PRIMARY KEY), name, created_at, updated_at, last_report_at。
reports: id, bot_id(FK), external_id(NULLABLE), executed_at(ISO8601 UTC 存储), received_at, content_md, title(可由首个标题提取), summary(可派生)。索引 (bot_id, executed_at DESC, id DESC)，可选 (bot_id, external_id) UNIQUE 用于幂等。FTS5 虚拟表维护 title/content_md；中文优先验证 trigram tokenizer 的匹配行为与索引体积，提供退化查询策略。SQLite migration 版本化，删除与更新时保持 FTS 同步。

## 4. HTTP API
`POST /api/v1/reports`：JSON，必填 bot_id、bot_name、executed_at、content；可选 external_id。校验 RFC3339 时间、长度、报文上限（建议 1 MiB，可配置），拒绝空白内容及非法字段；首次写入自动 upsert Bot；事务内插入报告；成功返回 201 + report_id、bot_id；重复 external_id 返回 200 + 已有 report_id；400 校验失败、401 鉴权失败、413 超限、429 限流、500 服务错误。避免在日志中打印 token 和报告正文。

只读接口：`GET /api/v1/bots`（卡片统计），`GET /api/v1/bots/{id}/reports?cursor=&date=`，`GET /api/v1/reports/{id}`，`GET /api/v1/search?q=&bot_id=&from=&to=&cursor=`，`GET /api/v1/events`（SSE），`GET /healthz`。历史按时间倒序稳定游标分页，默认 20 条；搜索结果有摘要、来源、时间与跳转目标。明确响应 JSON 与错误格式，分页须支持同时间戳排序。

## 5. 安全与通知
推送写接口默认使用环境变量配置的共享 Bearer Token，采用常量时间比较；无 token 时默认拒绝启动公网写入（开发模式显式配置）。Web 阅读端个人使用也要有基本访问保护，优先通过现有反向代理认证或轻量会话认证，避免公网裸露历史内容。CORS 默认同源；限制请求大小与速率；对 Markdown 中原始 HTML 与链接进行消毒，禁止脚本注入。浏览器通知需要 HTTPS/localhost、用户授权与浏览器/系统允许。SSE 支持重连与心跳；只保证页面在线时实时通知，不宣称浏览器关闭后仍能收到推送。

## 6. 前端交互
桌面优先、移动端自适应；首页卡片网格按最后更新时间排序。每张卡展示 Bot 名称、最新报告摘要、执行时间、未读状态。点击卡片在抽屉/详情视图阅读完整 Markdown（标题、列表、表格、代码、链接、图片）；历史日期筛选、分页；顶部全局搜索，搜索结果高亮关键词并可直达原文。通知权限由明确的用户操作触发，不在页面初次加载时强制弹窗。卡片列表不加载历史全文，避免性能浪费。

## 7. 质量保障
Go 单测覆盖验证、幂等、并发写入、FTS 同步、分页、鉴权；前端测试覆盖 Markdown XSS、卡片更新、检索与通知降级；端到端测试使用 curl POST → SQLite → Web/SSE。Docker 重启后数据不丢失。提供 curl 示例、.env.example、docker-compose.yml、Makefile、部署与备份说明；CI 运行 Go 测试和前端构建。默认 master 分支，提交前检查现有内容并避免破坏性覆盖。
