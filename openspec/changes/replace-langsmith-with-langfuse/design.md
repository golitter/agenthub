## Context

AgentEnd has two explicit LangSmith integrations. Non-Orchestrator CLI adapters are wrapped by `src/adapters/trace.py`, which converts `StreamEvent` activity into LangSmith `RunTree` runs. The Orchestrator relies on LangGraph's environment-driven LangSmith integration, with `get_config()` propagated into the reason-node LLM loop. Configuration and active documentation expose `LANGSMITH_*` variables.

The application services run locally during development. A local Langfuse v3 deployment would require Web, Worker, PostgreSQL, ClickHouse, Redis, and object storage, which is too resource-intensive for the desired workflow. The selected provider is therefore Langfuse Cloud Hobby in the Tokyo region. AgentEnd remains the only telemetry producer; frontend, Backend, shared contracts, and SSE payloads remain unchanged.

Agent traces may contain private source code, prompts, workspace paths, tool payloads, and group-chat context. The integration must therefore collect structural metadata by default and require explicit opt-in for content capture.

## Goals / Non-Goals

**Goals:**

- Remove every project-owned import, call, configuration variable, and active operational instruction for LangSmith.
- Add optional Langfuse Cloud observability for both Orchestrator and CLI-agent execution.
- Preserve existing LangChain/LangGraph execution and runnable-config propagation.
- Keep tracing transparent to SSE consumers and non-blocking for AgentEnd startup and request execution.
- Provide useful session/task/agent correlation, timing, tool activity, usage, errors, and cancellation visibility.
- Prevent prompt, source-code, path, and tool-payload disclosure by default.
- Make the Tokyo Cloud endpoint, sampling, content capture, masking, and payload limits explicit configuration.
- Ensure queued telemetry is flushed during graceful AgentEnd shutdown.

**Non-Goals:**

- Removing LangChain, LangGraph, or a LangSmith package installed transitively by `langchain-core`.
- Running Langfuse, ClickHouse, PostgreSQL, Redis, or MinIO locally.
- Making Langfuse availability part of AgentEnd health or readiness.
- Changing frontend behavior, Backend APIs, shared contracts, generated types, or SSE event schemas.
- Migrating historical LangSmith traces into Langfuse.
- Introducing Langfuse prompt management, datasets, evaluators, or production alerting in this change.
- Uploading full prompts, source code, tool inputs, or tool outputs by default.

## Decisions

### 1. Use Langfuse Cloud Tokyo as an optional external sink

AgentEnd will use the Langfuse Python SDK and default documented `LANGFUSE_BASE_URL` to `https://jp.cloud.langfuse.com`. Credentials remain in the untracked `agentend/.env`. Tracing is active only when `LANGFUSE_TRACING_ENABLED` is not false and both public and secret keys are present.

This avoids the memory and operational burden of a six-service local Langfuse stack. A self-hosted deployment was considered and rejected for this change. The integration remains endpoint-configurable so a future deployment decision does not require code changes.

### 2. Centralize provider-specific code under an observability module

Langfuse client access, attribute propagation, masking, content policy, and CLI event tracing will live under `agentend/src/observability/`. API and adapter code will call narrow helpers rather than import Langfuse throughout the runtime.

The existing `trace_stream_events()` call shape may be retained during migration, but its implementation and ownership move out of `src/adapters/trace.py`. This keeps adapters focused on Agent protocol translation and limits future provider changes to one module.

### 3. Use the Langfuse callback integration for Orchestrator LangGraph runs

Each top-level Orchestrator execution will create a Langfuse `CallbackHandler` and include it in the config passed to `graph.astream()`. The config will retain `configurable.thread_id`, add a stable run name, and carry request metadata.

The existing `get_config()` propagation in `reason_node` remains because it is provider-neutral LangGraph context propagation. It will carry the Langfuse callback to nested `llm_with_tools.ainvoke()` calls instead of triggering LangSmith. Direct LLM calls outside the reason loop will be audited and tested for callback inheritance; where inheritance is absent, they will receive the current runnable config explicitly.

### 4. Model CLI-agent execution as an opaque agent turn

CLI tools do not expose their internal prompts or individual provider LLM calls. The integration will not fabricate an internal model-call tree. Each non-Orchestrator request will create:

- a root `agent-turn` span carrying correlation and terminal status;
- an initialization span when INIT metadata is available;
- one opaque `cli-response` generation that aggregates TEXT output and receives DONE usage when available;
- tool spans created at TOOL_CALL and completed by the matching TOOL_RESULT;
- error or interrupted status on exceptional termination.

Same-name pending tools remain matched from newest to oldest. Unfinished tool spans are closed with an error at stream termination. Original `StreamEvent` objects are yielded unchanged and no observation is created per text chunk.

### 5. Propagate stable correlation attributes

The root context will propagate `session_id`, a trace name, tags, and bounded string metadata such as `task_id`, `agent_type`, CLI session identifier, and Orchestrator iteration. The current request contract has no user identifier, so this change will not populate `user_id` or modify contracts.

Absolute workspace and repository paths are not correlation identifiers and will not be uploaded by default. If path context is later needed, it must be reduced to a non-sensitive logical name or hash.

### 6. Enforce metadata-only collection by default

Project-owned controls will default to:

- `LANGFUSE_CAPTURE_CONTENT=false`
- `LANGFUSE_CAPTURE_TOOL_INPUT=false`
- `LANGFUSE_CAPTURE_TOOL_OUTPUT=false`

When content capture is disabled, observations contain structural metadata, names, timing, terminal status, model/usage fields when available, and sanitized error summaries. When an operator explicitly enables content capture, masking and length limits are still applied before values reach the Langfuse SDK.

The implementation will use explicit allowlists rather than serializing all `stream_kwargs`. Secrets, authorization values, environment variables, object instances, full local paths, and unsupported/non-serializable values are never uploaded.

### 7. Treat observability as best-effort

Missing or partial credentials produce a no-op integration and a bounded configuration warning. Langfuse authentication, network, serialization, batching, or exporter failures are logged but do not fail startup, change session state, interrupt the underlying event iterator, or alter emitted SSE data.

AgentEnd will not synchronously call `auth_check()` as a startup gate. The SDK batches in the background. FastAPI lifespan shutdown invokes the Langfuse client shutdown/flush path after normal runtime cleanup, with errors contained and logged.

### 8. Remove explicit LangSmith ownership without fighting transitive dependencies

Project source and active configuration will contain no direct `langsmith` import, `RunTree`, or `LANGSMITH_*` variable. `pyproject.toml` has no direct LangSmith dependency today; `uv.lock` may continue to contain LangSmith because `langchain-core` declares it as a dependency. This is accepted only as an unused transitive package.

Historical audit records remain unchanged because they describe past behavior. Active design, setup, reference, and AGENTS documentation will describe Langfuse as the current provider.

## Risks / Trade-offs

- **Sensitive repository or prompt data could leave the machine** → Default to metadata-only capture, use explicit allowlists, mask secrets, truncate opted-in content, and test the sanitizer independently.
- **The free Cloud allowance may be exhausted by detailed graph traces** → Preserve SDK sampling controls, document `LANGFUSE_SAMPLE_RATE`, avoid per-token observations, and review usage before enabling content or full sampling broadly.
- **Tokyo Cloud connectivity may be unavailable or slow from some networks** → Keep export asynchronous and best-effort; Langfuse outages never become an application dependency.
- **Queued observations may be lost during forced process termination** → Flush on graceful shutdown and accept loss during hard kills as an observability-only failure.
- **Callback context may not reach direct LangChain calls in every Orchestrator module** → Add integration tests covering graph nodes, reason-loop calls, coordination, skill selection, memory, and aggregation paths; pass runnable config explicitly where required.
- **CLI usage is aggregate rather than attributable to individual hidden model calls** → Mark the generation as opaque and avoid presenting inferred internal model structure as factual.
- **A transitive LangSmith package remains visible in `uv.lock`** → Define acceptance around absence of direct project imports/configuration/calls, not absence from the dependency tree.
- **Hobby retention and quotas limit long-term analysis** → Treat Cloud Hobby as development observability; historical trace migration and paid-plan decisions are outside this change.

## Migration Plan

1. Add the Langfuse SDK, observability configuration, no-op client behavior, masking helpers, and tests without enabling tracing by default.
2. Replace the CLI `RunTree` wrapper with Langfuse-native observations and validate all StreamEvent terminal paths, including cancellation.
3. Add a Langfuse callback to Orchestrator graph execution, verify nested LLM/tool coverage, and retain provider-neutral runnable-config propagation.
4. Add graceful client shutdown and fault-isolation tests.
5. Update `.env.example` and active documentation for Tokyo Cloud setup, free-plan sampling, privacy defaults, and troubleshooting.
6. Remove all active `LANGSMITH_*`, `RunTree`, and direct LangSmith references from source and configuration.
7. Verify with mocked exporters first, then opt-in to a development Langfuse project and compare representative CLI and Orchestrator traces.

Rollback consists of disabling Langfuse with `LANGFUSE_TRACING_ENABLED=false` or removing its credentials. The runtime remains functional without an observability provider; the removed LangSmith integration will not be restored as an automatic rollback path.

## Open Questions

- The exact opted-in content length limit should be selected during implementation based on representative tool outputs; it must be finite and centrally configured.
- Before enabling content capture for any shared project, maintainers must decide whether repository and user-data policy permits third-party Cloud storage. Metadata-only mode does not require this decision to complete the migration.
