## REMOVED Requirements

### Requirement: LangSmith tracing enabled by environment variables
**Reason**: Project-owned LangSmith configuration and reporting are being removed in favor of optional Langfuse Cloud observability.

**Migration**: Remove `LANGSMITH_TRACING`, `LANGSMITH_API_KEY`, and `LANGSMITH_PROJECT`; configure the `LANGFUSE_*` variables defined by the `agent-observability` capability.

### Requirement: LLM config propagation in reason_node
**Reason**: The requirement is LangSmith-specific. Runnable-config propagation remains as an implementation requirement of the new provider-neutral Orchestrator observability behavior.

**Migration**: Retain `get_config()` propagation where required, but attach the Langfuse callback at the top-level LangGraph invocation and remove LangSmith-specific comments and expectations.

### Requirement: Complete LLM call visibility
**Reason**: Unconditional full-message visibility conflicts with the new metadata-only privacy default and names the retired provider.

**Migration**: Use `agent-observability` requirements for Langfuse generation visibility; full content is exported only through explicit opt-in with masking and truncation.
