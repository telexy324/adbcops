# AI Native AIOps Platform

AI 原生智能运维分析平台。项目需求、架构和研发任务以
[`features.md`](features-v1.2.md) 为唯一主设计文档。

## 当前阶段

当前已完成：

- Task 0.1：Monorepo 目录和本地基础设施骨架；
- Task 0.2：Gin HTTP 服务、配置、结构化日志、Request ID、Recover 和健康检查。
- Task 0.3：Vite React 前端、基础布局、登录页与平台总览。
- Task 0.4：PostgreSQL、GORM、版本化迁移与数据库连接检查。
- Task 1.1：用户、登录审计、bcrypt、JWT、管理员初始化和认证 API。

## 本地准备

```bash
cp .env.example .env
# 使用前请替换 .env 中所有 replace-* 占位值
make compose-config
make compose-up
make migration-up
```

查看可用命令：

```bash
make help
```

## 启动后端

```bash
make backend-run
curl http://127.0.0.1:8080/api/health
```

查看迁移状态：

```bash
make migration-status
```

运行后端检查：

```bash
make backend-check
```

## 启动前端

```bash
pnpm --dir frontend install
make frontend-dev
```

浏览器访问：

- `http://127.0.0.1:5173/login`
- `http://127.0.0.1:5173/dashboard`

运行前端检查：

```bash
make frontend-check
```

> `.env.example` 仅用于展示配置项；生产环境必须使用独立的密钥管理方案。
