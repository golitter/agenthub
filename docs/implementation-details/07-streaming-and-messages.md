# SSE、消息持久化与渲染超详细实现

## 实现了什么

流式链路连接 AgentEnd 的异步 Agent 事件、Backend 的可恢复中转和浏览器的增量 UI。设计目标同时满足低延迟、断线续接、最终持久化、身份不混淆和大对象不进入 SSE。

## 怎么实现的

### 事件契约

`contracts/schemas/event-types.yaml` 定义 EventType 与 StreamEvent。事件覆盖 init、text、tool_call、tool_result、artifact、planning、plan_review、done、error、heartbeat，以及 runtime、coordination、ask card 等扩展。StreamEvent 至少有 type 与 data，可带 sequence/timestamp 等契约字段。

事件 type 是 SSE event name，也是前端 listener 的分派 key。新增事件必须同步 schema、生成文件、AgentEnd emitter、Backend passthrough/persistence 和前端 reducer。

### AgentEnd 产生事件

Adapter 将 CLI 私有 stdout 转为 StreamEvent：

- 首个 init 建立 Agent/Session/Message/Run 上下文。
- text 是可直接拼接的正文增量。
- tool_call/tool_result 保存工具生命周期而非混进正文。
- runtime 与 coordination 保存多 Agent 执行状态和身份。
- artifact 只携带 resource metadata。
- done/error 是终态信号。

事件进入 RunSupervisor journal 后才出站，因此按 `after_seq` 可以重新读取。sanitizer 对文本和工具 payload 进行预算裁剪，防止单事件压垮链路。

### Backend 建立流

run 请求由 TaskService 调 AgentEnd stream。Backend 的 `stream_helper` 解析 SSE frame，验证/补充 task-session-message 归属，写两个通道：

1. RuntimeHub：内存 pub/sub，当前在线消费者低延迟收到。
2. Redis Stream：持久有序事件，支持消费者迟到、重连和多实例协调基础。

Redis stream key 由稳定标识构造。sequence/Redis ID 与 Message.last_seq 用于判断 MySQL 已落到哪里，避免重复追加文本。

### RuntimeHub

Hub 按 stream key 管理 subscriber channel：

- subscribe 返回 channel 和 unsubscribe。
- publish 向当前订阅者广播。
- 慢消费者不能无限阻塞生产者；实现采用受限 channel/非阻塞策略。
- 无订阅时仍由 Redis 路径保存事件。
- 连接关闭时必须注销，防止 channel 泄漏。

Hub 是性能层，不是恢复真相；进程重启会丢失 Hub，但不能丢 Redis/MySQL 已确认数据。

### Redis 到 MySQL writer

writer 消费 Redis Stream，将高频 text 合并后批量更新 Message.content，而不是每个 token 执行一次 SQL。它同时：

- 创建/确认 streaming Agent Message。
- 保存 last sequence。
- 将 Agent type/name/group/run identity 写入消息。
- 在 done 设置 completed。
- 在 error/cancel/timeout 设置 failed 或 termination reason。
- 重试瞬时数据库错误，避免静默 ack 未落库事件。

Backend 启动时扫描遗留 streaming 消息并按恢复规则修正，避免上次崩溃留下永远 loading 的 UI。

### 浏览器订阅

公开端点要求 `session_id` 和 `message_id`，路径含 task id。`StreamController` 在校验通过前不发送 `text/event-stream` header；因此 not found/bad request 仍是正常 JSON。开始后设置：

- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- `Connection: keep-alive`
- `X-Accel-Buffering: no`

Frontend EventSource 使用实际 RunTaskResponse 中的 route/session/message，不从当前 UI 选中值猜测。heartbeat 保持中间代理连接，前端不把它渲染为消息。

### 断线与重连

恢复由三层组成：

- EventSource 网络重连。
- Backend 从 Redis Stream/last sequence 补发缺口。
- 页面刷新后从 MySQL messages 获取已落库正文，再订阅仍 active 的 Run/stream。

前端 generation guard 丢弃旧 EventSource 在 close 后排队投递的事件。Backend 用 sequence 去重；Message writer 只推进 last_seq。done/error 的处理必须幂等，因为终态可能在重连边界重复观察。

### 消息所有权

Message 同时保存 task_id、session_id、agent_type、agent_name、group_id、run_id。群聊显示层按 Task 聚合，但写入时必须保留产生事件的 Session/Agent。Orchestrator 子 Agent 消息不能全部冒充主 Orchestrator，否则头像、工作区和历史筛选都会错误。

### 分页与流式消息合并

初次进入会话获取最近页；向上滚动用数字主键 before 加载更早页。UI 合并时：

- 以 message_id 去重。
- 服务端页保持时间正序显示。
- 正在 streaming 的本地消息与服务端同 ID 消息 patch，而非追加副本。
- done 后以服务端返回为准对账正文、状态和 metadata。
- 切换 Session 不销毁其他 Session map 中的流。

### 结构化卡片

运行时事件和 `aka_yhy` block 最终统一为 MessageBlock。持久化 Message.content 仍可包含 fenced marker，保证刷新后重建同一 UI。工具、计划、Diff、Artifact 等块需要稳定 id，避免每个 token 导致 React remount。

### Artifact 分流

HTML 等大资源流程：

1. Backend 为特定 task/session/message 签发短期上传 capability，并通过 Run request 环境传给 builtin render。
2. render 计算大小和 SHA-256，以 multipart 上传 `/api/internal/artifacts`。
3. Backend 验证 capability、kind、数量、大小、摘要和消息绑定，写私有 MinIO 与 Artifact metadata。
4. SSE 只发 `resource_id,kind,filename,size,sha256`。
5. Frontend 用 `/api/artifacts/:resourceId/content` 加载。

这样 Redis/MySQL/SSE 不承载 HTML 全文，且浏览器拿不到 MinIO 凭据。

### 常见失败定位

| 现象 | 检查顺序 |
|---|---|
| 页面无增量但最终刷新有内容 | Frontend listener → RuntimeHub subscriber → Nginx buffering |
| 刷新后内容缺少尾部 | Redis stream → writer log → Message.last_seq/status |
| 重复文本 | sequence 去重 → EventSource generation → cache upsert key |
| 群聊头像/名称错 | AgentEnd event identity → Backend Message fields → Block metadata |
| 永远 streaming | AgentEnd Run terminal → Backend done/error → startup stale cleanup |
| HTML 卡片失败 | capability TTL/binding → Artifact status → MinIO → CSP/iframe |
