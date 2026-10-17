# 文档索引

按读者要完成的任务组织。首次使用请先读根目录 [README](../README.md)，不要从历史执行计划开始。

## 直接使用

| 目标 | 文档 |
| --- | --- |
| 安装、配置、升级、反向代理、备份和恢复 | [部署与运维](DEPLOYMENT.md) |
| 接入 Agent、REST、MCP 和 OAuth | [Agent 接入指南](AGENT_INTEGRATION.md) |
| 理解主题空间的当前行为和迁移边界 | [主题重构验收](TOPIC_REFACTOR_ACCEPTANCE.md) |

## 技术与协议

- [技术设计](TECHNICAL_DESIGN.md)：架构、数据模型、HTTP API、安全与测试。
- OAuth： [架构](P8_OAUTH_ARCHITECTURE.md)、[威胁模型](P8_OAUTH_THREAT_MODEL.md)、[客户端指南](P8_OAUTH_CLIENTS.md)、[迁移说明](P8_OAUTH_MIGRATION.md)、[客户端兼容矩阵](P8_MCP_CLIENT_COMPATIBILITY_MATRIX.md)、[验收记录](P8_OAUTH_ACCEPTANCE.md)。

## 验收证据与历史方案

以下文档记录设计、实施计划或阶段验收结果，用于追溯历史；不替代上面的当前使用指南。涉及现状时，以代码、根 README 和当前验收记录为准。

- 主题重构：[规格](TOPIC_REFACTOR_SPEC.md)、[验收](TOPIC_REFACTOR_ACCEPTANCE.md)。
- P9 响应式与分享：[移动端分享验收](P9_MOBILE_SHARE_ACCEPTANCE.md)、[桌面与响应式验收](P9_1_DESKTOP_RESPONSIVE_ACCEPTANCE.md)、[回归记录](P9_1_DESKTOP_RESPONSIVE_REGRESSION.md)、[执行计划](P9_MOBILE_SHARE_EXECUTION_PLAN.md)。
- P9 基线审计：[审计记录](P9_BASELINE_AUDIT.md)。
- P8 基线审计：[审计记录](P8_BASELINE_AUDIT.md)。
- P7 MCP：[验收](P7_MCP_ACCEPTANCE.md)、[执行计划](P7_MCP_EXECUTION_PLAN.md)、[产品定位修订](P7_MCP_PRODUCT_POSITIONING_CORRECTION.md)、[UI/UX 优化记录](P7_MCP_UI_UX_OPTIMIZATION.md)。
- P6 与更早阶段：[P6 验收](P6_ACCEPTANCE_REPORT.md)、[P6 修复计划](P6_CONSOLIDATED_FIX_PLAN.md)、[通用验收](ACCEPTANCE_REPORT.md)、[执行计划](EXECUTION_PLAN.md)、[优化验收](OPTIMIZATION_ACCEPTANCE.md)。
