# Langfuse Cloud 可观测性设计

## 目标

AgentEnd 使用 Langfuse Cloud 日本（东京）区域记录 Agent 运行轨迹，不部署本地 Langfuse。可观测性是可选能力：未配置、配置不完整、采样为零或云端不可达时，服务启动、健康检查、SSE 输出与会话状态都不受影响。

默认只上报关联元数据，不上报用户消息、模型提示词、工具输入、工具输出、环境变量或绝对工作区路径。

## 接入结构

- src/observability/config.py：独立解析 LANGFUSE_* 环境变量，不进入必填应用配置。
- src/observability/privacy.py：元数据白名单、密钥遮罩、路径省略、有限长度序列化。
- src/observability/client.py：Langfuse 客户端单例、失败隔离、退出 flush。
- src/observability/cli_trace.py：Claude Code、OpenCode、Codex 的不透明事件流映射。
- src/observability/orchestrator.py：LangGraph callback 与 trace 属性传播。

## Trace 模型

CLI Agent 每轮创建一个 agent 根 observation，包含聚合的 generation、INIT 元数据、按最新优先匹配的 tool observation、DONE usage 与 completed/error/interrupted 终态。它不伪造 CLI 内部 LLM 调用。

Orchestrator 为每次 Graph 执行（包括重规划迭代）创建一个 Langfuse callback。thread_id、run name、task/session/agent/iteration 元数据随 runnable config 传播；Graph 节点、LangChain LLM 和工具调用形成原生层级。

## 隐私边界

三个内容开关默认均为 false。只有显式开启对应开关后才采集该类别。即使开启，仍执行密钥和自定义正则遮罩、绝对路径省略、非数据对象省略与有效载荷长度限制。

## 可靠性与配额

SDK 初始化、序列化、mask、网络、batch/export 与 shutdown 异常都在观测边界内处理。业务事件按原对象、原顺序继续产出；底层迭代器自身的异常和取消保持原样抛出。请求路径不做同步 flush，进程 lifespan 退出时统一 shutdown。

免费 Hobby 项目应从较低采样率开始，例如 LANGFUSE_SAMPLE_RATE=0.1。遇到配额压力时优先降低采样并保持内容采集关闭。
