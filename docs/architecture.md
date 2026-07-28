# Architecture

架构基线见 [`features.md`](../features-v1.2.md)。后续 Task 在此补充实现级架构说明。

## Bounded Multi-round RCA

`general_rca_workflow` 是可执行、保持无环的内置 Workflow：先真实校验 Scope，再由 Coordinator 确认 `general_rca` 路由。Workflow 节点支持从 `workflowInput` 或已完成节点 `previous` 输出进行白名单路径映射；Condition 节点执行 `exists`、`not_exists`、`equals`、`not_equals` 或 `truthy` 判断。

多轮循环不放入 Workflow DAG，而由 RCA Orchestrator 负责。Orchestrator 默认最多三轮，统一限制每轮/全局 Skill Call、并发、估算 Token、上下文大小及墙钟时间，并将每次只读 Skill 输出归一化为 Evidence 后重新规划。
