# Architecture

架构基线见 [`features.md`](../features-v1.2.md)。后续 Task 在此补充实现级架构说明。

## Bounded Multi-round RCA

`general_rca_workflow` 是可执行、保持无环的内置 Workflow：先真实校验 Scope，再由 Coordinator 确认 `general_rca` 路由。Workflow 节点支持从 `workflowInput` 或已完成节点 `previous` 输出进行白名单路径映射；Condition 节点执行 `exists`、`not_exists`、`equals`、`not_equals` 或 `truthy` 判断。

多轮循环不放入 Workflow DAG，而由 RCA Orchestrator 负责。Orchestrator 默认最多三轮，统一限制每轮/全局 Skill Call、并发、估算 Token、上下文大小及墙钟时间，并将每次只读 Skill 输出归一化为 Evidence 后重新规划。

### 第二轮拓扑引导组件调查

第一轮结束后，Orchestrator 从日志 Evidence 和 Scope 提取依赖名，先通过 Topology Alias 解析真实节点，再以根服务为起点执行受 `depth`、`maxNodes`、`maxEdges` 限制的上下游遍历。候选节点按边类型、方向、置信度、跳数、时间新鲜度及 Alias 命中排序；同类候选分数接近时标记冲突，不静默选择。

只有证据相关且绑定到当前用户可访问只读数据源的组件会进入调查。目前覆盖 TiDB、Redis、Nacos、Nginx、Kubernetes 和 Linux。拓扑或绑定缺失时记录 `missingEvidence`，并仅在 Scope 明确给出对应数据源时降级。组件 Skill Evidence 的 `sourceRef.inputEvidenceIds` 保留首轮及拓扑 Evidence ID，形成可追溯证据链；重复节点和重复动作在执行前去重。

### 第三轮 Slow SQL 深度诊断

第二轮假设必须以 TiDB Evidence 确认 slow SQL，第三轮才会进入数据库深度诊断。`DatabaseDiagnosisProvider` 负责将已解析的数据源、服务、时间窗和支持 Evidence 转换为受限只读动作；当前只注册 TiDB Provider，不会把 MySQL 或 PostgreSQL 伪装为已支持。

TiDB Provider 最多调度 `query_tidb_slow_queries`、`query_tidb_processlist`、`query_tidb_lock_waits`、`query_tidb_hot_regions`、`query_tidb_statistics_health` 和 `explain_tidb_sql`。EXPLAIN 仅在共享的单条 `SELECT/SHOW` 校验通过时加入，固定 `analyze=false`；无安全 SQL 时记录缺失执行计划证据，不断言索引失效。

慢查询按 Digest 的累计 `total_query_time` 和执行次数排序，而不是只按单次最大耗时。SQL Evidence 保留脱敏结构和稳定指纹，Literal、注释、Token、账号及个人信息不会进入 Evidence。诊断计划显式保留服务、时间窗、Trace、调用量和基线关联维度，缺失的关联 Evidence 会进入 `missingEvidence`。Assessment 分别标记慢 SQL 影响、连接压力、资源压力、锁竞争、热点 Region、统计异常和执行计划回退；只有慢 SQL Evidence 与至少一种补充 Evidence 同时存在时，才将 slow SQL 标记为中等置信度的 contributing factor，否则保持低置信度和 `partial_success`。

### 确定性 RCA 报告聚合

RCA Report Aggregator 直接读取已经过权限过滤的 Run、Round、Action、Evidence 和根因候选，不要求 LLM 可用，也不复制 Evidence 原始 Content。报告按 Evidence Kind 分组，以 Evidence 来源数量、引用数量和候选置信度计算可解释的证据强度并排序；候选与已驳回假设始终明确区分，不会把推测渲染成确认事实。

每轮报告保存“查询了什么、发现了什么、继续原因和停止原因”，并将 `partial_success`、失败 Skill、数据源缺失及“暂无法定位”作为一等结果。Evidence 使用 `/api/evidence/{id}` 引用，Traceability 同时连接 RCA Run、Workflow Run、Agent Run、Skill Run、Round 和 Action。

Incident 和 Markdown RCA 文档均以只读草稿 Payload 生成。生成草稿不会创建 Incident、写入知识中心、发布文档或执行建议；保存和发布仍必须经过现有显式 API 与权限流程。

### 多轮 RCA 前端状态模型

智能分析工作台以服务端 RCA Run 为唯一事实源。创建后并行启动 Orchestrator 请求与详情轮询，页面按 Round 和 Action 展示独立状态；`running`、`partial_success`、`timed_out`、`permission_denied`、`missing_evidence` 和普通失败不会合并成模糊的成功/失败提示。URL `runId` 与本地活动 Run ID 只负责恢复定位，刷新后重新读取 Run、Round、Action、Evidence 和报告，不在浏览器重放 Skill。

拓扑视图只消费编排结果或 `find_dependencies` Evidence 重建节点和方向边；数据库视图只消费 TiDB 诊断中的 SQL 指纹与脱敏结构。Evidence 按 FACT、RULE、KNOWLEDGE、HYPOTHESIS 分组，根因候选同时展示支持、反证和缺失证据。取消、恢复和重试均调用专用 RCA API，前端不持有 Tool 调用能力，也不提供自动修复、任意 SQL 或其他生产写入口。

### RCA Security 与 Observability

RCA 采用两阶段授权：Run 创建时校验 Scope 中显式数据源；Skill 执行前再次读取当前 Registry 和数据源权限，避免规划与执行之间的权限变化。未知、禁用、非只读、风险等级不匹配、Schema 非法或数据源越权的动作均默认拒绝。Orchestrator 使用单用户和全局两层并发限制，Round、Skill Call、Token、Context、Wall Time 和数据量预算继续在单次 Run 内限制资源。

Run、Planner、Round、Action 和 Evidence 只记录结构化 ID、状态、计数、安全错误码和耗时。Prometheus 使用状态、Round、Skill、Evidence 类型等有界标签，不包含 Run ID、User ID、Query 或 Evidence 原文。版本化 E2E Fixture 固定 15 个场景，确保第二、三轮由前序 Evidence 触发，并覆盖降级、取消、恢复、无证据、权限撤销及 Prompt Injection。
