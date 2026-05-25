# Phase 6: 产物预览 + 部署发布

> **DRAFT — 待 Phase 5 完成后细化**
> 目标: Agent 产物的内联预览、代码编辑、一键部署
> 前置: Phase 5 完成 (Orchestrator 群聊可用)
> 对应任务要求: #4 产物预览与编辑、#5 部署发布

## 功能范围

### AgentEnd: Artifact Manager

```
职责: 注册任务产物，返回 artifact_id

方法:
  - register(task_id, file_path, mime_type) → artifact_id
  - resolve(artifact_id) → file_path
  - list_by_task(task_id) → List[Artifact]

存储: 内存 dict (MVP 阶段)
```

### AgentEnd: Artifact API

```
GET /v1/artifacts/{artifact_id}   返回文件内容
GET /v1/artifacts?task_id=xxx     列表
```

### Go Backend: Artifact 代理

```
GET  /api/artifacts/:id          代理到 AgentEnd 获取文件
GET  /api/artifacts?task_id=xxx  列表

处理:
  1. 查 DB 获取 artifact 元数据
  2. 请求 AgentEnd GET /v1/artifacts/{id}
  3. 透传 response body + Content-Type
```

### Frontend: ArtifactCard

```
┌─ 📎 Button.tsx (2.1 KB) ──────────────┐
│ [预览] [下载]                           │
└─────────────────────────────────────────┘

功能:
  - 图片: 缩略图预览 (<img src="/api/artifacts/{id}">)
  - 代码文件: 点击展开代码
  - 其他文件: 下载链接
```

### Frontend: iframe 网页预览

```
- Agent 产出 HTML 网页时，内联 iframe sandbox 预览
- 点击卡片展开全屏预览
```

### 部署指令 + 状态卡片

```
- 聊天中发送 "部署" 指令
- Agent 返回部署状态卡片
- 显示预览 URL / 构建日志
```

## 不做

| 不做 | 理由 |
|------|------|
| Diff 视图 / 版本历史 | 任务要求标记 P2 |
| 容器化部署 | P2 |
| 源码打包下载 | P2 |
| 桌面端 / 移动端 | 只做 Web 端 |
| Artifact DAG / versioning / lineage | MVP 不需要 |

## 预估

**TBD** — 待 Phase 5 完成后根据实际情况评估。

粗略估计 2-3 天（含 Artifact Manager + 卡片组件 + 部署卡片）。
