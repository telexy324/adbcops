# Database

数据模型基线见 [`features.md`](../features-v1.2.md)。

当前已落地迁移：

- `000001_enable_pg_trgm`
- `000002_create_users`
- `000003_create_conversations`
- `000004_create_llm_configs`
- `000005_create_kb_documents`
- `000006_create_kb_chunks`
- `000007_create_kb_document_reviews`
- `000008_create_qa_records`
- `000009_create_data_sources`
- `000010_create_analysis_tasks`
- `000035_knowledge_document_versions`
- `000036_quality_standard_model`
- `000037_quality_standard_import`

`kb_document.file_path` 保存服务端随机生成后的本地文件路径；API 响应不暴露该字段。
`kb_chunk` 保存解析切片结果，`chunk_index` 在同一文档内连续且唯一。
`kb_document_review` 保存管理员审核动作、流转前后状态和审核备注。
`kb_document_version` 保存原文件哈希、Parser 版本、解析状态和独立 parse quality；`revision_no` 使重新解析不会覆盖历史结果。
`kb_document_block` 保存统一 AST，使用 `parent_block_id`、`order_no`、`page_no` 和 `section_path` 保留结构与顺序。
`qa_record` 保存 RAG 问答审计记录、改写 query、引用和召回数量。
`credential_secret` 保存加密后的数据源凭据；`data_source.config` 只保存非敏感配置。
`analysis_task` 保存分析请求、状态、摘要、结构化结果与错误信息。

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
- `000005`：创建知识文档主表，记录文件元数据、质量分、状态和审核人；
- `000006`：创建知识切片表及 pg_trgm 检索索引；
- `000007`：创建知识文档审核记录表。
- `000008`：创建 RAG 问答记录表。
- `000009`：创建统一数据源与加密凭据表。
- `000010`：创建分析任务表。
- `000035`：创建 Knowledge Center 2.0 文档版本与 AST Block 表，并为升级前文档回填可识别的 legacy version。
- `000036`：将旧版上传型标准表保留为 `kb_quality_standard_legacy`，创建版本化的 Standard、Profile、Criterion、Rule 表并写入内置默认标准。
- `000037`：创建评分标准导入审计表，保留原始 Word/Excel 文件哈希、解析器版本、Warning、Validation Error、Preview 和生成的 Draft 关联。

## Quality Standard 2.0

- `kb_quality_standard`：标准身份、版本、状态、有效期、创建人和审批人；`(name, version)` 唯一。
- `kb_quality_profile`：适用范围、总分、通过/警告阈值与 hard-gate policy。
- `kb_quality_criterion`：评分维度、权重、最大分、评分方法和稳定顺序。
- `kb_quality_rule`：可执行规则、严重度、扣分、hard gate、证据要求和检测配置。
- 服务层要求每个 Profile 的 Criterion 权重合计为 `1.0000`、最大分合计等于 `total_score`，并要求 rule key 在同一 Profile 内唯一。
- `published` 标准不可更新结构；修改时必须创建新的 `(name, version)` 草稿。
- `kb_quality_standard_import` 记录 `uploaded → validation_failed/awaiting_confirmation` 导入结果；`stored_file_path` 仅供服务端读取，不通过 API 返回。
