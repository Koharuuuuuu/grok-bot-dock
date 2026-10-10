# P8 · OAuth 存储迁移、运行兼容和回滚

> 状态：设计待实施；不得改变已存在 P7 报告与 Bot 数据。数据库为 SQLite WAL、数据卷 `/data`；现有网页管理认证基于单管理员 ACCESS_TOKEN + 内存 session。

## 数据模型建议（不是已存在表）

Codex Phase 0 必须读取 `internal/store` 当前 schema/version/迁移机制，再按真实结构编写迁移，不准复制下面字段并假设数据库已存在 user_id。

| 建议表 | 最少字段 / 索引 | 约束 |
| --- | --- | --- |
| `oauth_clients` | `client_id` unique，`name`，`redirect_uris_json`，`registration_type`，`status`，`created_at` | metadata document URI/更新时间/可信状态可扩展；client_secret 只保存摘要 |
| `oauth_grants` | `id`，`owner_subject`，`client_id`，`resource`，`scopes_json`，`revoked_at` | 本期 owner subject 固定由既有单管理员会话认证推导，未来多用户另迁移 |
| `oauth_authorization_codes` | `code_hash` unique，`grant_id`，`client_id`，`redirect_uri`，`pkce_challenge`，`resource`，`expires_at`，`consumed_at` | 事务保证同一 code 只消费一次 |
| `oauth_access_tokens` | `token_hash` unique，`grant_id`，`client_id`，`resource`，`scopes_json`，`expires_at`，`revoked_at` | 随机不透明 token；按到期/授权/客户端索引 |
| `oauth_refresh_tokens`（按需） | `token_hash` unique，`family_id`，`grant_id`，`rotated_at`，`revoked_at`，`expires_at` | 重放即撤销 token family，轮换事务化 |
| `oauth_audit_events` | `id`，`timestamp`，`event_type`，`client_id`，`result`，`subject_ref` | 禁止存 token、code、报告正文、Cookie、完整敏感请求 |

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
- **前向迁移**：采用向后兼容的 **additive only** DDL，事务中执行，写 schema version；多次启动可识别已执行迁移；旧报告及索引验证 count/checksum/抽样检索一致。
- **启用**：OAuth 默认为关闭；确保 `PUBLIC_BASE_URL`（实际变量名由 Phase 0 决定）符合可信 HTTPS 域名；反向代理仅信任配置好的主机/转发代理；冒烟测试发现与授权流程。
- **灰度**：先隔离测试客户端与专用报告或本地测试实例做联调；需要真实 Grok OAuth 时仅进行不修改生产报告的握手/工具发现验证，写入工具必须经用户认可。
- **上线**：启用 OAuth 之前配置速率限制、监控和撤销入口；完成正式生产变更审批。

## 回滚

1. 先关闭 OAuth feature flag 或回退上一个 GHCR digest，恢复 P7 共享 Bearer 路径；不删除 `reports/bots` 数据。
2. 如 schema 兼容，**保留新增 OAuth 表**优先于删表，避免破坏正在运行的旧版；避免 SQLite 无意义的“down migration”删除授权审计。
3. 若数据库文件必须恢复：停服务，备份当前文件再使用升级前的已验证 DB 备份（此举会丢失备份后新增的报告，必须经人工明确批准）。
4. 验证旧 REST、MCP、浏览器登录、SQLite FTS、SSE 与历史报告正常；记录回滚原因/时间/版本。
5. 不得默默把恢复数据库视作无数据损失回滚。

## 验收证据

产出：实际 SQL/Go migration 文件、schema 版本前后记录、测试数据库迁移和重复运行日志、旧行为回归结果、测试隔离证明、备份恢复演练、无明文凭据检查及最终 Go/Vue 构建与 GHCR 状态。未经验证的生产事项明确标注“未验收”。
