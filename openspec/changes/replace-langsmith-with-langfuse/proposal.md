## Why

AgentEnd currently couples Orchestrator and CLI-agent tracing to LangSmith through environment-driven LangGraph tracing and a direct `RunTree` integration. The project needs one explicit observability provider with lower local resource cost, predictable failure isolation, and safer defaults for source code, prompts, workspace paths, and tool payloads.

## What Changes

- Replace all project-owned LangSmith tracing code and configuration with Langfuse Cloud tracing in the Tokyo region (`https://jp.cloud.langfuse.com`).
- Add the Langfuse Python SDK as an explicit AgentEnd dependency while retaining LangChain and LangGraph; a LangSmith package pulled transitively by `langchain-core` is permitted but SHALL NOT be imported, configured, or invoked by project code.
- Trace Orchestrator LangGraph runs through the Langfuse LangChain callback integration while preserving runnable-config propagation for nested LLM calls.
- Replace the CLI adapters' direct LangSmith `RunTree` wrapper with Langfuse-native observations that correlate agent turns, aggregated text output, tool calls, usage, errors, and stream cancellation without changing emitted SSE events.
- Make tracing optional and non-blocking: missing credentials, disabled tracing, exporter failures, or Langfuse outages SHALL NOT prevent AgentEnd startup or alter Agent/SSE behavior.
- Default to metadata-only collection. Prompt/message content, source code, absolute paths, tool arguments, and tool results SHALL require explicit opt-in and masking/truncation safeguards.
- Flush pending telemetry during AgentEnd shutdown without making Langfuse a required health dependency.
- Remove active `LANGSMITH_*` configuration and LangSmith-specific documentation, replacing it with Langfuse Cloud setup, privacy, sampling, and troubleshooting guidance.

## Capabilities

### New Capabilities

- `agent-observability`: Optional Langfuse Cloud observability for Orchestrator and CLI-agent execution, including configuration, trace structure, privacy controls, failure isolation, sampling, and lifecycle behavior.

### Modified Capabilities

- `langsmith-trace`: Remove the existing LangSmith environment configuration, callback reporting, and LangSmith visibility requirements; these requirements are superseded by `agent-observability`.

## Impact

- **AgentEnd code:** tracing wrapper, Agent API stream integration, Orchestrator LangGraph config, FastAPI lifespan, and new observability helpers.
- **Dependencies:** add a compatible Langfuse Python SDK and refresh `uv.lock`; retain the existing LangChain/LangGraph stack and tolerate only its transitive LangSmith package.
- **Configuration:** replace active `LANGSMITH_*` variables with `LANGFUSE_*` variables targeting the Tokyo Cloud endpoint; add content-capture and sampling controls.
- **Tests:** add unit coverage for disabled/no-op tracing, CLI event mapping, cancellation, pending tools, masking, callback propagation, exporter failure, and shutdown flush behavior.
- **Documentation:** update AgentEnd instructions, setup guide, observability design/reference documentation, and indexes; preserve historical audit logs as historical records.
- **Unaffected boundaries:** no frontend, Go Backend, Docker deployment, shared contract schema, generated type, or SSE protocol changes are required.
