# 安全与可靠性超详细实现

## 实现了什么

安全边界覆盖浏览器到 Backend、Backend 到 AgentEnd、AgentEnd 到本地文件/进程以及 builtin Skill 到对象存储。可靠性机制覆盖幂等、单调状态、journal、Outbox、lease、原子文件写入、启动恢复和健康检查。

## 怎么实现的

### 信任边界

| 边界 | 输入风险 | 控制 |
|---|---|---|
| Browser → Backend | 未认证调用、超大 body、路径注入、暴力登录 | JWT、Admin JWT、限流、body limit、path sanitize |
| Backend → AgentEnd | 伪造内部控制请求 | 双端 service auth 配置与 Bearer token |
| AgentEnd → Repo | 越权路径、symlink、任意工作目录 | PathPolicy、Workspace ID 解析、realpath containment |
| AgentEnd → CLI | 环境泄漏、僵尸进程、无限运行 | 环境白名单、process group、timeout、budget、supervisor |
| Skill ZIP → 文件系统 | zip slip、zip bomb、symlink、可执行内容 | entry 校验、大小/比例/数量限制、staging、scanner |
| render → Artifact | 通用存储凭据泄漏、跨消息覆盖 | 短期绑定 capability、digest、idempotency |

### 用户与管理认证

Backend `auth.enabled` 控制普通 `/api` JWT。AuthWithSkips 只跳过 `/api/admin/auth`、`/api/admin/health`、`/api/admin/avatar`；其他公开例外通过独立 RouterGroup 注册，而不是扩大 skip 通配。

管理员密码通过 `/api/admin/auth` 验证，登录端点每 IP 每分钟 5 次。AdminAuth 校验 JWT 后保护资源、批量删除、Workspace 管理、Agent/服务/统计和头像更新。若 `skill_storage.require_admin=true`，Skill upload/confirm/delete 也使用 AdminAuth；list/import/remove 仍供普通已认证用户使用。

### 服务认证

AgentEnd 的 `ServiceAuthMiddleware` 和 Backend 的 `ServiceAuth` 使用配置开关和环境 token。比较使用安全方式，错误不回显 token。配置必须成对：仅开启一端会导致内部调用 401，全部关闭只适合 loopback 本地开发。

AgentEnd 启动校验要求：非 loopback host 不能在关闭服务认证时启动；unsafe_process 需要 loopback 且 `allow_unsafe_local_execution` 显式允许。该 fail-fast 防止把本地控制面误暴露到网络。

### PathPolicy

`allowed_repo_roots` 是仓库根白名单。策略使用 expand/absolute/resolve 后判断目标是否 relative-to allowed root，处理 `..` 和 symlink。空列表只在受限本地配置语义下使用。Workspace 文件 API在受管 worktree 根再做第二次 containment。

Backend Workspace proxy 的 string-level `..` 检查不是最终边界；AgentEnd resolved-path 校验才是文件系统权威。两层都保留以减少异常请求到达执行面。

### 输入限制

- Backend 通用 JSON body limit。
- Workspace proxy 25 MiB body limit。
- Avatar 2 MiB、扩展名白名单和 read limit 双检。
- Artifact object 默认 25 MiB + 1 MiB multipart overhead。
- Skill upload/package/file/unpacked/ratio/count 多维限制。
- Message page limit 1..100。
- Repo path Controller 长度上限 512，并由 AgentEnd 做真实路径校验。
- SOUL.md 数据库字段长度与写入校验。

Content-Length 只是提前拒绝，真正读取仍必须使用 limited reader，因为客户端可省略或伪造长度。

### HTTP 与内容安全

- CORS origins 来自配置；凭据模式不能与不受控 wildcard 组合用于外网部署。
- 公开头像使用固定 MIME、nosniff、immutable cache、ETag。
- Artifact HTML 使用 CSP 和 sandbox iframe。
- SSE 禁止代理 buffering。
- 内部对象 key、secret、last error 不进入普通 JSON。
- 错误响应稳定化，不暴露 SQL、宿主绝对路径或 CLI 密钥。

### 子进程治理

Adapter 不通过 shell 字符串拼接命令，使用 argv。子进程进入独立 process group；取消/超时向整个组发送终止，等待 `process_terminate_timeout` 后强杀。stdout 与 stderr 并行 drain，所有 task 在 shutdown 收敛。

`child_process_env` 避免把 Backend/Artifact/LLM 等无关 secret 全量继承给任意 CLI，只按执行需要注入。工作目录固定为 Workspace。

### Run 状态可靠性

- Run start 使用请求指纹保证同 key 同请求。
- repository 在事务中分配 event seq。
- 状态转换只允许契约中的合法边。
- completed、failed、cancelled 三种终态不可被迟到事件覆盖；超时通过 termination reason 细分为 wall time、idle、CPU、memory 等原因。
- parent terminal 后不接受新 child。
- cancel 返回 accepted，不伪装为已完成；真实终态由 supervisor 写入。
- 重启 recover 处理遗留 active run，并保留 integration 仍需恢复的 root。

### 消息可靠性

RuntimeHub 提供实时性，Redis Stream 提供回放，MySQL 提供最终查询。三者不能合并成单一职责。Message.last_seq 和 status 连接流 journal 与数据库。终态写入幂等，Backend 启动修复遗留 streaming。

### Outbox 与 lease

SkillOperationJob、TaskCleanupJob 把数据库提交与外部副作用拆开。Job 包含 attempts、next retry、lease until/token 和 last error。worker 只能完成自己 lease token 对应的 job；lease 超时后其他 worker可接管。

幂等键防止重复意图；AgentSkillID fence 防 ABA；Task cleanup 保存删除时 Session IDs 快照，避免主记录删除后无法知道要清哪些 Workspace。

### 原子本地持久化

Session mapping、Workspace registry、Conversation memory 使用同目录临时文件、flush/fsync 语义和原子 replace。读取端对损坏文件有明确 policy，不接受半 JSON。Memory 额外有 revision compare-and-swap，避免两个并发 turn 后写覆盖先写。

### 就绪与降级

Backend `/ready` 检查所有已启用的必需依赖。关闭 feature gate 表示该依赖不参与 ready，而不是假装功能可用。AgentEnd `/health/ready` 检查生命周期依赖状态。流开始前错误用 JSON；开始后错误以 event/关闭表达。

### 审计与隐私

SkillAuditEvent 保存 actor、action、outcome、hash、文件清单和安全标记。Langfuse 可观测性在 `observability/privacy.py` 过滤 secret、绝对敏感路径和超大 payload；CLI trace 与 Orchestrator trace 共用初始化但保持 span 语义。

日志不得记录 JWT、service token、MinIO secret、Artifact capability、LLM API key 或完整敏感 tool args。

### 高风险改动检查表

| 改动 | 必须验证 |
|---|---|
| 放宽 allowed roots | realpath/symlink、非 loopback、测试覆盖 |
| 增加上传大小 | Gin/FastAPI/Nginx/MinIO/磁盘临时空间四层 |
| 修改取消 | process group、child Run、Message终态、重连 |
| 修改删除 | DB transaction、Outbox snapshot、重复消费、恢复 |
| 修改 Artifact CSP | iframe sandbox、脚本能力、外链资源、XSS |
| 修改 Skill 安装 | zip slip、同 FS rename、backup recovery、fence |
