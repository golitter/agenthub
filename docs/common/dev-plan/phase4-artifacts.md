# Phase 4: 产物与打磨 — 卡片组件 + Artifact 预览

> 目标: Agent 回复中的富媒体卡片（代码块、工具进度、产物预览）。
> 预估: 2-3 天
> 前置: Phase 3 完成 (IM 基础体验可用)

## 交付标准

1. Agent 回复中代码块有语法高亮 + 复制按钮
2. 工具调用有进度卡片 (命令 + 实时输出)
3. 产物文件有预览卡片（图片可直接看，代码文件有详情）
4. 群聊模式：选择 orchestrator 后看到多 Agent 协作

## 要写的文件 / 修改

### 1. AgentEnd: Artifact Manager (简化版)

**文件**: `agentend/src/artifacts/manager.py`

```
职责: 注册任务产物，返回 artifact_id

方法:
  - register(task_id, file_path, mime_type) → artifact_id
  - resolve(artifact_id) → file_path
  - list_by_task(task_id) → List[Artifact]

简化:
  - 不做 artifact DAG / versioning / lineage
  - artifact_id = uuid4
  - 存储在内存 dict 中 (进程重启丢失，MVP 可接受)
```

**文件**: `agentend/src/artifacts/models.py`

```
类型:
  Artifact {
    artifact_id: str
    task_id: str
    filename: str
    mime_type: str
    file_path: str
    size: int
    created_at: float
  }
```

**文件**: `agentend/src/api/v1/artifact.py`

```
路由:
  GET /v1/artifacts/{artifact_id}   返回文件内容
  GET /v1/artifacts?task_id=xxx     列表
```

**修改**: `agentend/src/api/v1/router.py` — 注册 artifact 路由

### 2. Go Backend: Artifact 代理

**文件**: `backend/internal/model/artifact.go`

```go
type ArtifactMeta struct {
    ID           uint   `gorm:"primaryKey"`
    ArtifactID   string `gorm:"uniqueIndex;size:36"`
    TaskID       string `gorm:"index;size:36"`
    SessionID    string `gorm:"index;size:36"`
    ArtifactType string `gorm:"size:30"` // file, image
    Filename     string `gorm:"size:200"`
    MimeType     string `gorm:"size:100"`
    Size         int64
    CreatedAt    time.Time
}
```

**文件**: `backend/internal/handler/artifact.go`

```
路由:
  GET  /api/artifacts/:id          代理到 AgentEnd 获取文件
  GET  /api/artifacts?task_id=xxx  列表

处理:
  1. 查 DB 获取 artifact 元数据
  2. 请求 AgentEnd GET /v1/artifacts/{id}
  3. 透传 response body + Content-Type
```

### 3. Frontend: 卡片组件群

#### CodeBlockCard

**文件**: `frontend/src/components/cards/CodeBlockCard.tsx`

```
功能:
  - 语法高亮 (复用 Phase 3 的 rehype-highlight)
  - 文件名标签 (左上角)
  - 复制按钮 (右上角)
  - 行号 (可选)

来源: agent 返回 ```language ... ``` 格式的 markdown
      由 MarkdownRenderer 内部处理，不是独立卡片

实际上 Phase 3 的 MarkdownRenderer 已经能渲染代码块。
这里的增强是:
  - 复制按钮
  - 文件名显示 (从代码块上方的文字提取)
```

#### ToolProgressCard

**文件**: `frontend/src/components/cards/ToolProgressCard.tsx`

```
功能:
  - 显示工具名称 (如 "bash", "read_file", "write_file")
  - 显示命令内容 (如 "npm install")
  - 实时显示 stdout/stderr 输出
  - 完成状态 (✓ 成功 / ✗ 失败)

数据来源: SSE event type = "tool_call" / "tool_result"

结构:
  ┌─ 🔧 bash ──────────────────────────────┐
  │ $ npm install                           │
  │ added 142 packages in 3.2s             │
  │                                         │
  │ ✓ completed                             │
  └─────────────────────────────────────────┘
```

#### ArtifactCard

**文件**: `frontend/src/components/cards/ArtifactCard.tsx`

```
功能:
  - 显示文件名 + 类型图标
  - 图片: 缩略图预览
  - 代码文件: 点击展开代码
  - 其他文件: 下载链接

数据来源: SSE event type = "artifact"

结构:
  ┌─ 📎 Button.tsx (2.1 KB) ──────────────┐
  │ [预览] [下载]                           │
  └────────────────────────────────────────┘
```

### 4. Event 分发渲染

**修改**: `frontend/src/stores/chat.ts`

```
当前: 所有 SSE event 的 content 都拼成文本
升级: 根据 event.type 分发到不同渲染方式

onEvent(event: StreamEvent):
  switch (event.type):
    case 'text':
      → 追加到 streamingContent
    case 'tool_call':
      → 创建 ToolProgressCard 数据
    case 'tool_result':
      → 更新 ToolProgressCard 状态
    case 'artifact':
      → 创建 ArtifactCard 数据
    case 'done':
      → 结束 streaming
```

**修改**: `frontend/src/components/chat/MessageBubble.tsx`

```
变更:
  - assistant 消息不再只是 markdown 文本
  - 而是由多个 "block" 组成:
    - text block → MarkdownRenderer
    - tool block → ToolProgressCard
    - artifact block → ArtifactCard

Message 数据结构升级:
  Message {
    id: string
    role: 'user' | 'assistant'
    blocks: MessageBlock[]  // 替代 content
    timestamp: number
  }

  MessageBlock =
    | { type: 'text', content: string }
    | { type: 'tool', name: string, command: string, output: string, status: string }
    | { type: 'artifact', id: string, filename: string, mimeType: string }
```

### 5. 群聊模式 (Orchestrator)

**修改**: `frontend/src/components/chat/MessageBubble.tsx`

```
当 agent_type = 'orchestrator' 时:
  - 每个 assistant 消息显示来源 agent 名称
  - 用不同颜色区分不同 agent

简化: 不做 Timeline 视图，用普通消息流 + agent 标签。
```

## 文件清单

```
AgentEnd:
├── src/artifacts/
│   ├── __init__.py                     # 新增
│   ├── manager.py                      # 新增 ~60 行
│   └── models.py                       # 新增 ~20 行
├── src/api/v1/
│   ├── artifact.py                     # 新增 ~50 行
│   └── router.py                       # 修改: 注册路由

Go Backend:
├── internal/model/artifact.go          # 新增 ~20 行
├── internal/handler/artifact.go        # 新增 ~60 行
└── cmd/server/main.go                  # 修改: 加路由 + migration

Frontend:
├── src/components/cards/
│   ├── CodeBlockCard.tsx               # 新增 ~60 行
│   ├── ToolProgressCard.tsx            # 新增 ~80 行
│   └── ArtifactCard.tsx                # 新增 ~60 行
├── src/stores/chat.ts                  # 修改: block-based messages
├── src/components/chat/MessageBubble.tsx # 修改: block 渲染
└── src/api/client.ts                   # 修改: 加 artifact API
```

**新增代码量: ~410 行，修改 ~150 行**

## 注意事项

- ToolProgressCard 的实时输出需要 store 支持增量更新 stdout/stderr
- Artifact 预览图片用 `<img src="/api/artifacts/{id}">` 直接指向 Go 代理
- 代码块复制用 `navigator.clipboard.writeText()`
- 群聊模式 Phase 4 只做最小版：agent 标签 + 颜色区分
- Artifact Manager 的内存存储在 MVP 阶段够用，后续可迁移到 Go DB

## 后续迭代 (Phase 4 之后)

如果时间充裕，可以继续做：

```
P3 — 体验优化
├── DiffViewCard (代码差异视图)
├── 网页 iframe 预览 (sandbox)
├── 部署状态卡片
├── 主题切换 (dark/light)
└── 响应式布局 (移动端)

P4 — 架构升级
├── EventEnvelope v1 升级 (三端协议)
├── EventLog 持久化 (Go DB)
├── Replay + 断线重连 (Subscription Cursor)
├── Task State Machine (完整状态机)
└── Runtime Core (EventBus + Backpressure)

P5 — 高级特性
├── 多 Agent Timeline 视图
├── Agent 自定义 (System Prompt + 工具集)
├── JWT 登录/注册 UI
└── Workspace 管理面板
```
