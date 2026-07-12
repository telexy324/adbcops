.DEFAULT_GOAL := help

COMPOSE ?= docker compose
ENV_FILE ?= .env

ifneq (,$(wildcard $(ENV_FILE)))
include $(ENV_FILE)
endif

export APP_ENV APP_PORT APP_TIMEZONE DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME DB_SSLMODE JWT_SECRET JWT_EXPIRE_HOURS INITIAL_ADMIN_USERNAME INITIAL_ADMIN_PASSWORD

.PHONY: help env-check compose-config compose-up compose-down compose-logs backend-run backend-test backend-vet backend-build backend-check migration-up migration-down migration-status e2e frontend-dev frontend-test frontend-build frontend-format frontend-check

help: ## 显示可用命令
	@awk 'BEGIN {FS = ":.*## "; printf "AI Native AIOps Platform\n\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

env-check: ## 检查本地环境文件是否存在
	@test -f "$(ENV_FILE)" || (printf '%s\n' "缺少 $(ENV_FILE)，请先执行: cp .env.example .env" >&2; exit 1)

compose-config: env-check ## 校验 Docker Compose 配置
	$(COMPOSE) --env-file "$(ENV_FILE)" config --quiet

compose-up: env-check ## 启动本地基础设施
	$(COMPOSE) --env-file "$(ENV_FILE)" up -d

compose-down: env-check ## 停止本地基础设施
	$(COMPOSE) --env-file "$(ENV_FILE)" down

compose-logs: env-check ## 查看本地基础设施日志
	$(COMPOSE) --env-file "$(ENV_FILE)" logs -f

backend-run: env-check ## 启动后端 HTTP 服务
	cd backend && go run ./cmd/server

backend-test: ## 运行后端单元测试
	cd backend && go test ./...

backend-vet: ## 运行后端静态检查
	cd backend && go vet ./...

backend-build: ## 构建后端服务到 backend/bin
	cd backend && mkdir -p bin && go build -o bin/server ./cmd/server

backend-check: backend-test backend-vet backend-build ## 执行后端测试、静态检查和构建

migration-up: env-check ## 执行所有待处理数据库迁移
	cd backend && go run ./cmd/migrate up

migration-down: env-check ## 回滚一个迁移（生产环境拒绝）
	cd backend && go run ./cmd/migrate down

migration-status: env-check ## 查看当前数据库迁移版本
	cd backend && go run ./cmd/migrate status

e2e: ## 运行最终端到端验收脚本
	node scripts/e2e.mjs

frontend-dev: ## 启动前端开发服务
	cd frontend && pnpm dev

frontend-test: ## 运行前端组件测试
	cd frontend && pnpm test

frontend-build: ## 执行 TypeScript 检查和前端生产构建
	cd frontend && pnpm build

frontend-format: ## 格式化前端代码
	cd frontend && pnpm format

frontend-check: frontend-test frontend-build ## 执行前端测试和生产构建
	cd frontend && pnpm format:check
