# Stage 1: Build
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Go 模块代理（加速依赖下载）
ENV GOPROXY=https://goproxy.cn,direct

# 缓存依赖下载
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# 复制源码并构建
COPY backend/ .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /server ./cmd/server

# Stage 2: Runtime
FROM alpine:3.21

WORKDIR /app

COPY --from=builder /server .

# 从 docker/configs/backend/ 复制配置文件
COPY docker/configs/backend/config.yaml ./configs/config.yaml
COPY docker/configs/backend/.env .env

EXPOSE 8080

CMD ["./server"]
