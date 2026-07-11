# Database

数据模型基线见 [`features.md`](../features.md)。

当前已落地迁移：

- `000001_enable_pg_trgm`
- `000002_create_users`
- `000003_create_conversations`
- `000004_create_llm_configs`
- `000005_create_kb_documents`
- `000006_create_kb_chunks`

`kb_document.file_path` 保存服务端随机生成后的本地文件路径；API 响应不暴露该字段。
`kb_chunk` 保存解析切片结果，`chunk_index` 在同一文档内连续且唯一。

## 迁移规则

- SQL 迁移位于 `backend/migrations`，使用递增版本号和 `.up.sql`、`.down.sql` 文件。
- 服务启动只校验数据库连接，不自动执行迁移。
- `make migration-up` 显式执行所有向前迁移。
- `make migration-status` 查看当前版本和 dirty 状态。
- `make migration-down` 仅回滚一个版本，并在 `APP_ENV=prod` 或 `production` 时拒绝执行。
- 生产迁移应通过独立部署任务执行，不得依赖应用启动时的 destructive migrate。

当前迁移：

- `000001`：启用 PostgreSQL `pg_trgm` 扩展；
- `000002`：创建 `app_user`、`login_audit` 及登录审计查询索引。
