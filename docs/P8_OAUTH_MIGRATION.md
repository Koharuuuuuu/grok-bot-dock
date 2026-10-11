# P8 · OAuth 存储迁移、运行兼容和回滚

> 状态：实现已加入本地分支，尚未发布；不得改变已存在 P7 报告与 Bot 数据。数据库为 SQLite WAL、数据卷 `/data`；网页管理认证基于单管理员 ACCESS_TOKEN + 内存 session。

## 已实现数据模型

基线 schema 为 v1。P8 在事务中幂等创建 OAuth 表，并由 v2 升级到 v3；v3 为 `oauth_clients` 添加 `registration_type`，用于区分手动登记和 CIMD 客户端。迁移不改写旧报告/Bot/FTS 结构；SQLite 外键与 WAL 单连接策略保持不变。

| 建议表 | 最少字段 / 索引 | 约束 |
| --- | --- | --- |
| `oauth_clients` | `client_id` primary key，`name`，`redirect_uris_json`，`client_secret_hash`，`created_at`，`disabled_at` | 秘密只保存 SHA-256 摘要；禁用不删除历史记录 |
| `oauth_grants` | `id`，`owner_subject`，`client_id`，`resource`，`scopes_json`，`created_at`，`expires_at`，`revoked_at` | 固定 `owner` subject；撤销和过期由每次 token 验证强制执行 |
| `oauth_authorization_codes` | `code_hash`，`grant_id`，`client_id`，`redirect_uri`，`pkce_challenge`，`resource`，`scopes_json`，`expires_at`，`consumed_at` | 不透明 code 只存 SHA-256 摘要；兑换事务一次性消费 |
| `oauth_access_tokens` | `token_hash`，`grant_id`，`client_id`，`resource`，`scopes_json`，`expires_at`，`revoked_at` | 不透明 token 只存 SHA-256 摘要；每个 MCP 请求查库 |
| `oauth_audit_events` | `id`，`event_at`，`event_type`，`client_id`，`result`，`subject_ref` | 不存 token、secret、code、Cookie 或报告正文；授权时清理 90 天前审计 |

尚未建 refresh-token 表，因为当前未发放 refresh token；客户端如要求刷新暂标不兼容，需基于实测再设计 rotation/family replay 防护。Access token TTL 为 15 分钟，授权 grant 最长 30 天，授权码 TTL 为 5 分钟；过期 grant/token 在校验时拒绝，后续授权创建时清理过期 code/token/grant 和 90 天前审计。CIMD 已实现：只允许 HTTPS、禁止重定向、校验解析出的全部 IP 为公网地址并固定到已校验 IP、限制响应大小/超时/并发、校验 client_id 与文档内容及 redirect URI；成功结果按受限缓存策略缓存。匿名 DCR 未实现。

建议使用单独的 schema version migration 和幂等索引；所有 timestamp 使用 UTC；设置紧凑的数据保留和清理周期，确保不增加轻量服务器资源压力。授权管理页只暴露可撤销 metadata，client secret 永不可二次显示。若项目选 JWT，需要明确解释撤销设计与为何优于不透明 token；不得两套方案混搭。

## 兼容性与权限不变量

1. 没开启 OAuth 时：运行行为应与当前 `master` 完全等价；无需新增 OAuth 配置即可启动。
2. 开启 OAuth 时：原 `ACCESS_TOKEN` 在 `/mcp` 和 REST 推送保持完全原样；旧浏览器登录/注销与 SSE 不变。
3. OAuth Bearer 只被 `/mcp` 接受；不能用来取得管理员会话、调用 REST 写入或删除 API。
4. 新旧 token 各自独立撤销/轮换；不得互认，不得把单一 `ACCESS_TOKEN` 自动下放给所有 OAuth 客户端。
5. Scope `reports:read` 控制 `search_reports`；`reports:write` 控制 `publish_report`，且保留原报告参数验证、来源隔离及 `external_id` 去重。
6. 没有 OAuth 授权记录时，不因迁移脚本改变任何已存在 `reports`、`bots`、FTS/触发器或会话行为。
7. 公开 GHCR 镜像仍可匿名拉取；仓库始终保持 private；不能把发布和生产启用自动绑定。

## 迁移步骤

- **准备**：记录 `master` 基线 SHA/当前数据库 schema、容器 digest 和部署配置；先 `go test ./...`、`npm test`/build 建立回归基线。密钥不打印。
- **备份**：生产停写/停服后，备份 `/data/bolt-dock.db`（同时检查 WAL/SHM 一致性），备份加密并校验；勿使用 `docker compose down -v`。
- **前向迁移**：实现为 `internal/store/store.go` 调用的事务式 additive DDL；多次启动通过 schema version 识别，不会对 P7 表执行 UPDATE/DELETE。自动化测试覆盖 v1→当前版本及 v2→v3 升级、重复迁移、历史报告和 token 持久性。
- **启用**：OAuth 默认为关闭；`OAUTH_ENABLED=true` 时必须设置 `PUBLIC_BASE_URL` 为固定 HTTPS origin（仅 loopback 测试可 HTTP）；代理公网转发和备份仍需部署者验证。
- **灰度**：先隔离测试客户端与专用报告或本地测试实例做联调；需要真实 Grok OAuth 时仅进行不修改生产报告的握手/工具发现验证，写入工具必须经用户认可。
- **上线**：启用 OAuth 之前配置速率限制、监控和撤销入口；完成正式生产变更审批。

## 回滚

1. 先关闭 OAuth feature flag 或回退上一个 GHCR digest，恢复 P7 共享 Bearer 路径；不删除 `reports/bots` 数据。
2. 如 schema 兼容，**保留新增 OAuth 表**优先于删表，避免破坏正在运行的旧版；避免 SQLite 无意义的“down migration”删除授权审计。
3. 若数据库文件必须恢复：停服务，备份当前文件再使用升级前的已验证 DB 备份（此举会丢失备份后新增的报告，必须经人工明确批准）。
4. 验证旧 REST、MCP、浏览器登录、SQLite FTS、SSE 与历史报告正常；记录回滚原因/时间/版本。
5. 不得默默把恢复数据库视作无数据损失回滚。

## 验收证据

本地证据：`internal/store/oauth_test.go` 验证 v1/v2 前向迁移、闭库文件备份恢复、历史数据保留、schema v3、OAuth token 持久/过期拒绝和客户端停用即时失效；`internal/api/oauth_test.go` 验证本地 SQLite 授权/token 生命周期及 CIMD 元数据获取和授权。命令与实际输出见 [P8 验收记录](./P8_OAUTH_ACCEPTANCE.md)。未执行生产数据库备份/恢复、公开反向代理验证、GHCR 发布或生产回滚；这些保持“未验收”，不得把本地演练外推成部署证明。
