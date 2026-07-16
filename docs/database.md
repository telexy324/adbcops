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
- `000038_deterministic_quality_evaluation`
- `000039_quality_evaluation_review`
- `000040_chunk_strategy_center`
- `000041_embedding_index_manager`

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
- `000038`：创建质量评估及 Rule Result 表，保存确定性评分、Hard Gate、Block Evidence、扣分原因和建议。
- `000039`：增加评分 Review 生命周期、发布不可变约束、重新评分历史指针与人工覆盖审计表。
- `000040`：创建版本化 Chunk Strategy，并将 Chunk 绑定到 Document Version、Strategy、Parent Chunk 和来源 AST Block。
- `000041`：启用 pgvector，创建 Embedding Index 状态表，并为向量增加模型 Revision、维度、内容哈希、状态与 vector 数据。

## Quality Standard 2.0

- `kb_quality_standard`：标准身份、版本、状态、有效期、创建人和审批人；`(name, version)` 唯一。
- `kb_quality_profile`：适用范围、总分、通过/警告阈值与 hard-gate policy。
- `kb_quality_criterion`：评分维度、权重、最大分、评分方法和稳定顺序。
- `kb_quality_rule`：可执行规则、严重度、扣分、hard gate、证据要求和检测配置。
- 服务层要求每个 Profile 的 Criterion 权重合计为 `1.0000`、最大分合计等于 `total_score`，并要求 rule key 在同一 Profile 内唯一。
- `published` 标准不可更新结构；修改时必须创建新的 `(name, version)` 草稿。
- `kb_quality_standard_import` 记录 `uploaded → validation_failed/awaiting_confirmation` 导入结果；`stored_file_path` 仅供服务端读取，不通过 API 返回。

## Deterministic Quality Evaluation

- `kb_quality_evaluation` 绑定不可变的 `document_version_id`、Published Profile 及 Profile 所属 Standard 版本。
- `kb_quality_rule_result` 每条结果保存 finding status、分数、置信度和 JSON Block Evidence；同一次评估内 `(criterion_key, rule_key)` 唯一。
- 明文凭据的 Evidence 只保存脱敏占位符，不写入检测到的原始 Secret。
- `source=deterministic` 不会为 semantic/LLM Rule 生成评分；此类结果标记为 `manual_confirmation_required`。
- `source=hybrid` 保存确定性规则与通过 Evidence 校验的 LLM Rule 结果；`model_config_id` 固定本次模型配置，`result` 记录 Criterion 分数、Map 调用次数、校验 Warning 和降级组件。
- `review_status=published` 的评估不可再覆盖；`kb_quality_evaluation_override` 保存人工修改前后值、理由、操作人和时间。
- 重新评分创建新评估，并用 `supersedes_evaluation_id` 保留与旧评估的历史关系。

## Chunk Strategy Center

- `kb_chunk_strategy` 使用 `(name, version)` 唯一约束保存不可变策略版本和适用文档类型。
- `kb_chunk` 使用 `(document_version_id, strategy_id, chunk_index)` 唯一标识 Chunk Set；新策略版本不会删除或覆盖旧集合。
- Parent Chunk 保存完整章节，Child Chunk 保存可检索语义单元，并通过 `parent_chunk_id` 关联。
- `source_block_ids`、页码范围及 `content_hash` 保留从 Chunk 到 AST Block 和原文件位置的追溯链。
- 表格 Child 重复表头；命令 Child 同时携带前置条件、风险提示、所属步骤及验证/回滚上下文。

## Embedding Index Manager

- `kb_embedding_index` 将 Document Version、Chunk Strategy、Embedding Config、Model Revision 和 Dimension 绑定成一个逻辑索引。
- 状态流转为 `pending → building → ready`，失败进入 `failed`，Chunk 指纹变化进入 `stale`。
- `kb_chunk_embedding.vector_data` 使用 pgvector；JSONB `embedding` 暂时保留，兼容 1.9A 落地前的旧 RAG 读取路径。
- 每个向量同时保存 `content_hash`，只有与当前 Chunk Hash 一致且状态为 Ready 的向量可被读取。
- 不同 Dimension 或 Model Revision 使用不同逻辑索引；HNSW 以逻辑 Index 为 Partial Index 边界，避免混用维度。
- HNSW 可配置 `m` 与 `ef_construction`，关闭时使用 pgvector 精确距离计算。
