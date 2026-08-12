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
    recover_skill_staging_for_workspace,
    _safe_zip_entry_name,
    _skill_directory_has_symlink,
)
from src.skills.provisioner import _atomic_refresh_skill, _ensure_safe_skill_target, _skill_matches_manifest


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


def test_extract_skill_zip_rejects_unicode_casefold_collisions(tmp_path: Path):
    error = _extract_skill_zip(
        _zip([("ß.txt", b"one"), ("SS.txt", b"two")]),
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


def test_startup_recovery_restores_recent_atomic_backup(tmp_path: Path):
    skills_dir = tmp_path / ".claude" / "skills"
    skills_dir.mkdir(parents=True)
    backup = skills_dir / ".demo.previous-crash"
    backup.mkdir()
    (backup / "SKILL.md").write_text("---\nname: demo\n---\n")
    install = skills_dir / ".demo.install-crash"
    install.mkdir()
    (install / "partial.txt").write_text("discarded")

    # The artifacts are intentionally recent; startup recovery must not wait
    # for the periodic 24-hour stale-artifact threshold.
    recover_skill_staging_for_workspace(str(tmp_path), "claude-code")

    assert (skills_dir / "demo" / "SKILL.md").is_file()
    assert not backup.exists()
    assert not install.exists()


def test_startup_recovery_preserves_skill_names_with_backup_marker(tmp_path: Path):
    skills_dir = tmp_path / ".claude" / "skills"
    skills_dir.mkdir(parents=True)
    backup = skills_dir / ".demo.previous-stable.previous-crash"
    backup.mkdir()
    (backup / "SKILL.md").write_text("---\nname: demo.previous-stable\n---\n")

    recover_skill_staging_for_workspace(str(tmp_path), "claude-code")

    assert (skills_dir / "demo.previous-stable" / "SKILL.md").is_file()
    assert not backup.exists()


def test_skill_directory_rejects_symlink_root(tmp_path: Path):
    target = tmp_path / "target"
    target.mkdir()
    link = tmp_path / ".claude"
    try:
        link.symlink_to(target, target_is_directory=True)
    except OSError:
        return
    assert _skill_directory_has_symlink(link / "skills")


def test_provisioner_does_not_treat_managed_symlink_as_current(tmp_path: Path):
    source = tmp_path / "source"
    dest = tmp_path / "dest"
    source.mkdir()
    dest.mkdir()
    (source / "render").write_text("new-binary")
    external = tmp_path / "external"
    external.write_text("new-binary")
    try:
        (dest / "render").symlink_to(external)
    except OSError:
        return

    assert not _skill_matches_manifest(dest, source, {"file": ["render"]})


def test_provisioner_does_not_follow_managed_file_parent_symlink(tmp_path: Path):
    source = tmp_path / "source"
    dest = tmp_path / "dest"
    source.mkdir()
    dest.mkdir()
    (source / "scripts").mkdir()
    (source / "scripts" / "run.sh").write_text("safe")
    external = tmp_path / "external"
    external.mkdir()
    (external / "run.sh").write_text("safe")
    try:
        (dest / "scripts").symlink_to(external, target_is_directory=True)
    except OSError:
        return

    assert not _skill_matches_manifest(dest, source, {"file": ["scripts/run.sh"]})


def test_provisioner_refresh_replaces_managed_symlink_and_preserves_unmanaged(tmp_path: Path):
    source = tmp_path / "source"
    dest = tmp_path / "dest"
    source.mkdir()
    dest.mkdir()
    (source / "render").write_text("new-binary")
    (dest / "notes.txt").write_text("keep me")
    external = tmp_path / "external"
    external.write_text("old-binary")
    try:
        (dest / "render").symlink_to(external)
    except OSError:
        return

    _atomic_refresh_skill(dest, source, {"file": ["render"]})

    assert (dest / "render").read_text() == "new-binary"
    assert not (dest / "render").is_symlink()
    assert (dest / "notes.txt").read_text() == "keep me"


def test_provisioner_refreshes_managed_directory_with_nested_symlink(tmp_path: Path):
    source = tmp_path / "source"
    dest = tmp_path / "dest"
    source.mkdir()
    dest.mkdir()
    (source / "references").mkdir()
    (source / "references" / "guide.md").write_text("safe")
    (dest / "references").mkdir()
    external = tmp_path / "external-guide.md"
    external.write_text("unsafe")
    try:
        (dest / "references" / "guide.md").symlink_to(external)
    except OSError:
        return

    _atomic_refresh_skill(dest, source, {"dir": ["references"]})

    assert (dest / "references" / "guide.md").read_text() == "safe"
    assert not (dest / "references" / "guide.md").is_symlink()


def test_provisioner_refresh_preserves_unmanaged_files_inside_managed_directory(tmp_path: Path):
    source = tmp_path / "source"
    dest = tmp_path / "dest"
    source.mkdir()
    dest.mkdir()
    (source / "references").mkdir()
    (source / "references" / "guide.md").write_text("new guide")
    (dest / "references").mkdir()
    (dest / "references" / "guide.md").write_text("old guide")
    (dest / "references" / "local-note.md").write_text("keep me")

    _atomic_refresh_skill(dest, source, {"dir": ["references"]})

    assert (dest / "references" / "guide.md").read_text() == "new guide"
    assert (dest / "references" / "local-note.md").read_text() == "keep me"


def test_provisioner_rejects_symlinked_skill_parent(tmp_path: Path):
    external = tmp_path / "external"
    external.mkdir()
    link = tmp_path / ".claude"
    try:
        link.symlink_to(external, target_is_directory=True)
    except OSError:
        return

    try:
        _ensure_safe_skill_target(tmp_path, link / "skills")
    except ValueError as error:
        assert "symlink" in str(error)
    else:
        raise AssertionError("symlinked skill parent was accepted")


def test_provisioner_rejects_existing_symlinked_workspace_ancestor(tmp_path: Path):
    real_root = tmp_path / "real-root"
    real_root.mkdir()
    linked_root = tmp_path / "linked-root"
    try:
        linked_root.symlink_to(real_root, target_is_directory=True)
    except OSError:
        return

    worktree = linked_root / "worktree"
    worktree.mkdir()
    try:
        _ensure_safe_skill_target(worktree, worktree / "skills")
    except ValueError as error:
        assert "symlink" in str(error)
    else:
        raise AssertionError("symlinked workspace ancestor was accepted")


def test_provisioner_creates_missing_workspace_root_safely(tmp_path: Path):
    worktree = tmp_path / "new-worktree" / "shared" / ".agent"
    target = worktree / ".orchestrator" / "skills"

    _ensure_safe_skill_target(worktree, target)

    assert target.is_dir()


def test_provisioner_rejects_skill_target_traversal(tmp_path: Path):
    worktree = tmp_path / "worktree"
    escaped = worktree / ".." / "outside" / "skills"

    try:
        _ensure_safe_skill_target(worktree, escaped)
    except ValueError as error:
        assert "unsafe" in str(error)
    else:
        raise AssertionError("skill target traversal was accepted")
