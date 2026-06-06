# 消息刷新后最新输出丢失与运行态不渲染修复

> 日期: 2026-05-30

## 现象

群聊中向 Orchestrator 发送规划类指令后，出现两个相关问题：

- 子 Agent 的 `runtime_text` 进入了前端 store，但聊天栏不稳定显示或完成后消失
- 已落库的裸 `type: runtime_status` / `json: ...` 片段被当成 Markdown 代码块大量渲染，页面卡顿
- 数据库中有对应 Message 记录，但刷新前端后，最新一轮输出不显示

## 根因

### 运行态不渲染

前端 `use-chat-stream.ts` 已经把 `planning`、`runtime_*`、`coordination_*` 事件写入 `SessionChatState.runtimeBlocks`，但 `MessageList` 只渲染 `messages` 和 `streamingContent`，没有渲染 `runtimeBlocks`。

同时，`runtime_completed` 会用一个不带 `streamingText` 的状态块覆盖已有 `runtime_status`，导致前面累积的子 Agent 输出在完成瞬间被清空。

历史数据中已经存在裸的运行态片段或普通 fenced code block 包裹的运行态片段。它们不是 `aka_yhy` 卡片，但如果直接交给 Markdown 渲染，会变成大量代码块，造成聊天页面明显卡顿。

### 刷新后最新消息不显示

前端刷新时调用 `GET /api/tasks/:taskId/messages?limit=20&session_id=...` 加载历史消息。

后端分页查询按 `created_at ASC` 加 `LIMIT 20` 返回，实际取到的是该会话最早的 20 条消息，而不是最新的 20 条消息。群聊消息数量超过 20 后，最新输出仍在数据库中，但刷新首屏拿不到，看起来就像“没有持久化”。

同类问题也影响向上加载历史：`before=<oldest_id>` 时应取距离当前最老消息最近的一页更早消息，而不是从全库最早处开始取。

## 修复

### Frontend

前端修复运行态渲染链路：

- `MessageList` 接收并渲染 `runtimeBlocks`
- streaming 伪消息在只有运行态块、没有普通文本时也会显示
- `runtime_completed` 更新状态时保留已有 `streamingText`
- `streamDone` 完成时清空临时 `runtimeBlocks`，最终消息仍以真实 `text` 内容为准
- 新一轮发送或错误结束时清空旧 `runtimeBlocks`
- `block-reducer` 兼容解析历史中的裸运行态片段，将碎片化 `streamingText` 合并为一个运行态块，避免大量 code block 渲染

### Backend

`backend/internal/handler/message.go`（**注：后端已重构为 controller + service 分层，对应文件现为 `controller/impl/message_controller.go` + `service/impl/message_service.go`**）对分页查询改为：

- 数据库查询按 `id DESC` 取最新/最近的一页
- 截断 `limit + 1` 后在内存中反转
- API 响应仍保持聊天展示需要的时间正序

因此：

- 刷新首屏加载最新 20 条消息
- 继续向上滚动加载更早一页消息
- 不改变 Message schema 和前端 API 结构

## 注意

- `planning`、`runtime_*`、`coordination_*` 是 SSE 运行时事件，由前端直接构造运行态块，不应被后端强行转换为 `aka_yhy` 文本卡片。
- `aka_yhy` 卡片仍然只来自 Agent 文本中明确存在的 fenced block。
- 普通子 Agent 最终输出仍依赖既有 `text` 消息持久化；刷新恢复最新输出依赖消息分页修复。

## 验证

- `go test ./internal/controller/... ./internal/service/... ./internal/stream`
- `pnpm exec vitest run src/lib/__tests__/block-reducer.test.ts`
- `pnpm exec eslint src/lib/block-reducer.ts src/lib/__tests__/block-reducer.test.ts`
