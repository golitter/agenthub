# 契约变更：消息持久化 + Agent 头像 + repoPath 校验

## 变更原因

支持聊天消息持久化存储、Agent 自定义头像上传、以及创建 Task 时设置并校验 repoPath。

## 变更文件

- `contracts/schemas/message.yaml` — **新增** Message 数据模型
- `contracts/schemas/validate-repo-path.yaml` — **新增** repoPath 校验请求/响应

## 契约变更

### 新增 message.yaml

定义 `Message` 结构和 `MessageRole` 枚举（user/agent），用于三端共享消息类型。

字段：id, task_id, session_id, role, content, agent_type, agent_name, created_at

### 新增 validate-repo-path.yaml

定义 `ValidateRepoPathRequest`（含 repo_path 字段）和 `ValidateRepoPathResponse`（含 valid + errors 字段）。

## 跨端影响

- **Backend**: 新增 Message 模型和 CRUD API，新增头像上传/更新 API，新增 repoPath 校验转发端点
- **Frontend**: 新增消息历史加载、AgentAvatar 支持 DiceBear fallback、Agent 编辑弹窗、NewChatDialog 新增 repoPath 输入
- **AgentEnd**: 新增 `/v1/validate-repo-path` 校验接口
