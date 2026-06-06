# Stage 1: Build
FROM node:22-alpine AS builder

RUN corepack enable && corepack prepare pnpm@latest --activate

WORKDIR /src

# CI 环境变量：跳过 pnpm 交互式确认
ENV CI=true

# 缓存依赖下载
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --ignore-scripts

# 复制源码并构建
COPY frontend/ .
RUN pnpm build

# Stage 2: Runtime
FROM nginx:1.27-alpine

COPY --from=builder /src/dist /usr/share/nginx/html

# 从 docker/frontend/ 复制 nginx 配置
COPY docker/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
