#!/usr/bin/env python3
"""Positive controls for the user-operation contract guard.

The clean fixture is checked green first.  Each mutant then proves a different
load-bearing direction of the guard:

* narrowing a sentence without changing its owner ruling is red, so a seemingly
  harmless narrowing cannot silently reduce the protected behavior;
* a row claiming two screens with one named assertion is red, so scope cannot be
  inferred by a reader;
* the reverse ReplyCardBody caller enumeration is derived from the fixture tree:
  removing one known caller makes the reported count drop, and omitting its
  mapping is red; adding a fourth caller without a mapping is red too;
* an orphan named assertion is red, so adding an assertion does not silently
  create an undocumented contract;
* deleting a row without a ruling record is red, so deletion is not a bypass.

Every mutation is applied from the in-memory clean snapshot, not restored with a
checkout command.  The cases that exercise the diff rule use a real temporary
git baseline and a real working-tree diff.
"""

from __future__ import annotations

import os
import json
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Callable, Tuple


ROOT = Path(__file__).resolve().parents[2]
GUARD = ROOT / "bin/user-operation-contract-guard.py"
CONTRACT_REL = "docs/design/user-operation-contracts.md"
SPEC_REL = "e2e_test/tests/uoc_contract_fixture.spec.js"
SURFACE_MANIFEST_REL = "docs/design/user-operation-contract-surfaces.json"
UNCLAIMED_SURFACE_REL = "frontend/src/components/FourthReplyCard.tsx"

SURFACE_ROWS = [
    {
        "source": "frontend/src/components/ChatReplyCard.tsx",
        "screen": "chat-page",
    },
    {
        "source": "frontend/src/components/RepliesPage.tsx",
        "screen": "replies-page",
    },
    {
        "source": "frontend/src/components/TaskReplyCard.tsx",
        "screen": "tasks-page",
    },
]

CLEAN_CONTRACT = """# fixture

<!-- user-operation-contract: id=UOC-TEST-TWO-SCREENS scope=replies-page,chat-page,tasks-page surface_set=reply-card-body-callers ruling=rc-test-one evidence=fixture:1 -->
- sentence: 點一下選項就完成回答。
- assertion: screen=replies-page marker=test_replies_one_tap
- assertion: screen=chat-page marker=test_chat_one_tap
- assertion: screen=tasks-page marker=test_tasks_one_tap
<!-- /user-operation-contract -->

<!-- user-operation-contract: id=UOC-TEST-SECOND scope=replies-page,chat-page,tasks-page surface_set=reply-card-body-callers ruling=rc-test-two evidence=fixture:2 -->
- sentence: 打字後送出會保留文字。
- assertion: screen=replies-page marker=test_replies_draft
- assertion: screen=chat-page marker=test_chat_draft
- assertion: screen=tasks-page marker=test_tasks_draft
<!-- /user-operation-contract -->
"""

CLEAN_SPEC = """const { test, expect } = require('@playwright/test');

test('fixture assertions', async ({ page }) => {
  // UOC_ASSERT id=UOC-TEST-TWO-SCREENS screen=replies-page name=test_replies_one_tap
  await expect(page, 'replies one tap').toHaveURL('/');
  // UOC_ASSERT id=UOC-TEST-TWO-SCREENS screen=chat-page name=test_chat_one_tap
  await expect(page, 'chat one tap').toHaveURL('/');
  // UOC_ASSERT id=UOC-TEST-TWO-SCREENS screen=tasks-page name=test_tasks_one_tap
  await expect(page, 'tasks one tap').toHaveURL('/');
  // UOC_ASSERT id=UOC-TEST-SECOND screen=replies-page name=test_replies_draft
  await expect(page, 'draft is kept').toHaveURL('/');
  // UOC_ASSERT id=UOC-TEST-SECOND screen=chat-page name=test_chat_draft
  await expect(page, 'chat draft is kept').toHaveURL('/');
  // UOC_ASSERT id=UOC-TEST-SECOND screen=tasks-page name=test_tasks_draft
  await expect(page, 'tasks draft is kept').toHaveURL('/');
});
"""

SURFACE_SOURCE = "import { ReplyCardWaitingBody } from \"./ReplyCardBody\";\n"


def git(root: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", "-C", str(root), *args],
        text=True,
        capture_output=True,
        check=True,
    )
    return result.stdout.strip()


def commit(root: Path, message: str) -> str:
    subprocess.run(["git", "-C", str(root), "add", "."], check=True)
    subprocess.run(
        ["git", "-C", str(root), "commit", "-m", message],
        check=True,
        stdout=subprocess.DEVNULL,
    )
    return git(root, "rev-parse", "HEAD")


def stage_fixture(tmp: Path) -> Tuple[Path, str]:
    root = tmp / "tree"
    (root / "docs/design").mkdir(parents=True)
    (root / "e2e_test/tests").mkdir(parents=True)
    (root / "frontend/src/components").mkdir(parents=True)
    (root / CONTRACT_REL).write_text(CLEAN_CONTRACT, encoding="utf-8")
    (root / SPEC_REL).write_text(CLEAN_SPEC, encoding="utf-8")
    (root / SURFACE_MANIFEST_REL).write_text(
        json.dumps({"surfaces": SURFACE_ROWS}, indent=2) + "\n",
        encoding="utf-8",
    )
    for row in SURFACE_ROWS:
        (root / row["source"]).write_text(SURFACE_SOURCE, encoding="utf-8")
    subprocess.run(["git", "init", "-q", "-b", "main", str(root)], check=True)
    subprocess.run(["git", "-C", str(root), "config", "user.name", "selftest"], check=True)
    subprocess.run(
        ["git", "-C", str(root), "config", "user.email", "selftest@example.invalid"],
        check=True,
    )
    return root, commit(root, "clean fixture")


def run_guard(root: Path, base: str) -> Tuple[int, str]:
    env = dict(os.environ)
    env.pop("GITHUB_BASE_SHA", None)
    env.pop("GITHUB_EVENT_PATH", None)
    env["OC_USER_OPERATION_CONTRACT_ROOT"] = str(root)
    env["OC_UOC_BASE_SHA"] = base
    result = subprocess.run(
        [sys.executable, str(GUARD)],
        text=True,
        capture_output=True,
        env=env,
        check=False,
    )
    return result.returncode, result.stdout + result.stderr


def require_green(root: Path, base: str, label: str) -> None:
    rc, output = run_guard(root, base)
    if rc != 0:
        raise AssertionError(f"{label}: clean guard was not green:\n{output}")


def require_red(root: Path, base: str, label: str, *needles: str) -> None:
    rc, output = run_guard(root, base)
    if rc == 0:
        raise AssertionError(f"{label}: mutant was not caught:\n{output}")
    for needle in needles:
        if needle not in output:
            raise AssertionError(
                f"{label}: failure did not name {needle!r}:\n{output}"
            )


def reset_fixture(root: Path) -> None:
    (root / CONTRACT_REL).write_text(CLEAN_CONTRACT, encoding="utf-8")
    (root / SPEC_REL).write_text(CLEAN_SPEC, encoding="utf-8")
    (root / SURFACE_MANIFEST_REL).write_text(
        json.dumps({"surfaces": SURFACE_ROWS}, indent=2) + "\n",
        encoding="utf-8",
    )
    for row in SURFACE_ROWS:
        (root / row["source"]).write_text(SURFACE_SOURCE, encoding="utf-8")
    unclaimed = root / UNCLAIMED_SURFACE_REL
    if unclaimed.exists():
        unclaimed.unlink()


def mutate_narrow(root: Path) -> None:
    old = "點一下選項就完成回答。"
    new = "在桌面寬版請示列表頁點一下選項就完成回答。"
    path = root / CONTRACT_REL
    text = path.read_text(encoding="utf-8")
    if old not in text:
        raise AssertionError("narrowing anchor disappeared")
    path.write_text(text.replace(old, new, 1), encoding="utf-8")


def mutate_missing_screen_assertion(root: Path) -> None:
    path = root / CONTRACT_REL
    text = path.read_text(encoding="utf-8")
    old = "- assertion: screen=chat-page marker=test_chat_one_tap\n"
    if old not in text:
        raise AssertionError("screen assertion anchor disappeared")
    path.write_text(text.replace(old, "", 1), encoding="utf-8")


def mutate_surface_import(root: Path) -> None:
    path = root / SURFACE_ROWS[0]["source"]
    text = path.read_text(encoding="utf-8")
    if SURFACE_SOURCE not in text:
        raise AssertionError("surface import anchor disappeared")
    path.write_text(text.replace(SURFACE_SOURCE, "", 1), encoding="utf-8")


def mutate_surface_manifest(root: Path) -> None:
    path = root / SURFACE_MANIFEST_REL
    payload = json.loads(path.read_text(encoding="utf-8"))
    rows = payload["surfaces"]
    if len(rows) != len(SURFACE_ROWS):
        raise AssertionError("surface manifest fixture shape changed")
    payload["surfaces"] = rows[1:]
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


def mutate_new_surface(root: Path) -> None:
    path = root / UNCLAIMED_SURFACE_REL
    path.write_text(SURFACE_SOURCE, encoding="utf-8")


def mutate_orphan_marker(root: Path) -> None:
    path = root / SPEC_REL
    text = path.read_text(encoding="utf-8")
    old = "  await expect(page, 'draft is kept').toHaveURL('/');\n"
    new = old + (
        "  // UOC_ASSERT id=UOC-TEST-SECOND screen=settings-page "
        "name=test_settings_orphan\n"
        "  await expect(page, 'orphan assertion').toHaveURL('/');\n"
    )
    if old not in text:
        raise AssertionError("orphan marker anchor disappeared")
    path.write_text(text.replace(old, new, 1), encoding="utf-8")


def mutate_delete_entry(root: Path) -> None:
    path = root / CONTRACT_REL
    text = path.read_text(encoding="utf-8")
    start = "<!-- user-operation-contract: id=UOC-TEST-TWO-SCREENS"
    start_index = text.index(start)
    end_index = text.index("<!-- /user-operation-contract -->", start_index)
    end_index = text.index("\n", end_index) + 1
    path.write_text(text[:start_index] + text[end_index:], encoding="utf-8")
    spec_path = root / SPEC_REL
    spec = spec_path.read_text(encoding="utf-8")
    for marker in (
        "  // UOC_ASSERT id=UOC-TEST-TWO-SCREENS screen=replies-page name=test_replies_one_tap\n",
        "  // UOC_ASSERT id=UOC-TEST-TWO-SCREENS screen=chat-page name=test_chat_one_tap\n",
        "  // UOC_ASSERT id=UOC-TEST-TWO-SCREENS screen=tasks-page name=test_tasks_one_tap\n",
    ):
        if marker not in spec:
            raise AssertionError("deleted-entry marker anchor disappeared")
        spec = spec.replace(marker, "", 1)
    spec_path.write_text(spec, encoding="utf-8")


def case_narrowing(root: Path, clean_sha: str) -> None:
    reset_fixture(root)
    mutate_narrow(root)
    require_red(
        root,
        clean_sha,
        "narrowing",
        "UOC-TEST-TWO-SCREENS",
        "owner ruling source",
        "Next action",
    )


def case_missing_screen(root: Path, clean_sha: str) -> None:
    reset_fixture(root)
    mutate_missing_screen_assertion(root)
    mutant_sha = commit(root, "mutant: claim two screens with one assertion")
    require_red(
        root,
        mutant_sha,
        "missing screen assertion",
        "scope claims screen(s) chat-page",
        "UOC-TEST-TWO-SCREENS",
    )


def case_surface_import_removed(root: Path, clean_sha: str) -> None:
    reset_fixture(root)
    mutate_surface_import(root)
    require_red(
        root,
        clean_sha,
        "reverse surface enumeration",
        "discovered 2 production ReplyCardBody caller(s)",
        "no longer import ReplyCardBody",
    )


def case_surface_manifest_omits_caller(root: Path, clean_sha: str) -> None:
    reset_fixture(root)
    mutate_surface_manifest(root)
    require_red(
        root,
        clean_sha,
        "surface manifest omission",
        "discovered 3 production ReplyCardBody caller(s)",
        "unclaimed production ReplyCardBody caller(s)",
    )


def case_new_unclaimed_surface(root: Path, clean_sha: str) -> None:
    reset_fixture(root)
    mutate_new_surface(root)
    require_red(
        root,
        clean_sha,
        "new unclaimed surface",
        "discovered 4 production ReplyCardBody caller(s)",
        "unclaimed production ReplyCardBody caller(s)",
    )


def case_orphan_marker(root: Path, clean_sha: str) -> None:
    reset_fixture(root)
    mutate_orphan_marker(root)
    mutant_sha = commit(root, "mutant: add undocumented assertion")
    require_red(root, mutant_sha, "orphan marker", "test_settings_orphan", "next action")


def case_delete(root: Path, clean_sha: str) -> None:
    reset_fixture(root)
    mutate_delete_entry(root)
    require_red(
        root,
        clean_sha,
        "deleted entry",
        "UOC-TEST-TWO-SCREENS",
        "removed without an owner ruling source",
    )


def main() -> int:
    with tempfile.TemporaryDirectory(prefix="oc-uoc-selftest-") as raw:
        root, clean_sha = stage_fixture(Path(raw))
        require_green(root, clean_sha, "initial fixture")
        case_narrowing(root, clean_sha)
        case_missing_screen(root, clean_sha)
        case_surface_import_removed(root, clean_sha)
        case_surface_manifest_omits_caller(root, clean_sha)
        case_new_unclaimed_surface(root, clean_sha)
        case_orphan_marker(root, clean_sha)
        case_delete(root, clean_sha)
        reset_fixture(root)
        restored_sha = commit(root, "restore clean fixture")
        require_green(root, restored_sha, "restored fixture")
    print(
        "[user-operation-contract-guard-selftest] OK — clean, narrowing, "
        "multi-screen coverage, reverse surface enumeration, orphan marker and "
        "deletion and fourth-surface mutants all behaved"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
