# API

API 契约基线见 [`features.md`](../features.md)。

## Auth

除登录与健康检查外，认证接口使用：

```text
Authorization: Bearer <access-token>
```

### 登录

```http
POST /api/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "..."
}
```

成功响应包含 `accessToken`、`tokenType`、`expiresAt` 和脱敏用户信息。失败响应不会区分用户不存在、密码错误或账号已禁用。

### 当前用户

```http
GET /api/auth/me
```

### 修改密码

```http
POST /api/auth/change-password
Content-Type: application/json

{
  "currentPassword": "...",
  "newPassword": "..."
}
```

新密码要求 12～72 字节且不得与当前密码相同。成功修改后，签发时间早于改密时间的 Token 会失效。

### 登出

```http
POST /api/auth/logout
```

当前使用无状态 JWT；登出确认后客户端必须立即删除本地 Token。服务端不维护 Token 黑名单。

## User

用户管理接口均要求登录且当前用户角色为 `admin`。普通 `user` 访问会返回 `403`。

```http
GET /api/users
POST /api/users
PUT /api/users/{id}
POST /api/users/{id}/reset-password
POST /api/users/{id}/enable
POST /api/users/{id}/disable
```

### 创建用户

```http
POST /api/users
Content-Type: application/json

{
  "username": "operator",
  "password": "operator-password",
  "displayName": "Operator",
  "role": "user",
  "enabled": true
}
```

`role` 仅允许 `admin` 或 `user`；未传时默认为 `user`。`enabled` 未传时默认为 `true`。响应只返回脱敏用户信息，不返回密码哈希。

### 更新用户

```http
PUT /api/users/{id}
Content-Type: application/json

{
  "displayName": "New Name",
  "role": "admin"
}
```

将最后一个启用的 `admin` 降级为 `user` 会返回 `409`。

### 重置密码

```http
POST /api/users/{id}/reset-password
Content-Type: application/json

{
  "password": "new-secure-password"
}
```

新密码要求 12～72 字节。服务端只保存 bcrypt 哈希，不返回明文密码。

### 启用/禁用

```http
POST /api/users/{id}/enable
POST /api/users/{id}/disable
```

禁用最后一个启用的 `admin` 会返回 `409`，避免平台失去管理员入口。

## Conversation

会话接口均要求登录。普通用户只能访问自己的会话；`admin` 默认也只列出自己的会话，需要审计指定用户时显式传 `userId`。

```http
GET /api/conversations
GET /api/conversations?userId=2
POST /api/conversations
GET /api/conversations/{id}
GET /api/conversations/{id}/summary
DELETE /api/conversations/{id}
POST /api/conversations/{id}/messages
```

### 创建会话

```http
POST /api/conversations
Content-Type: application/json

{
  "title": "支付接口排障",
  "contextSnapshot": {
    "selectedEnvironment": "prod",
    "selectedSystem": "payment"
  }
}
```

### 获取会话

```http
GET /api/conversations/{id}
```

响应包含 `conversation` 和 `recentMessages`。`recentMessages` 固定返回最近 16 条消息，用作最近 8 轮上下文窗口。

### 添加消息

```http
POST /api/conversations/{id}/messages
Content-Type: application/json

{
  "role": "user",
  "content": "支付接口 9 点后超时增多，可能是什么原因？",
  "metadata": {
    "source": "manual"
  }
}
```

`role` 允许 `user`、`assistant`、`system`、`tool`，未传时默认为 `user`。本阶段只保存消息，不触发 LLM 调用。

### Summary 预留

```http
GET /api/conversations/{id}/summary
```

当前返回已保存的 `conversationSummary` 与 `contextSnapshot`；摘要生成会在后续任务接入。

## LLM Config

LLM 配置接口仅允许 `admin` 访问。请求中可以提交 `apiKey`，但任何响应都不会返回明文 API Key 或密文引用，只返回 `apiKeyConfigured`。

```http
GET    /api/llm-configs
POST   /api/llm-configs
GET    /api/llm-configs/{id}
PUT    /api/llm-configs/{id}
DELETE /api/llm-configs/{id}
POST   /api/llm-configs/{id}/default
POST   /api/llm-configs/{id}/test
```

### 创建配置

```http
POST /api/llm-configs
Content-Type: application/json

{
  "name": "OpenAI Compatible",
  "provider": "openai-compatible",
  "baseUrl": "https://api.example.com",
  "model": "ops-model",
  "apiKey": "sk-...",
  "temperature": 0.2,
  "enabled": true,
  "isDefault": true
}
```

`provider` 允许 `deepseek`、`qwen`、`openai-compatible`。`isDefault=true` 会自动取消其它默认模型，数据库唯一索引保证同一时间只有一个默认模型。

### 测试配置

```http
POST /api/llm-configs/{id}/test
Content-Type: application/json

{
  "prompt": "Say ok."
}
```

测试接口使用 OpenAI Chat Completions compatible 协议请求 `{baseUrl}/v1/chat/completions`，并返回模型名、内容和 usage 摘要。

## Knowledge Document

文档接口要求登录。当前上传阶段仅接收 `.md` 与 `.txt`，最大文件大小由 `MAX_UPLOAD_BYTES` 控制，默认 50MB。上传记录会保存 `createdBy`。

```http
POST /api/documents/upload
GET  /api/documents
GET  /api/documents/{id}
GET  /api/documents/{id}/chunks
POST /api/documents/{id}/review
POST /api/documents/{id}/reprocess
```

### 上传文档

```http
POST /api/documents/upload
Content-Type: multipart/form-data

file=<guide.md>
title=支付系统排障手册
systemName=payment
componentName=payment-api
environment=prod
docType=runbook
version=v1.0
tags=["payment","runbook"]
```

服务端只使用原始文件名作为元数据，实际存储文件名由服务端随机生成；非法扩展名、路径穿越文件名和超限文件会被拒绝。响应不返回服务器本地 `file_path`。

### 解析与切片

```http
POST /api/documents/{id}/reprocess
GET  /api/documents/{id}/chunks
```

`reprocess` 会读取已上传原文，按 Markdown 标题、空行段落和固定长度规则生成 `kb_chunk`。默认 `RAG_CHUNK_SIZE=800`、`RAG_CHUNK_OVERLAP=100`，返回的 `chunkIndex` 从 0 开始连续递增，空白 chunk 会被丢弃。
每个 chunk 会生成 `summary`、`keywords`、`possibleQuestions` 和 `searchText`，供检索召回使用。

### 文档质检与审核发布

```http
POST /api/documents/{id}/review
Content-Type: application/json

{
  "result": {
    "score": 85,
    "summary": "结构清晰，排障步骤完整。",
    "findings": ["包含系统范围", "包含处置步骤"],
    "suggestions": ["补充负责人"]
  }
}
```

`result` 必须符合本地 JSON schema：`score` 为 0～100，且 `summary`、`findings`、`suggestions` 不能为空。`score >= 70` 会进入 `reviewing`，`score < 70` 会进入 `rejected`，不可发布。

管理员可以通过同一接口提交审核动作：

```http
POST /api/documents/{id}/review
Content-Type: application/json

{
  "action": "publish",
  "comment": "质量达标，允许进入正式知识库。"
}
```

支持的 `action` 为 `publish`、`reject`、`archive`、`deprecate`。审核动作会写入 `kb_document_review` 记录。`publish` 仅允许质量分 `>= 70` 且当前状态为 `reviewing` 的文档进入 `published`。

### 知识检索

```http
POST /api/knowledge/search
Content-Type: application/json

{
  "query": "数据库连接池",
  "limit": 10
}
```

当前使用 `kb_chunk.search_text` 与 `content` 进行 pg_trgm 召回并返回 chunk 列表；只有 `published` 文档的 chunk 会参与正式检索。

### RAG 问答

```http
POST /api/knowledge/ask
Content-Type: application/json

{
  "conversationId": 1,
  "question": "数据库连接池耗尽时如何排查？",
  "limit": 5
}
```

`conversationId` 可选；不传时会自动创建会话。接口会执行 query rewrite、召回、rerank、回答生成，并返回 `citations`。引用只来自已发布文档的真实 chunk，同时会写入用户消息、助手消息和 `qa_record`。如果没有已发布知识可支撑回答，会明确返回“未找到可依据的已发布知识，无法基于知识库回答该问题。”

## Data Source

数据源接口要求登录。普通用户只能查看脱敏后的启用数据源；创建、更新、删除和测试连接均要求 `admin`。

```http
GET    /api/data-sources
POST   /api/data-sources
GET    /api/data-sources/{id}
PUT    /api/data-sources/{id}
DELETE /api/data-sources/{id}
POST   /api/data-sources/{id}/test
```

创建示例：

```http
POST /api/data-sources
Content-Type: application/json

{
  "name": "prod-es",
  "sourceType": "elasticsearch",
  "environment": "prod",
  "systemName": "payment",
  "componentName": "payment-api",
  "config": {
    "baseUrl": "https://es.example",
    "index": "logs-*"
  },
  "credential": {
    "username": "elastic",
    "password": "..."
  },
  "enabled": true,
  "readOnly": true
}
```

`config` 只能保存非敏感连接配置；包含 `password`、`token`、`secret`、`apiKey`、`privateKey` 等字段会被拒绝。`credential` 会加密保存到 `credential_secret`，响应只返回 `credentialConfigured`，不会返回明文或密文。

## Analysis Logs

日志查询接口要求登录，当前支持 `elasticsearch` / `opensearch` 类型数据源。接口只执行 `_search` 查询，不提供任何管理操作。

```http
POST /api/analysis/logs
Content-Type: application/json

{
  "dataSourceId": 1,
  "index": "logs-*",
  "from": "2026-07-11T00:00:00Z",
  "to": "2026-07-11T01:00:00Z",
  "keyword": "database",
  "level": "ERROR",
  "size": 100,
  "timeoutMs": 10000
}
```

默认不允许查询超过 24 小时的时间范围，超过会返回明确错误；如确需大范围查询，需要显式传入 `allowLargeRange: true`。返回统一 `LogItem` 列表，字段包含 `timestamp`、`level`、`message`、`source`、`systemName`、`component`、`environment`、`host`、`cluster`、`namespace`、`pod`、`container`、`traceId`、`requestId`、`errorCode` 和 `raw`。

### 日志预处理

```http
POST /api/analysis/logs/preprocess
Content-Type: application/json

{
  "items": [
    {
      "timestamp": "2026-07-11T01:00:00Z",
      "level": "error",
      "message": "request 123 failed password=secret",
      "pod": "payment-0"
    }
  ],
  "stackMaxLines": 40
}
```

预处理按顺序执行字段/时间标准化、敏感信息脱敏、去重、模板聚类、错误计数、时间桶统计和堆栈截断。脱敏覆盖手机号、身份证、银行卡号、token、password 等；结果返回 `items`、`clusters`、`timeStats`、`errorCount` 和 `redactionCount`。

### 日志分析 MVP

```http
POST /api/analysis/general
Content-Type: application/json

{
  "conversationId": 12,
  "question": "支付接口 9 点后超时增多，可能是什么原因？",
  "scope": {
    "environment": "prod",
    "systemName": "支付系统",
    "componentName": "payment-api",
    "timeStart": "2026-07-11T09:00:00+08:00",
    "timeEnd": "2026-07-11T10:00:00+08:00"
  },
  "dataSourceIds": [1]
}
```

接口会查询日志、执行预处理、检索已发布知识库并生成分析报告，同时保存 `analysis_task`。结果包含 `facts`、`evidence`、`citations`、`rootCauseCandidates`、`missingEvidence` 和 `confidence`；事实来自日志统计，根因候选会以“推测”标注。若配置了默认启用 LLM，会优先使用 LLM 生成摘要；否则返回确定性的本地报告。

```http
GET /api/analysis/tasks
GET /api/analysis/tasks/{id}
```

普通用户只能查看自己的分析任务，管理员可查看全部。

### SFTP 文件读取

SFTP 工具要求 `ssh` 类型数据源，且只提供只读文件读取，不提供 Shell 执行能力。数据源 `config` 示例：

```json
{
  "host": "sftp.example",
  "port": 22,
  "username": "ops",
  "pathAllowlist": ["/var/log/app"],
  "maxBytes": 1048576
}
```

凭据放入 `credential`，支持 `password` 或 `privateKey` / `passphrase`。

```http
POST /api/analysis/sftp/read
Content-Type: application/json

{
  "dataSourceId": 1,
  "path": "/var/log/app/app.log",
  "maxBytes": 1048576
}
```

路径必须是绝对路径，不允许 `..`，清理和软链接解析后仍必须位于 `pathAllowlist` 内；`/etc`、`/root`、`/proc`、`/sys` 和 `.ssh` 等敏感路径会被拒绝。

### Kubernetes 只读资源 Tool

K8s Tool 要求 `kubernetes` 类型数据源，只提供只读资源读取和连通性测试，不提供 create、update、patch、delete 等写操作。数据源 `config` 示例：

```json
{
  "apiServer": "https://kubernetes.example",
  "allowedNamespaces": ["prod", "ops"],
  "insecureSkipTlsVerify": false,
  "timeoutMs": 10000
}
```

`allowedNamespaces` 必填，不允许为空或使用 `*`。除 `namespaces` 资源外，请求 namespace 必须位于允许列表内，否则返回 403。凭据放入 `credential`，支持完整 `kubeconfig`，或 `bearerToken` / `caData`：

```json
{
  "bearerToken": "...",
  "caData": "-----BEGIN CERTIFICATE-----..."
}
```

```http
POST /api/analysis/k8s/test
Content-Type: application/json

{
  "dataSourceId": 1
}
```

```http
POST /api/analysis/k8s/resources
Content-Type: application/json

{
  "dataSourceId": 1,
  "namespace": "prod",
  "resource": "pods",
  "limit": 50
}
```

当前支持 `pods`、`services`、`events`、`deployments` 和 `namespaces`。返回条目包含 `kind`、`namespace`、`name`、`status` 和原始 Kubernetes 对象 `raw`。

#### Pod 诊断采集

```http
POST /api/analysis/k8s/pod-diagnose
Content-Type: application/json

{
  "dataSourceId": 1,
  "namespace": "prod",
  "podName": "payment-api-0",
  "includeNode": true,
  "includePreviousLogs": true,
  "logTailLines": 200,
  "logMaxBytes": 65536
}
```

接口会采集 Pod 摘要、OwnerReference、相关 Events、current/previous container logs、匹配该 Pod labels 的 Service/Endpoint/Ingress，并可选采集所在 Node 的条件摘要。`logTailLines` 最大 2000，默认 200；`logMaxBytes` 最大 1MiB，默认 64KiB。诊断结果不读取、不返回 Kubernetes Secret 对象，也不返回 Pod 原始 Spec 中的 Secret 引用明细。

响应中的 `rules` 是确定性规则引擎输出，当前覆盖 `CrashLoopBackOff`、`OOMKilled`、`ImagePullBackOff`、`Pending`、Service 无可用 Endpoint、Ingress backend 无可用 Endpoint。每条规则包含 `id`、`severity`、`category`、`description`、`suggestion` 和 `evidenceKeys`，其中 `evidenceKeys` 用于指向触发规则的 Pod / Event / Service / Endpoint / Ingress 摘要字段。

### Prometheus Metrics Tool

Prometheus Tool 要求 `prometheus` 类型数据源，只调用 HTTP 查询 API，不执行任何写操作。数据源 `config` 示例：

```json
{
  "baseUrl": "https://prometheus.example",
  "timeoutMs": 10000
}
```

凭据放入 `credential`，支持 `username` / `password` 或 `bearerToken`。

```http
POST /api/analysis/metrics/test
Content-Type: application/json

{
  "dataSourceId": 1
}
```

```http
POST /api/analysis/metrics/query
Content-Type: application/json

{
  "dataSourceId": 1,
  "query": "rate(http_requests_total[5m])",
  "range": true,
  "start": "2026-07-12T10:00:00+08:00",
  "end": "2026-07-12T11:00:00+08:00",
  "stepSeconds": 60,
  "maxSeries": 20,
  "maxPoints": 500
}
```

`range=false` 时调用 `/api/v1/query`，`range=true` 时调用 `/api/v1/query_range`。响应统一为 `series[].metric` 和 `series[].points[]`，每个点包含 `timestamp`、`value` 和 `rawValue`。`maxSeries` 最大 100，默认 20；`maxPoints` 最大 2000，默认 500，服务端会强制截断超限返回。

## Events

### Alertmanager Webhook

Alertmanager Webhook 会把告警转换为统一 `ops_event`。重复告警按 `fingerprint` 归并，`resolved` 告警会更新同一事件状态并记录 `resolvedAt`。

```http
POST /api/events/alertmanager
Content-Type: application/json

{
  "receiver": "default",
  "status": "firing",
  "alerts": [
    {
      "status": "firing",
      "labels": {
        "alertname": "HighErrorRate",
        "severity": "critical",
        "environment": "prod",
        "system": "payment",
        "service": "payment-api",
        "namespace": "prod",
        "pod": "payment-api-0"
      },
      "annotations": {
        "summary": "payment api error rate is high"
      },
      "startsAt": "2026-07-12T10:00:00Z",
      "fingerprint": "alertmanager-fingerprint"
    }
  ]
}
```

若 Alertmanager 未提供 `fingerprint`，平台会使用 `alertname + environment + system + component + resource_identity` 生成稳定指纹。响应返回写入或归并后的事件摘要：`id`、`fingerprint`、`status`、`severity`、`summary`、`occurrenceCount` 和可选 `resolvedAt`。

## Tool Registry

Tool Registry 提供只读 Tool 元数据和启停管理。所有 v1 Tool 均为只读；平台不暴露通用 invoke API 给前端，业务能力必须通过受控 Skill、Workflow 或专用分析 API 调用。

```http
GET  /api/tools
GET  /api/tools/{name}
POST /api/tools/{name}/test
POST /api/tools/{name}/enable
POST /api/tools/{name}/disable
```

已注册内置 Tool：

- `elasticsearch`
- `ssh_sftp`
- `kubernetes`
- `prometheus`
- `alertmanager`

`GET` 接口要求登录；`test`、`enable`、`disable` 要求管理员。禁用 Tool 后，后续 Skill Framework 会通过 Registry 拒绝依赖该 Tool 的 Skill 执行。
