# Security

安全基线见 [`features.md`](../features-v1.2.md)。平台默认只读，凭据不得以明文提交。

## 认证基线

- 密码仅以 bcrypt 哈希保存，API、日志和审计不得返回 `password_hash`。
- JWT 仅允许 HS256，密钥至少 32 字符，默认有效期 12 小时。
- 每个受保护请求都会重新读取用户状态；禁用账号的既有 Token 无法继续访问。
- 修改密码后，早于改密时间签发的 Token 自动失效。
- 登录失败统一返回相同提示，具体失败原因仅写入内部 `login_audit`。
- 初始化管理员只在用户名不存在时创建，重启不会覆盖既有密码。
- 当前登出采用无状态 JWT 语义，由客户端删除 Token；平台不在数据库中保存明文 Token。

## RBAC

- 业务 API 默认要求登录；健康检查和登录接口保持公开。
- 用户管理接口仅允许 `admin` 访问，普通 `user` 会收到 `403`。
- 数据库事务会阻止禁用或降级最后一个启用的 `admin`，避免平台失去管理员入口。

## 会话隔离

- 普通用户只能访问自己的 Conversation 和 Conversation Message。
- `GET /api/conversations` 默认只返回当前登录用户的数据，避免 admin 默认页面混合展示其他用户会话。
- `admin` 可通过显式 `userId` 查询或直接按会话 ID 访问来审计用户会话。

## 凭据加密

- LLM API Key 使用 AES-256-GCM 加密后保存，密文包含 nonce、ciphertext 和 key_version。
- API 响应只暴露 `apiKeyConfigured`，不返回明文 API Key 或密文引用。
- `CREDENTIAL_MASTER_KEY` 至少 32 字符，`CREDENTIAL_KEY_VERSION` 用于标记当前加密版本。

## 文件上传

- 文档上传仅允许 `.md` 与 `.txt`，默认最大 50MB。
- 原始文件名不得包含路径分隔符或 `..`；真实落盘文件名由服务端随机生成。
- 上传目录会解析为绝对路径并校验最终文件仍位于 `LOCAL_FILE_DIR` 内。
- API 响应不返回服务器本地 `file_path`。

## 多轮 RCA 安全边界

- 创建 RCA Run 时会解析 Scope 中的显式 `dataSourceId`，只接受当前用户可访问、已启用且只读的数据源。
- Planner 只能看到已启用、只读且符合当前角色风险等级的 Skill；未知、写入、Schema 不合法、重复或越权动作会被过滤。
- 每次 Skill 执行前再次读取 Skill 定义和数据源权限，防止规划后权限撤销产生 TOCTOU 越权。
- 默认并发限制为单用户 2 个、全局 8 个 RCA Orchestrator；轮次、Skill Call、Token、上下文、墙钟时间和数据量另有独立预算。
- 日志、知识文档、拓扑结果和 SQL 均被视为不可信 Evidence，不能作为系统指令。TiDB 只接受单条 `SELECT`/`SHOW`，EXPLAIN 固定 `analyze=false`。
- RCA 结构化日志不记录问题原文、Evidence Content、凭据或完整 SQL；Prometheus 标签不使用 Run ID、User ID 等高基数字段。
