from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import evals.batch as batch_module
from evals.batch import (
    _build_instruction,
    _completed_case_ids,
    _completed_keys,
    _judge_gate,
    _sandbox_gate,
    orchestration_facts,
    run_batch,
)
from evals.digests import file_digest
from evals.readiness import BatchEvalReadiness


def _args(tmp_path: Path, **overrides) -> argparse.Namespace:
    values = {
        "dataset": tmp_path / "dataset",
        "environment_digest": "sha256:" + "0" * 64,
        "output": tmp_path / "out",
        "backend": "http://127.0.0.1:1",  # never reached: gates fail first
        "allow_unsafe": False,
        "limit": None,
        "case": None,
        "repetitions": 1,
        "arm": "parallel",
    }
    values.update(overrides)
    return argparse.Namespace(**values)


def _not_ready(*_args, **_kwargs) -> BatchEvalReadiness:
    return BatchEvalReadiness(
        sandbox_mode="unsafe",
        sandbox_backend="unsafe_process",
        capabilities={"bubblewrap": False, "network_isolation": False},
    )


def _git(repository: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repository), *args], check=True, capture_output=True, text=True
    ).stdout.strip()


def make_dataset_dir(tmp_path: Path) -> Path:
    source = tmp_path / "source"
    source.mkdir()
    _git(source, "init", "-q", "-b", "main")
    _git(source, "config", "user.email", "eval@example.invalid")
    _git(source, "config", "user.name", "Eval Fixture")
    (source / "app.py").write_text("VALUE = 1\n", encoding="utf-8")
    _git(source, "add", "app.py")
    _git(source, "commit", "-q", "-m", "fixture")

    dataset_root = tmp_path / "dataset"
    (dataset_root / "cases").mkdir(parents=True)
    (dataset_root / "fixtures").mkdir()
    bundle = dataset_root / "fixtures" / "bugfix-001.bundle"
    subprocess.run(
        ["git", "-C", str(source), "bundle", "create", str(bundle), "main"],
        check=True, capture_output=True,
    )
    (dataset_root / "dataset.yaml").write_text(
        "schema_version: 1\ndataset_id: gate-v1\nversion: 1.0.0\n"
        "description: gates\ncase_ids: [bugfix-001]\n",
        encoding="utf-8",
    )
    (dataset_root / "cases" / "bugfix-001.yaml").write_text(
        f"""schema_version: 1
case_id: bugfix-001
category: bugfix
difficulty: easy
fixture:
  source: fixtures/bugfix-001.bundle
  sha256: {file_digest(bundle)}
  base_ref: main
prompt: Fix VALUE.
scope:
  allowed_paths: [app.py]
  forbidden_paths: [secrets/**]
graders:
  - type: run_state
  - type: git_diff
expected:
  require_change: true
""",
        encoding="utf-8",
    )
    return dataset_root


def test_sandbox_gate_blocks_unsafe_without_flag(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(batch_module, "current_batch_eval_readiness", _not_ready)
    ok, message = _sandbox_gate(_args(Path("."), allow_unsafe=False))
    assert ok is False
    assert "--allow-unsafe" in message
    ok, mode = _sandbox_gate(_args(Path("."), allow_unsafe=True))
    assert ok is True and mode == "unsafe"


def test_sandbox_gate_passes_strict(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        batch_module,
        "current_batch_eval_readiness",
        lambda **_kwargs: BatchEvalReadiness(
            sandbox_mode="strict",
            sandbox_backend="bubblewrap",
            capabilities={"bubblewrap": True, "network_isolation": True},
        ),
    )
    ok, mode = _sandbox_gate(_args(Path("."), allow_unsafe=False))
    assert ok is True and mode == "strict"


def test_judge_gate_requires_ds_api_key(monkeypatch: pytest.MonkeyPatch) -> None:
    dataset = SimpleNamespace(manifest=SimpleNamespace(case_ids=["a", "b"]))
    monkeypatch.delenv("DS_API_KEY", raising=False)
    ok, message = _judge_gate(dataset)
    assert ok is False
    assert "DS_API_KEY" in message and "inflate" in message
    monkeypatch.setenv("DS_API_KEY", "sk-test")
    assert _judge_gate(dataset)[0] is True
    assert _judge_gate(SimpleNamespace(manifest=SimpleNamespace(case_ids=[])))[0] is True


def test_run_batch_fails_closed_without_allow_unsafe(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    dataset_root = make_dataset_dir(tmp_path)
    monkeypatch.setattr(batch_module, "current_batch_eval_readiness", _not_ready)
    monkeypatch.delenv("DS_API_KEY", raising=False)
    monkeypatch.setattr(batch_module, "load_dotenv", lambda *_a, **_k: None)
    exit_code = run_batch(_args(tmp_path, dataset=dataset_root, allow_unsafe=False))
    assert exit_code == 2
    assert not (tmp_path / "out" / "results.jsonl").exists()


def test_run_batch_fails_closed_without_judge_key(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    dataset_root = make_dataset_dir(tmp_path)
    monkeypatch.setattr(batch_module, "current_batch_eval_readiness", _not_ready)
    monkeypatch.delenv("DS_API_KEY", raising=False)
    monkeypatch.setattr(batch_module, "load_dotenv", lambda *_a, **_k: None)
    # --allow-unsafe acknowledges the sandbox, but not the missing judge:
    exit_code = run_batch(_args(tmp_path, dataset=dataset_root, allow_unsafe=True))
    assert exit_code == 2
    assert not (tmp_path / "out" / "results.jsonl").exists()


def test_collect_evidence_reads_agent_role_messages() -> None:
    from evals.batch import BackendDriver

    class StubDriver(BackendDriver):
        def __init__(self, payload: dict) -> None:  # noqa: D107
            self.payload = payload

        def request(self, method: str, path: str, payload=None, timeout: float = 30) -> dict:
            assert "messages?session_id=" in path
            return self.payload

    # Backend wraps the list as {"data": {"data": [...]}}; request() unwraps once.
    driver = StubDriver({"data": [
        {"role": "user", "content": "问题"},
        {"role": "agent", "content": "第一段过程"},
        {"role": "agent", "content": ""},
        {"role": "agent", "content": "最终回答"},
    ]})
    evidence = driver._collect_evidence("t", "s")
    assert evidence["final_text"] == "最终回答"
    assert evidence["transcript_text"] == "第一段过程\n最终回答"
    assert evidence["orchestration"]["plan_task_count"] == 0

    empty = StubDriver({"data": []})._collect_evidence("t", "s")
    assert empty["final_text"] is None and empty["transcript_text"] is None


def test_collect_evidence_strips_runtime_markers_but_counts_them() -> None:
    from evals.batch import BackendDriver

    marked = (
        "编排开始\n"
        "\ntype: plan\njson: {\"overview\": \"x\", \"tasks\": ["
        "{\"task_id\": \"p1\", \"agent\": \"claude-code\"},"
        "{\"task_id\": \"p2\", \"agent\": \"pi\"}]}\n"
        "\ntype: runtime_status\njson: {\"task_id\": \"p1\", \"attempt\": 1, \"status\": \"running\"}\n"
        "\ntype: coordination\njson: {\"ignored\": true}\n"
        "\ntype: runtime_status\njson: {\"task_id\": \"p2\", \"attempt\": 2, \"status\": \"failed\"}\n"
        "\ntype: runtime_status\njson: {\"task_id\": \"p1\", \"conflict_id\": \"c1\", \"status\": \"conflict\"}\n"
        "\ntype: runtime_status\njson: {\"task_id\": \"p1\", \"conflict_id\": \"c1\", \"status\": \"resolving\"}\n"
        "\ntype: runtime_status\njson: {\"task_id\": \"p1\", \"conflict_id\": \"c1\", \"status\": \"completed\"}\n"
        "\n最终回答正文"
    )

    class StubDriver(BackendDriver):
        def __init__(self, payload: dict) -> None:  # noqa: D107
            self.payload = payload

        def request(self, method: str, path: str, payload=None, timeout: float = 30) -> dict:
            return self.payload

    evidence = StubDriver({"data": [{"role": "agent", "content": marked}]})._collect_evidence("t", "s")
    assert "type: plan" not in (evidence["final_text"] or "")
    assert evidence["final_text"].endswith("最终回答正文")  # markers stripped, prose kept
    assert "编排开始" in evidence["final_text"]
    facts = evidence["orchestration"]
    assert facts["plan_task_ids"] == ["p1", "p2"]
    assert facts["plan_task_count"] == 2
    assert facts["dispatched_implementers"] == ["claude-code", "pi"]
    assert facts["retry_count"] == 3  # attempt max: p1 -> 1, p2 -> 2
    assert facts["conflict_count"] == 1
    assert facts["conflict_recovery_count"] == 1
    assert facts["conflict_chains"]["c1"] == ["conflict", "resolving", "completed"]
    assert facts["integration_conflict_seen"] is True
    assert facts["resolution_completed_seen"] is True


def test_orchestration_facts_empty_and_malformed() -> None:
    empty = orchestration_facts("")
    assert empty["plan_task_count"] == 0 and empty["conflict_count"] == 0
    assert empty["integration_conflict_seen"] is False
    assert empty["resolution_completed_seen"] is False
    malformed = orchestration_facts(
        "\ntype: plan\njson: not-json\n"
        "\ntype: runtime_status\njson: [1, 2]\n"
        "\ntype: runtime_status\njson: {\"status\": \"running\"}\n"
    )
    assert malformed["plan_task_count"] == 0
    assert malformed["subtask_final_status"] == {}


def test_build_instruction_routes_by_category_and_arm() -> None:
    conversation = _build_instruction("问题", "chat", "parallel")
    assert "不要派实现者" in conversation and "用户需求：问题" in conversation
    qa = _build_instruction("问题", "knowledge_qa", "parallel")
    assert "不要派实现者" in qa
    noop = _build_instruction("问题", "no_op", "serial")
    assert "不要派实现者" in noop

    coding = _build_instruction("问题", "bugfix", "parallel")
    assert "简单任务尽量只派一个实现者" in coding

    neutral = _build_instruction("问题", "orchestrator", "parallel")
    assert "按你认为合理的方式分解并执行" in neutral
    assert "一次只派一个实现者" not in neutral

    serial = _build_instruction("问题", "orchestrator", "serial")
    assert "一次只派一个实现者" in serial
    assert "禁止同时派多个实现者" in serial

    for instruction in (conversation, coding, neutral, serial):
        assert "禁止调度 Codex" in instruction
        assert "不得寻找或访问隐藏测试" in instruction


def test_completed_keys_dedupes_by_case_and_repetition(tmp_path: Path) -> None:
    results = tmp_path / "results.jsonl"
    results.write_text(
        '{"case_id": "orch-parallel-001", "repetition": 0, "status": "completed"}\n'
        '{"case_id": "orch-parallel-001", "repetition": 1, "status": "completed"}\n'
        '{"case_id": "orch-parallel-001"}\n'
        "not-json\n"
        '{"status": "infrastructure_error", "error": "x"}\n'
        '{"case_id": "chat-001", "repetition": 0}\n',
        encoding="utf-8",
    )
    assert _completed_keys(results) == {
        ("orch-parallel-001", 0),
        ("orch-parallel-001", 1),
        ("orch-parallel-001", 0),  # repetition defaults to 0 -> same key
        ("chat-001", 0),
    }
    assert _completed_keys(tmp_path / "missing.jsonl") == set()
    # _completed_case_ids keeps listing every occurrence (legacy consumers).
    assert _completed_case_ids(results) == [
        "orch-parallel-001", "orch-parallel-001", "orch-parallel-001", "chat-001",
    ]
