## 1. Dependency and Configuration Foundation

- [x] 1.1 Add a compatible Langfuse Python SDK constraint to `agentend/pyproject.toml`, refresh `agentend/uv.lock`, and verify LangChain/LangGraph versions remain unchanged.
- [x] 1.2 Create the `agentend/src/observability/` package with centralized Langfuse client access, tracing-enabled evaluation, and no-op behavior for disabled or incomplete configuration.
- [x] 1.3 Add typed parsing for sampling, Tokyo Cloud base URL, environment, release, content-capture flags, and a finite payload length limit without making Langfuse a required application setting.
- [x] 1.4 Add configuration tests for valid credentials, explicit disablement, missing keys, partial keys, zero sampling, partial sampling, and malformed optional values.

## 2. Privacy and Payload Safety

- [x] 2.1 Implement explicit metadata allowlisting for task, session, agent, CLI-session, iteration, model, usage, timing, and terminal-status fields.
- [x] 2.2 Implement centralized recursive masking for API keys, authorization values, configured secret patterns, and other credential-like strings.
- [x] 2.3 Implement bounded serialization and truncation that omits object instances, unsupported values, environment data, and absolute workspace or repository paths by default.
- [x] 2.4 Enforce independent opt-in controls for message/prompt content, tool input, and tool output while retaining masking and truncation after opt-in.
- [x] 2.5 Add unit tests proving metadata-only defaults, category-specific opt-in, secret redaction, path omission, unsupported-value omission, and payload truncation.

## 3. CLI Agent Observation Migration

- [x] 3.1 Move `trace_stream_events()` ownership from `src/adapters/trace.py` into the observability package and replace the direct LangSmith `RunTree` implementation with Langfuse-native observations.
- [x] 3.2 Implement the CLI trace model with a root agent-turn span, INIT metadata, one opaque aggregated response generation, matched tool spans, DONE usage, and terminal status.
- [x] 3.3 Preserve newest-first matching for overlapping same-name tools and close every unmatched pending tool with an error when the stream terminates.
- [x] 3.4 Handle emitted ERROR events, iterator exceptions, and `asyncio.CancelledError` as distinct terminal states while re-yielding events and re-raising underlying failures unchanged.
- [x] 3.5 Update the Agent API stream integration to pass only approved correlation metadata and remove serialization of arbitrary `stream_kwargs`.
- [x] 3.6 Add async unit tests for successful streams, text aggregation, same-name tools, unmatched results, missing tool results, ERROR events, raised exceptions, cancellation, and enabled/disabled event-stream transparency.

## 4. Orchestrator LangGraph Integration

- [x] 4.1 Create a Langfuse callback per traced Orchestrator execution and attach it to the top-level `graph.astream()` config alongside `thread_id`, run name, and bounded metadata.
- [x] 4.2 Propagate task/session/agent/iteration attributes around each Orchestrator execution so replanning iterations remain correlated to the originating request and session.
- [x] 4.3 Retain `get_config()` propagation in `reason_node`, remove LangSmith-specific comments, and verify nested `llm_with_tools.ainvoke()` calls inherit the Langfuse callback.
- [x] 4.4 Audit coordination, skill selection, pin memory, result aggregation, and other direct LangChain model calls; explicitly pass runnable config where callback inheritance is absent.
- [x] 4.5 Add tests that verify Graph, node, LLM, and tool callback coverage, multiple replan iterations, missing runnable context fallback, and unchanged Orchestrator StreamEvent output.

## 5. Failure Isolation and Lifecycle

- [x] 5.1 Contain Langfuse initialization, authentication, masking, serialization, network, batching, and exporter errors so they cannot fail AgentEnd startup or Agent requests.
- [x] 5.2 Add Langfuse client shutdown/flush to FastAPI lifespan cleanup without adding synchronous per-request flushes.
- [x] 5.3 Verify health and readiness do not depend on Langfuse credentials or endpoint reachability.
- [x] 5.4 Add tests for unreachable Cloud endpoint, exporter exceptions, shutdown success, shutdown failure, and preservation of business session state and SSE output.

## 6. Explicit LangSmith Removal

- [x] 6.1 Remove direct `langsmith` imports, `RunTree` usage, LangSmith helper code, and active `LANGSMITH_*` reads from AgentEnd runtime source.
- [x] 6.2 Remove `LANGSMITH_*` entries from active environment templates and replace them with documented Tokyo-region `LANGFUSE_*`, sampling, and content-capture variables.
- [x] 6.3 Confirm that any remaining LangSmith package in `uv.lock` is introduced only transitively by `langchain-core` and is never imported or configured by project code.
- [x] 6.4 Add a source/config audit check that fails on new direct LangSmith imports, `RunTree`, or active `LANGSMITH_*` usage while excluding immutable historical audit records.

## 7. Documentation and Verification

- [x] 7.1 Replace active LangSmith design and operation guides with Langfuse Cloud Tokyo setup, trace models, privacy defaults, sampling, quota awareness, failure behavior, and troubleshooting.
- [x] 7.2 Update `agentend/AGENTS.md`, `agentend/docs/reference/details.md`, Orchestrator architecture documentation, root setup guidance, and all affected indexes or links.
- [x] 7.3 Preserve historical LangSmith audit/change records as historical facts and add a new migration record where the documentation conventions require one.
- [x] 7.4 Run AgentEnd formatting, linting, and unit tests, then run the repository-prescribed relevant test targets.
- [ ] 7.5 With explicit operator opt-in and test credentials, validate one Claude/OpenCode/Codex stream and one Orchestrator run in a Langfuse Cloud Tokyo development project without exposing default-prohibited content.
- [x] 7.6 Re-run OpenSpec validation and source/config scans, confirming no frontend, Backend, Docker, contract schema, generated type, or SSE protocol change was introduced.
