# Database

数据模型基线见 [`features.md`](../features.md)。

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
