# 契约变更：AgentRequest 增加资源上传上下文

## 变更原因

内置 `render html-render` 生成的 HTML 不再通过 Agent SSE 链路传给前端，而是由 AgentEnd 将内容上传到 Backend/MinIO。Backend 需要把本次消息的关联 ID 与短期上传能力令牌传给 AgentEnd，AgentEnd 仅把令牌注入 builtin skill 子进程环境，不将令牌或大内容写入 SSE。

## 变更文件

- `contracts/schemas/agent-request.yaml`

## 契约变更

- 新增可选 `message_id: string | null`：资源归属的消息 ID。
- 新增可选 `artifact_upload_token: string | null`：Backend 签发的资源上传能力令牌。

## 跨端影响

- Backend 在创建任务时填充两个字段，并负责令牌签发与校验。
- AgentEnd 接收字段并为 `render` 进程注入上传端点所需上下文；普通 Agent CLI 不应读取或输出令牌。
- Frontend 不直接发送这两个字段；生成的类型文件保持协议一致。
