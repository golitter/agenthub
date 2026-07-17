# Langfuse Cloud Tokyo 配置指南

## 1. 创建项目

在 Langfuse Cloud 日本区域创建免费项目，并取得 Public Key 与 Secret Key。AgentEnd 默认地址为 https://jp.cloud.langfuse.com。不同区域的 key 不可混用。

## 2. 配置 AgentEnd

复制环境模板，在 agentend/.env 中填写：

~~~dotenv
LANGFUSE_TRACING_ENABLED=true
LANGFUSE_PUBLIC_KEY=pk-lf-CHANGE_ME
LANGFUSE_SECRET_KEY=sk-lf-CHANGE_ME
LANGFUSE_BASE_URL=https://jp.cloud.langfuse.com
LANGFUSE_TRACING_ENVIRONMENT=development
LANGFUSE_RELEASE=
LANGFUSE_SAMPLE_RATE=0.1
LANGFUSE_CAPTURE_CONTENT=false
LANGFUSE_CAPTURE_TOOL_INPUT=false
LANGFUSE_CAPTURE_TOOL_OUTPUT=false
LANGFUSE_MAX_PAYLOAD_CHARS=4000
LANGFUSE_MASK_PATTERNS=
~~~

追踪只有在开关开启且两把 key 都存在时才启用。删除 key 或设置 LANGFUSE_TRACING_ENABLED=false 即可关闭；无需修改 config.yaml。

## 3. 本地启动与验证

Langfuse 在云上，AgentHub 三端仍使用 make dev 在本地启动。不需要 Langfuse Docker Compose，也不会增加本地常驻容器。

先保持三个内容开关为 false，执行一次 CLI Agent 对话和一次 Orchestrator 任务。东京项目中应出现 CLI 的 agent turn、opaque generation、tool、usage 与终态，以及 Orchestrator 的 Graph、节点、LLM、tool 和 iteration 层级；不应出现原始消息、prompt、工具内容、密钥或绝对路径。

只有在明确获得操作者同意并使用测试数据时，才临时打开某一内容开关。

## 4. 故障排查

- 没有 trace：检查两把 key、开关、东京 base URL 和采样率。
- 401/403：通常是 key 所属区域或项目不匹配。
- trace 延迟：SDK 异步批量上报，等待进程正常退出 flush；不要在请求内强制 flush。
- 云端不可达：业务应继续运行；查看 AgentEnd 日志中的 Langfuse warning/error。
- 配额接近上限：降低 LANGFUSE_SAMPLE_RATE，保持内容采集关闭。
