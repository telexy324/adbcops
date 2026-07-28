# Prompts

Prompt 规范基线见 [`features.md`](../features-v1.2.md)。后续 Task 在此维护版本化模板。

# Quality Evaluation Prompt Contract

Knowledge Center 的 Evidence-based Quality Evaluation 不发送完整文档和完整标准。服务端按 Criterion 选择相关 AST Block，并要求模型仅返回 JSON：

```json
{
  "criterionKey": "accuracy",
  "ruleResults": [
    {
      "ruleKey": "content_consistent",
      "score": 8,
      "maxScore": 10,
      "status": "partial",
      "confidence": 0.9,
      "evidence": [
        {"blockId": "block-000012", "quote": "exact text", "reason": "why it supports the finding"}
      ],
      "deductionReason": "reason",
      "suggestion": "actionable correction"
    }
  ]
}
```

系统 Prompt 明确要求无 Evidence 时省略 Rule，不允许引用当前批次之外的 Block。服务端仍会独立验证 Rule Key、Block ID、Quote、Score、Status 和 Confidence；Prompt 约束不被视为安全边界。包含凭据的 Block 在进入 Prompt 前整体替换为脱敏占位符。

## RCA Planner Prompt Contract

`rca-planner-prompt-v1` 接收经过压缩和脱敏的 Scope、历史轮次、结构化 Evidence、已有假设、剩余预算、注册且已授权的只读 Skill Catalog，以及确定性规则生成的基线方案。

模型只能返回 `rca-planner-schema-v1` JSON。服务端会再次检查 Skill 注册状态、启用状态、只读风险等级、用户角色、数据源可见性与只读属性、数据源类型、Skill Input Schema、Evidence ID、置信度变化依据和动作去重。模型无配置、超时、失败或输出无效时，使用 `rca-planner-v1` 确定性规则结果。
