FROM node:22-alpine AS frontend-build

WORKDIR /src/frontend

COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN corepack enable \
	&& corepack prepare pnpm@9.15.4 --activate \
	&& pnpm install --frozen-lockfile

COPY frontend ./
ARG VITE_API_BASE_URL=
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
RUN pnpm build

FROM nginx:1.27-alpine

COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=frontend-build /src/frontend/dist /usr/share/nginx/html

EXPOSE 80

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
	CMD wget -qO- http://127.0.0.1/ >/dev/null 2>&1 || exit 1
