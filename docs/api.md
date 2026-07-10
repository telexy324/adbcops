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
