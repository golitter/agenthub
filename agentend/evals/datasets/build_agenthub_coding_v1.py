from __future__ import annotations

import hashlib
import shutil
import subprocess
import tempfile
from dataclasses import dataclass
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent / "agenthub-coding-v1"


@dataclass(frozen=True)
class CaseDefinition:
    case_id: str
    category: str
    difficulty: str
    prompt: str
    initial_status: str
    expected_status: str
    require_change: bool = True


CASES = [
    *[
        CaseDefinition(f"bugfix-core-{index:03d}", "bugfix", "medium", f"修复编号 {index} 的行为缺陷。", "BROKEN", "FIXED")
        for index in range(1, 9)
    ],
    *[
        CaseDefinition(f"feature-core-{index:03d}", "feature", "medium", f"实现编号 {index} 的缺失功能。", "MISSING", "IMPLEMENTED")
        for index in range(1, 7)
    ],
    *[
        CaseDefinition(f"refactor-core-{index:03d}", "refactor", "medium", f"重构编号 {index} 的实现并保持行为。", "STABLE", "STABLE")
        for index in range(1, 5)
    ],
    *[
        CaseDefinition(f"testgen-core-{index:03d}", "test_generation", "easy", f"为编号 {index} 的边界行为补充测试。", "TESTABLE", "TESTABLE")
        for index in range(1, 5)
    ],
    *[
        CaseDefinition(f"integration-core-{index:03d}", "integration", "hard", f"完成编号 {index} 的多文件集成修改。", "PARTIAL", "INTEGRATED")
        for index in range(1, 5)
    ],
    *[
        CaseDefinition(
            f"noop-core-{index:03d}",
            "no_op",
            "easy",
            f"核查编号 {index} 的问题；若前提不成立，不要修改仓库。",
            "CORRECT",
            "CORRECT",
            require_change=False,
        )
        for index in range(1, 5)
    ],
]


def run(*argv: str, cwd: Path) -> str:
    return subprocess.run(argv, cwd=cwd, capture_output=True, check=True, text=True).stdout.strip()


def sha256(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def build() -> None:
    if len(CASES) != 30:
        raise RuntimeError("core dataset must contain exactly 30 cases")
    (ROOT / "cases").mkdir(parents=True, exist_ok=True)
    (ROOT / "fixtures").mkdir(parents=True, exist_ok=True)
    (ROOT / "hidden").mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="agenthub-dataset-build-") as temporary:
        build_root = Path(temporary)
        for definition in CASES:
            _build_case(build_root, definition)
    dataset = {
        "schema_version": 1,
        "dataset_id": "agenthub-coding-v1",
        "version": "1.0.0",
        "description": "AgentHub Coding Agent 30-case core regression dataset",
        "case_ids": [definition.case_id for definition in CASES],
        "defaults": {"wall_time_seconds": 600, "grader_time_seconds": 180},
    }
    (ROOT / "dataset.yaml").write_text(
        yaml.safe_dump(dataset, allow_unicode=True, sort_keys=False), encoding="utf-8"
    )


def _build_case(build_root: Path, definition: CaseDefinition) -> None:
    repository = build_root / definition.case_id
    repository.mkdir()
    run("git", "init", "-q", "-b", "main", cwd=repository)
    run("git", "config", "user.email", "eval-fixture@agenthub.invalid", cwd=repository)
    run("git", "config", "user.name", "AgentHub Eval Fixture", cwd=repository)
    (repository / "solution.py").write_text(
        f'TASK_ID = "{definition.case_id}"\nSTATUS = "{definition.initial_status}"\n',
        encoding="utf-8",
    )
    (repository / "public_check.py").write_text(
        "from solution import TASK_ID\nassert TASK_ID\nprint('public check passed')\n",
        encoding="utf-8",
    )
    (repository / "README.md").write_text(
        f"# {definition.case_id}\n\nThis repository is an isolated evaluation fixture.\n",
        encoding="utf-8",
    )
    run("git", "add", ".", cwd=repository)
    run("git", "commit", "-q", "-m", "evaluation fixture", cwd=repository)
    bundle = ROOT / "fixtures" / f"{definition.case_id}.bundle"
    run("git", "bundle", "create", str(bundle), "main", cwd=repository)

    hidden = ROOT / "hidden" / f"{definition.case_id}-behavior-v1"
    if hidden.exists():
        shutil.rmtree(hidden)
    hidden.mkdir(parents=True)
    (hidden / "check.py").write_text(
        "from pathlib import Path\n"
        "scope = {}\n"
        "exec(Path('/workspace/solution.py').read_text(encoding='utf-8'), scope)\n"
        f"assert scope['TASK_ID'] == {definition.case_id!r}\n"
        f"assert scope['STATUS'] == {definition.expected_status!r}\n"
        "print('1 passed')\n",
        encoding="utf-8",
    )
    graders = [
        {"type": "run_state"},
        {"type": "git_diff"},
        {"type": "anti_gaming"},
        {"type": "command", "argv": ["python3", "public_check.py"]},
        {
            "type": "hidden_command",
            "asset_id": f"{definition.case_id}-behavior-v1",
            "argv": ["python3", "/eval-hidden/check.py"],
        },
        {"type": "regression", "argv": ["python3", "public_check.py"]},
    ]
    if definition.category == "no_op":
        graders.insert(2, {"type": "no_op"})
    case = {
        "schema_version": 1,
        "case_id": definition.case_id,
        "category": definition.category,
        "difficulty": definition.difficulty,
        "owner": "agenthub-eval",
        "fixture": {
            "source": f"fixtures/{definition.case_id}.bundle",
            "sha256": sha256(bundle),
            "base_ref": "main",
        },
        "prompt": definition.prompt,
        "execution": {"timeout_seconds": 600, "max_turns": 20, "network": "none"},
        "scope": {
            "allowed_paths": ["solution.py", "tests/**"] if definition.require_change else ["**"],
            "forbidden_paths": [".git/**", "agentend/evals/hidden/**"],
        },
        "baseline": [
            {"type": "command", "argv": ["python3", "public_check.py"]},
            {
                "type": "hidden_command",
                "asset_id": f"{definition.case_id}-behavior-v1",
                "argv": ["python3", "/eval-hidden/check.py"],
                "expect": "passed" if definition.initial_status == definition.expected_status else "failed",
            },
        ],
        "graders": graders,
        "expected": {
            "require_change": definition.require_change,
            "require_commit": definition.require_change,
            "max_changed_files": 3,
        },
    }
    (ROOT / "cases" / f"{definition.case_id}.yaml").write_text(
        yaml.safe_dump(case, allow_unicode=True, sort_keys=False), encoding="utf-8"
    )


if __name__ == "__main__":
    build()
