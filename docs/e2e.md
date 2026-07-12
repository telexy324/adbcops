# 最终 E2E 验收

`scripts/e2e.mjs` 覆盖 Task 5.6 的最终验收路径：

1. admin 初始化；
2. 用户创建；
3. LLM 配置；
4. 文档上传与发布；
5. RAG；
6. 日志数据源；
7. K8s 数据源；
8. Prometheus 数据源；
9. Agent 分析；
10. Workflow；
11. Alert Webhook；
12. Incident；
13. Topology；
14. 审计；
15. 权限隔离。

## 运行方式

先启动 API 并完成数据库迁移：

```bash
cp .env.example .env
# 编辑 .env，替换所有 replace-* 值
docker compose up -d --build
```

然后运行：

```bash
E2E_BASE_URL=http://127.0.0.1:8080 \
E2E_ADMIN_USERNAME=admin \
E2E_ADMIN_PASSWORD=initial-admin-password \
node scripts/e2e.mjs
```

也可以使用 Makefile：

```bash
make e2e
```

## 外部依赖模式

默认模式不强制连接真实 Elasticsearch、Kubernetes 或 Prometheus，只验证平台侧的数据源配置、凭据加密、权限、审计与编排路径。

如果已经准备好真实外部依赖，可开启严格模式：

```bash
E2E_STRICT_EXTERNAL=1 node scripts/e2e.mjs
```

严格模式会调用每个数据源的 `/test` 接口，并要求连接配置可读且可解密。
