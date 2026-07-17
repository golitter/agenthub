## ADDED Requirements

### Requirement: Optional Langfuse Cloud configuration
AgentEnd SHALL support optional Langfuse Cloud tracing configured by `LANGFUSE_TRACING_ENABLED`, `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY`, `LANGFUSE_BASE_URL`, `LANGFUSE_TRACING_ENVIRONMENT`, `LANGFUSE_RELEASE`, and `LANGFUSE_SAMPLE_RATE`. The documented Cloud endpoint SHALL be the Tokyo region at `https://jp.cloud.langfuse.com`.

#### Scenario: Valid Cloud configuration
- **WHEN** tracing is enabled and both Langfuse project keys are configured
- **THEN** AgentEnd SHALL send sampled telemetry to the configured Langfuse endpoint

#### Scenario: Tracing explicitly disabled
- **WHEN** `LANGFUSE_TRACING_ENABLED=false`
- **THEN** AgentEnd SHALL execute requests without creating or exporting Langfuse observations

#### Scenario: Credentials missing or incomplete
- **WHEN** either Langfuse project key is absent
- **THEN** observability SHALL operate as a no-op and AgentEnd SHALL remain available

### Requirement: No explicit LangSmith integration
Project-owned runtime code and active configuration SHALL NOT import or invoke LangSmith, construct `RunTree` objects, or read `LANGSMITH_*` variables. A LangSmith package installed only as an unused transitive dependency of `langchain-core` MAY remain in the resolved dependency tree.

#### Scenario: Source and configuration audit
- **WHEN** runtime source and active environment templates are scanned for direct LangSmith imports, `RunTree`, and `LANGSMITH_*`
- **THEN** no active integration reference SHALL be found

#### Scenario: Transitive dependency remains
- **WHEN** dependency resolution installs LangSmith solely through `langchain-core`
- **THEN** the build SHALL remain compliant provided project code neither imports nor configures that package

### Requirement: Orchestrator Langfuse tracing
Each traced Orchestrator execution SHALL attach a Langfuse callback to the top-level LangGraph invocation and propagate the current runnable config to nested LangChain model calls. The trace SHALL correlate graph activity, model generations, and tool execution with the current task, session, agent type, and iteration when available.

#### Scenario: Graph execution is traced
- **WHEN** an Orchestrator request runs with valid Langfuse configuration
- **THEN** Langfuse SHALL receive a correlated graph trace containing observable node, LLM, and tool activity

#### Scenario: Nested reason call inherits tracing
- **WHEN** `reason_node` invokes `llm_with_tools.ainvoke()` inside a traced graph context
- **THEN** the current runnable config SHALL be passed so the LLM generation is attached to the active Langfuse trace

#### Scenario: Runnable context is unavailable
- **WHEN** a LangChain model call executes outside a LangGraph runnable context
- **THEN** the call SHALL continue normally without requiring a tracing callback

### Requirement: Transparent CLI-agent observations
Each traced non-Orchestrator stream SHALL create a correlated agent-turn observation that represents initialization, aggregated assistant output, tool activity, usage, terminal status, and errors without changing the source event stream. The system SHALL NOT create one observation per text chunk or claim visibility into hidden CLI-internal model calls.

#### Scenario: Successful CLI stream
- **WHEN** a CLI adapter emits INIT, TEXT, TOOL_CALL, TOOL_RESULT, and DONE events
- **THEN** Langfuse SHALL receive one correlated agent turn with aggregated response output, matched tool spans, available usage, and completed status

#### Scenario: Downstream stream transparency
- **WHEN** CLI tracing is enabled or disabled
- **THEN** downstream consumers SHALL receive the same StreamEvent objects in the same order

#### Scenario: Same-name tools overlap
- **WHEN** multiple pending calls use the same tool name
- **THEN** TOOL_RESULT SHALL complete the newest matching pending tool span

#### Scenario: Tool result is missing
- **WHEN** the stream terminates while a tool span remains pending
- **THEN** the pending span SHALL be closed with an error indicating that the result was not observed

### Requirement: Correct exceptional terminal states
Observability SHALL distinguish successful completion, emitted error events, raised exceptions, and asynchronous stream cancellation. It SHALL close all active observations on every terminal path and SHALL re-raise underlying exceptions or cancellation after recording status.

#### Scenario: Adapter emits an error event
- **WHEN** a CLI adapter emits an ERROR event
- **THEN** the root agent turn SHALL end with error status while the original ERROR event is still yielded

#### Scenario: Event iteration raises
- **WHEN** the wrapped event iterator raises an exception
- **THEN** active observations SHALL close with error status and the same exception SHALL propagate

#### Scenario: SSE consumer cancels
- **WHEN** stream iteration is cancelled with `asyncio.CancelledError`
- **THEN** active observations SHALL close with interrupted status and cancellation SHALL propagate rather than being reported as completed

### Requirement: Metadata-only privacy default
Agent observability SHALL default to structural metadata collection and SHALL require explicit opt-in before exporting prompt/message content, tool input, or tool output. Exported values SHALL pass through centralized allowlisting, masking, serialization validation, and finite length limits.

#### Scenario: Default capture policy
- **WHEN** no content-capture variables are enabled
- **THEN** observations SHALL omit full prompts, messages, source code, absolute paths, tool arguments, and tool results

#### Scenario: Content capture opted in
- **WHEN** an operator explicitly enables a content-capture category
- **THEN** only that category SHALL be eligible for export and its values SHALL still be masked and truncated

#### Scenario: Secret-like data is present
- **WHEN** an eligible payload contains configured secret patterns or authorization values
- **THEN** the exported payload SHALL contain a redacted value rather than the secret

#### Scenario: Unsupported metadata value
- **WHEN** runtime metadata contains an object instance or another unsupported non-serializable value
- **THEN** the value SHALL be omitted without failing the Agent request

### Requirement: Stable trace correlation
AgentEnd SHALL propagate bounded correlation attributes to observations, including `session_id`, `task_id`, `agent_type`, trace name, and applicable CLI session or Orchestrator iteration metadata. It SHALL NOT fabricate a user identifier that is absent from the current Agent request contract.

#### Scenario: Multi-turn session
- **WHEN** multiple Agent requests share a session identifier
- **THEN** their Langfuse observations SHALL be queryable as the same session

#### Scenario: Request has no user identity
- **WHEN** AgentEnd receives the current AgentRequest without a user identifier
- **THEN** observability SHALL leave Langfuse `user_id` unset

### Requirement: Observability failure isolation
Langfuse initialization, authentication, serialization, batching, network, and exporter failures SHALL NOT prevent AgentEnd startup, fail an Agent execution, change session state, or alter SSE output. Langfuse SHALL NOT be a dependency of AgentEnd health or readiness.

#### Scenario: Langfuse is unreachable
- **WHEN** the Cloud endpoint is unavailable during an Agent request
- **THEN** the Agent request and SSE stream SHALL continue and the observability failure SHALL be contained and logged

#### Scenario: AgentEnd starts without Langfuse
- **WHEN** tracing is disabled, unconfigured, or the endpoint cannot be reached
- **THEN** AgentEnd health and readiness SHALL remain based only on required application dependencies

### Requirement: Batched export and graceful shutdown
AgentEnd SHALL use the Langfuse SDK's background batching for normal requests and SHALL attempt to flush and shut down the Langfuse client during graceful FastAPI lifespan shutdown. Normal requests SHALL NOT synchronously flush telemetry.

#### Scenario: Normal request completes
- **WHEN** an Agent request finishes
- **THEN** its response SHALL not wait for an explicit per-request telemetry flush

#### Scenario: Application shuts down gracefully
- **WHEN** FastAPI lifespan cleanup runs
- **THEN** AgentEnd SHALL attempt to export pending observations and contain any shutdown-export failure

### Requirement: Configurable sampling
AgentEnd SHALL honor the configured Langfuse sample rate so operators can control Cloud usage without code changes. Sampling SHALL not change Agent execution or SSE behavior.

#### Scenario: Partial sampling configured
- **WHEN** `LANGFUSE_SAMPLE_RATE` is set to a value between zero and one
- **THEN** the SDK SHALL sample traces at that rate while all Agent requests continue to execute

#### Scenario: Sampling disabled
- **WHEN** `LANGFUSE_SAMPLE_RATE=0`
- **THEN** no request trace SHALL be exported and Agent behavior SHALL remain unchanged
