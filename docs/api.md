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
