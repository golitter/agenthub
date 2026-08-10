from io import BytesIO
import os
from pathlib import Path
import sys
import time
import zipfile
import warnings

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.api.v1.skills import (
    _AGENT_CONFIG_DIRS,
    _EXTERNAL_SKILL_AGENT_TYPES,
    _parse_skill_md,
    _cleanup_stale_skill_staging,
    _extract_skill_zip,
    _safe_zip_entry_name,
    _skill_directory_has_symlink,
)


def _zip(entries: list[tuple[str, bytes]]) -> bytes:
    buf = BytesIO()
    with zipfile.ZipFile(buf, "w") as archive:
        for name, data in entries:
            archive.writestr(name, data)
    return buf.getvalue()


def test_safe_zip_entry_rejects_traversal_and_windows_paths():
    assert _safe_zip_entry_name("../SKILL.md")[1]
    assert _safe_zip_entry_name("C:/tmp/SKILL.md")[1]
    assert _safe_zip_entry_name("skill\\SKILL.md")[1]
    assert _safe_zip_entry_name("skill/\x00SKILL.md")[1]
    assert _safe_zip_entry_name("skill//SKILL.md")[1]
    assert _safe_zip_entry_name("skill/./SKILL.md")[1]
    assert _safe_zip_entry_name("skill/../SKILL.md")[1]
    assert _safe_zip_entry_name("skill//")[1]
    assert _safe_zip_entry_name(" SKILL.md")[1]
    assert _safe_zip_entry_name("SKILL.md ")[1]


def test_extract_skill_zip_rejects_duplicate_entries(tmp_path: Path):
    with warnings.catch_warnings():
        warnings.simplefilter("ignore", UserWarning)
        error = _extract_skill_zip(
            _zip([("SKILL.md", b"---\nname: demo\n---\n"), ("SKILL.md", b"duplicate")]),
            tmp_path,
        )
    assert error and "duplicate" in error


def test_extract_skill_zip_writes_valid_package(tmp_path: Path):
    error = _extract_skill_zip(
        _zip([("SKILL.md", b"---\nname: demo\n---\n"), ("references/readme.md", b"ok")]),
        tmp_path,
    )
    assert error is None
    assert (tmp_path / "SKILL.md").is_file()
    assert (tmp_path / "references/readme.md").read_text() == "ok"


def test_extract_skill_zip_normalizes_explicit_execute_bit(tmp_path: Path):
    archive = BytesIO()
    with zipfile.ZipFile(archive, "w") as zf:
        zf.writestr("SKILL.md", "---\nname: demo\n---\n")
        header = zipfile.ZipInfo("run.sh")
        header.external_attr = (0o100755 << 16)
        zf.writestr(header, "#!/bin/sh\necho ok\n")
    assert _extract_skill_zip(archive.getvalue(), tmp_path) is None
    assert (tmp_path / "run.sh").stat().st_mode & 0o777 == 0o755


def test_extract_skill_zip_accepts_single_top_level_skill_directory(tmp_path: Path):
    error = _extract_skill_zip(
        _zip(
            [
                ("demo/SKILL.md", b"---\nname: demo\n---\n"),
                ("demo/references/readme.md", b"ok"),
            ]
        ),
        tmp_path,
    )
    assert error is None
    assert (tmp_path / "demo" / "SKILL.md").is_file()


def test_extract_skill_zip_rejects_mismatched_top_level_directory(tmp_path: Path):
    error = _extract_skill_zip(
        _zip([("other/SKILL.md", b"---\nname: demo\n---\n")]),
        tmp_path,
    )
    assert error and "top-level" in error


def test_extract_skill_zip_rejects_unsafe_skill_name(tmp_path: Path):
    error = _extract_skill_zip(
        _zip([("SKILL.md", b"---\nname: ../demo\n---\n")]),
        tmp_path,
    )
    assert error


def test_extract_skill_zip_parses_yaml_comments_and_quoted_values(tmp_path: Path):
    error = _extract_skill_zip(
        _zip(
            [
                (
                    "SKILL.md",
                    b"---\nname: 'demo' # stable name\ndescription: \"review source\"\n---\n",
                )
            ]
        ),
        tmp_path,
    )
    assert error is None


def test_extract_skill_zip_rejects_non_string_yaml_name(tmp_path: Path):
    error = _extract_skill_zip(_zip([("SKILL.md", b"---\nname: 123\n---\n")]), tmp_path)
    assert error and "frontmatter" in error


def test_parse_skill_md_rejects_invalid_utf8(tmp_path: Path):
    skill_md = tmp_path / "SKILL.md"
    skill_md.write_bytes(b"---\nname: demo\n---\n\xff")
    assert _parse_skill_md(skill_md) is None


def test_skill_agent_type_boundaries_do_not_default_to_claude_directory():
    assert _AGENT_CONFIG_DIRS["orchestrator"] == ".orchestrator"
    assert "orchestrator" not in _EXTERNAL_SKILL_AGENT_TYPES
    assert "unknown" not in _AGENT_CONFIG_DIRS


def test_extract_skill_zip_rejects_malformed_frontmatter_closing_marker(tmp_path: Path):
    error = _extract_skill_zip(
        _zip([("SKILL.md", b"---\nname: demo\n---not-a-delimiter\n")]),
        tmp_path,
    )
    assert error


def test_extract_skill_zip_accepts_root_skill_with_content_directory(tmp_path: Path):
    error = _extract_skill_zip(
        _zip(
            [
                ("SKILL.md", b"---\nname: demo\n---\n"),
                ("other/readme.md", b"ambiguous"),
            ]
        ),
        tmp_path,
    )
    assert error is None


def test_cleanup_stale_skill_staging_does_not_follow_links(tmp_path: Path):
    stale = tmp_path / ".demo.previous-old"
    stale.mkdir()
    (stale / "old.txt").write_text("old")
    old = time.time() - 2 * 24 * 60 * 60
    stale.touch()
    os.utime(stale, (old, old))
    link_target = tmp_path / "target"
    link_target.mkdir()
    link = tmp_path / ".demo.remove-link"
    try:
        link.symlink_to(link_target, target_is_directory=True)
    except OSError:
        link = None

    _cleanup_stale_skill_staging(tmp_path)
    assert not stale.exists()
    assert (tmp_path / "demo" / "old.txt").read_text() == "old"
    if link is not None:
        assert link.is_symlink()
        assert link_target.exists()


def test_skill_directory_rejects_symlink_root(tmp_path: Path):
    target = tmp_path / "target"
    target.mkdir()
    link = tmp_path / ".claude"
    try:
        link.symlink_to(target, target_is_directory=True)
    except OSError:
        return
    assert _skill_directory_has_symlink(link / "skills")
