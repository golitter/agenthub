class IntegrationError(RuntimeError):
    """Stable machine-readable IntegrationService error."""

    def __init__(self, code: str, message: str = "") -> None:
        self.code = code
        self.message = message or code
        # Keep the stable machine-readable code visible to logs and callers
        # that only receive the exception string.  ``code`` remains the
        # canonical field for structured responses.
        super().__init__(f"{code}: {self.message}")


def sanitize_error_text(value: object, *, limit: int = 4096) -> str:
    """Bound diagnostic text before it reaches durable/UI projections."""
    text = str(value or "")
    text = "".join(char if ord(char) >= 32 else " " for char in text)
    return text[:limit]


ERROR_INTEGRATION_RESULT_INVALID = "integration_result_invalid"
ERROR_INTEGRATION_VERSION_UNSUPPORTED = "integration_version_unsupported"
ERROR_OPERATION_NOT_FOUND = "operation_not_found"
ERROR_OPERATION_BINDING_MISMATCH = "operation_binding_mismatch"
ERROR_OPERATION_STALE_ATTEMPT = "operation_stale_attempt"
ERROR_OPERATION_CANCELLED = "operation_cancelled"
ERROR_OPERATION_TERMINAL_MISMATCH = "operation_terminal_mismatch"
ERROR_CAPABILITY_INVALID = "integration_capability_invalid"
ERROR_WORKSPACE_MISSING = "workspace_missing"
ERROR_SOURCE_MISSING = "source_missing"
ERROR_TARGET_MOVED = "target_moved"
ERROR_MERGE_CONFLICT = "merge_conflict"
ERROR_MERGE_FAILED = "merge_failed"
ERROR_STATE_UNCERTAIN = "integration_state_uncertain"
