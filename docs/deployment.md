# 部署说明

## 镜像

根目录 `Dockerfile` 会构建两个 Go 二进制：

- `/app/server`：HTTP API 服务；
- `/app/migrate`：数据库迁移命令。

镜像默认监听 `8080`，上传文件目录为 `/app/data/uploads`。

## Docker Compose

本地或单机环境可直接使用 Compose：

```bash
cp .env.example .env
# 编辑 .env，替换所有 replace-* 密钥和密码
docker compose up -d --build
```

Compose 会启动 PostgreSQL、先运行一次 `migrate up`，再启动 API。API 健康检查使用 `/api/ready`。

## Kubernetes

原生 YAML 位于 `deploy/k8s/`：

```bash
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
cp deploy/k8s/secret.example.yaml /tmp/aiops-secret.yaml
# 编辑 /tmp/aiops-secret.yaml，替换所有密钥
kubectl apply -f /tmp/aiops-secret.yaml
kubectl apply -f deploy/k8s/pvc.yaml
kubectl apply -f deploy/k8s/migration-job.yaml
kubectl apply -f deploy/k8s/deployment.yaml
kubectl apply -f deploy/k8s/service.yaml
```

默认假设 PostgreSQL 已存在，并通过 `DB_HOST` 指向外部或集群内数据库。

## Helm

Helm chart 位于 `deploy/helm/aiops-platform/`：

```bash
helm upgrade --install aiops deploy/helm/aiops-platform \
  --namespace aiops \
  --create-namespace \
  --set image.repository=your-registry/aiops-platform \
  --set image.tag=your-tag \
  --set secrets.DB_PASSWORD='replace-me' \
  --set secrets.JWT_SECRET='replace-with-at-least-32-chars' \
  --set secrets.INITIAL_ADMIN_PASSWORD='replace-me' \
  --set secrets.CREDENTIAL_MASTER_KEY='replace-with-at-least-32-chars'
```

默认会创建一个 migration job，可通过 `migration.enabled=false` 关闭。

## 健康与观测端点

- `GET /api/live`：liveness probe；
- `GET /api/ready`：readiness probe，会检查数据库连接；
- `GET /api/health`：兼容的基础健康检查；
- `GET /api/metrics`：Prometheus 文本格式指标。
