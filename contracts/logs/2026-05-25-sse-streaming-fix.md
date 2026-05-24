# SSE 实时流式修复

## 变更原因

Agent 回复不能实时流式显示，需刷新页面才能看到完整内容。根因有两个：
1. 前端 SSE 客户端使用 `fetch + ReadableStream`，被 Vite dev proxy 缓冲
2. 后端 `RunTask` goroutine 使用 `c.Request.Context()`，请求返回后 context 被取消，导致 StreamWriter 跳过所有 SSE 事件处理，内容未写入 MySQL/Redis

## 变更文件

无 schema 文件修改。变更仅涉及 handler 内部实现：
- `backend/internal/handler/task.go` — goroutine context 修复
- `backend/internal/handler/stream.go` — serveStreaming 竞态修复

## 对比结果

无契约变更。所有 API 接口签名、请求/响应格式保持不变。

## 跨端影响

- **Frontend**：SSE 客户端从 `fetch + ReadableStream` 重写为原生 `EventSource`，开发环境直连后端绕过 proxy
- **Backend**：`RunTask` 返回 HTTP 202 JSON（未变更），`ServeStream` 行为未变更
- **AgentEnd**：无影响
