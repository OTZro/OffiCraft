#!/usr/bin/env python3
"""Guard the user-operation contract list and its named browser assertions.

The contract is deliberately checked in two directions:

* every listed screen must have a named e2e assertion, and every assertion marker
  must be listed, so a row cannot quietly claim more screens than it tests;
* every literal change to an existing contract block must change its owner
  `ruling=` value to a different value.  A narrowing is a change too, even when
  the new sentence is still true; a new block must add its ruling metadata.

The reply-card scope is also checked backwards from production code: every
production caller importing the shared ReplyCardBody must appear in the surface
enumeration input, and every contract scope must equal that discovered set.  The
enumerator prints its derived count before validating the mapping, so a narrowed
scan cannot hide behind a fixed number.

The baseline is resolved from the explicit selftest override or the
merge-base of HEAD and origin/main.  Every committed contract change in the
entire baseline..HEAD range is checked, not only HEAD^; the working tree is
checked separately so an uncommitted change is protected during local
development too.
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional, Sequence, Set, Tuple

sys.path.insert(0, str(Path(__file__).resolve().parent))
from reply_card_surface_guard import (  # noqa: E402
    SURFACE_SET,
    Surface,
    SurfaceEnumerationError,
    enumerate_and_report,
)


ROOT = Path(
    os.environ.get(
        "OC_USER_OPERATION_CONTRACT_ROOT",
        str(Path(__file__).resolve().parents[1]),
    )
).resolve()
CONTRACT_REL = os.environ.get(
    "OC_USER_OPERATION_CONTRACT_FILE", "docs/design/user-operation-contracts.md"
)
CONTRACT_PATH = ROOT / CONTRACT_REL

START_RE = re.compile(r"^<!-- user-operation-contract: (.+) -->$")
END_LINE = "<!-- /user-operation-contract -->"
RETIRED_RE = re.compile(
    r"^<!-- user-operation-contract-ruling: id=(\S+) ruling=(\S+) -->$"
)
ASSERTION_RE = re.compile(
    r"^\s*- assertion: screen=(\S+) marker=([A-Za-z0-9][A-Za-z0-9_-]*)\s*$"
)
SENTENCE_RE = re.compile(r"^\s*- sentence: (\S.*)$")
MARKER_RE = re.compile(
    r"^\s*// UOC_ASSERT id=(\S+) screen=(\S+) name=([A-Za-z0-9][A-Za-z0-9_-]*)\s*$"
)
ID_RE = re.compile(r"^UOC-[A-Z0-9][A-Z0-9_-]+$")
SCREEN_RE = re.compile(r"^[a-z][a-z0-9-]*$")
RULING_RE = re.compile(r"^rc-[A-Za-z0-9][A-Za-z0-9_-]*$")
META_TOKEN_RE = re.compile(r"^[a-z_]+=[^\s]+$")


class GuardError(Exception):
    """A user-facing, actionable guard failure."""


@dataclass(frozen=True)
class AssertionSpec:
    screen: str
    marker: str
    line: int


@dataclass(frozen=True)
class ContractBlock:
    contract_id: str
    scope: Tuple[str, ...]
    ruling: str
    evidence: str
    surface_set: str
    sentence: str
    assertions: Tuple[AssertionSpec, ...]
    start_line: int
    metadata_line: str
    raw: Tuple[str, ...]


@dataclass(frozen=True)
class Marker:
    contract_id: str
    screen: str
    name: str
    path: str
    line: int


def git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", "-C", str(ROOT), *args],
        text=True,
        capture_output=True,
        check=False,
    )


def parse_metadata(
    payload: str,
    line: int,
    source: str,
    allow_legacy_surface_set: bool = False,
) -> Dict[str, str]:
    tokens = payload.split()
    if not tokens or any(not META_TOKEN_RE.fullmatch(token) for token in tokens):
        raise GuardError(
            f"{source}:{line}: malformed contract metadata; next action: use "
            "id= scope= ruling= evidence= key=value fields with no spaces"
        )
    values: Dict[str, str] = {}
    for token in tokens:
        key, value = token.split("=", 1)
        if key in values:
            raise GuardError(
                f"{source}:{line}: duplicate metadata field {key}; next action: "
                "keep exactly one value for each contract field"
            )
        values[key] = value
    required = {"id", "scope", "ruling", "evidence", "surface_set"}
    missing = sorted(required - set(values))
    extra = sorted(set(values) - required)
    if allow_legacy_surface_set and missing == ["surface_set"]:
        # The first T-46 commit predates the reverse surface enumeration field.
        # Historical snapshots still need to be comparable, while the current
        # worktree remains strict below.
        values["surface_set"] = SURFACE_SET
        missing = []
    if missing or extra:
        detail = []
        if missing:
            detail.append("missing " + ", ".join(missing))
        if extra:
            detail.append("unknown " + ", ".join(extra))
        raise GuardError(
            f"{source}:{line}: contract metadata has {'; '.join(detail)}; next "
            "action: keep id, scope, surface_set, ruling and evidence only"
        )
    if not ID_RE.fullmatch(values["id"]):
        raise GuardError(
            f"{source}:{line}: invalid contract id {values['id']!r}; next action: "
            "use a stable UOC-... identifier"
        )
    if not values["scope"]:
        raise GuardError(
            f"{source}:{line}: empty scope for {values['id']}; next action: name "
            "each covered screen explicitly"
        )
    scopes = values["scope"].split(",")
    if any(not SCREEN_RE.fullmatch(screen) for screen in scopes):
        raise GuardError(
            f"{source}:{line}: invalid scope {values['scope']!r} for "
            f"{values['id']}; next action: use comma-separated named screens"
        )
    if len(set(scopes)) != len(scopes):
        raise GuardError(
            f"{source}:{line}: duplicate screen in scope for {values['id']}; next "
            "action: list each screen once"
        )
    if not RULING_RE.fullmatch(values["ruling"]):
        raise GuardError(
            f"{source}:{line}: invalid owner ruling source {values['ruling']!r} "
            f"for {values['id']}; next action: attach an owner rc-... card"
        )
    if not values["evidence"]:
        raise GuardError(
            f"{source}:{line}: empty evidence for {values['id']}; next action: "
            "point to the current source-of-truth line"
        )
    if values["surface_set"] != SURFACE_SET:
        raise GuardError(
            f"{source}:{line}: unknown surface_set {values['surface_set']!r} for "
            f"{values['id']}; next action: use {SURFACE_SET!r} so the scope is "
            "derived from the ReplyCardBody caller enumeration"
        )
    return values


def parse_contract(
    text: str,
    source: str,
    allow_legacy_surface_set: bool = False,
) -> Tuple[List[ContractBlock], Dict[str, str]]:
    lines = text.splitlines()
    blocks: List[ContractBlock] = []
    retired: Dict[str, str] = {}
    seen_ids: Set[str] = set()
    i = 0
    while i < len(lines):
        start_match = START_RE.fullmatch(lines[i])
        if start_match:
            start = i
            meta = parse_metadata(
                start_match.group(1),
                i + 1,
                source,
                allow_legacy_surface_set=allow_legacy_surface_set,
            )
            end = None
            for j in range(i + 1, len(lines)):
                if lines[j] == END_LINE:
                    end = j
                    break
            if end is None:
                raise GuardError(
                    f"{source}:{i + 1}: contract {meta['id']} has no closing "
                    "marker; next action: close the block with "
                    f"{END_LINE}"
                )
            if meta["id"] in seen_ids:
                raise GuardError(
                    f"{source}:{i + 1}: duplicate contract id {meta['id']}; next "
                    "action: keep one canonical entry per external action/result"
                )
            seen_ids.add(meta["id"])
            sentence_lines: List[Tuple[int, str]] = []
            assertions: List[AssertionSpec] = []
            for body_index in range(i + 1, end):
                line = lines[body_index]
                if not line.strip():
                    continue
                sentence_match = SENTENCE_RE.fullmatch(line)
                if sentence_match:
                    sentence_lines.append((body_index + 1, sentence_match.group(1)))
                    continue
                assertion_match = ASSERTION_RE.fullmatch(line)
                if assertion_match:
                    assertions.append(
                        AssertionSpec(
                            screen=assertion_match.group(1),
                            marker=assertion_match.group(2),
                            line=body_index + 1,
                        )
                    )
                    continue
                raise GuardError(
                    f"{source}:{body_index + 1}: unexpected text inside "
                    f"{meta['id']}; next action: keep one sentence line and "
                    "one assertion line per named screen"
                )
            if len(sentence_lines) != 1 or not sentence_lines[0][1].strip():
                raise GuardError(
                    f"{source}:{i + 1}: {meta['id']} must contain exactly one "
                    "non-empty sentence; next action: keep one external action → "
                    "result sentence"
                )
            if not assertions:
                raise GuardError(
                    f"{source}:{i + 1}: {meta['id']} has no named e2e assertion; "
                    "next action: add one assertion marker for every scoped screen"
                )
            scope = tuple(meta["scope"].split(","))
            assertion_screens = [assertion.screen for assertion in assertions]
            if any(screen not in scope for screen in assertion_screens):
                extra = sorted(set(assertion_screens) - set(scope))
                raise GuardError(
                    f"{source}:{i + 1}: {meta['id']} binds assertion screen(s) "
                    f"{', '.join(extra)} outside scope; next action: either add "
                    "the screen to scope with its contract wording or remove the "
                    "orphan assertion"
                )
            if set(assertion_screens) != set(scope):
                missing = sorted(set(scope) - set(assertion_screens))
                raise GuardError(
                    f"{source}:{i + 1}: {meta['id']} scope claims screen(s) "
                    f"{', '.join(missing)} but has no named e2e assertion for "
                    "them; next action: add an assertion= line and matching "
                    "UOC_ASSERT marker for every scoped screen"
                )
            assertion_keys = [(a.screen, a.marker) for a in assertions]
            if len(set(assertion_keys)) != len(assertion_keys):
                raise GuardError(
                    f"{source}:{i + 1}: duplicate screen/marker binding in "
                    f"{meta['id']}; next action: give each screen one unique "
                    "named assertion binding"
                )
            blocks.append(
                ContractBlock(
                    contract_id=meta["id"],
                    scope=scope,
                    ruling=meta["ruling"],
                    evidence=meta["evidence"],
                    surface_set=meta["surface_set"],
                    sentence=sentence_lines[0][1],
                    assertions=tuple(assertions),
                    start_line=i + 1,
                    metadata_line=lines[i],
                    raw=tuple(lines[start : end + 1]),
                )
            )
            i = end + 1
            continue
        retired_match = RETIRED_RE.fullmatch(lines[i])
        if retired_match:
            contract_id, ruling = retired_match.groups()
            if not ID_RE.fullmatch(contract_id) or not RULING_RE.fullmatch(ruling):
                raise GuardError(
                    f"{source}:{i + 1}: malformed retired-contract ruling record; "
                    "next action: use id=UOC-... ruling=rc-..."
                )
            if contract_id in retired:
                raise GuardError(
                    f"{source}:{i + 1}: duplicate retired ruling record for "
                    f"{contract_id}; next action: keep one deletion record"
                )
            retired[contract_id] = lines[i]
        i += 1
    if not blocks:
        raise GuardError(
            f"{source}: no contract blocks found; next action: add one "
            "user-operation-contract block with a named e2e assertion"
        )
    return blocks, retired


def scan_markers() -> List[Marker]:
    tests_root = ROOT / "e2e_test" / "tests"
    if not tests_root.is_dir():
        raise GuardError(
            f"{tests_root}: e2e test directory is missing; next action: restore "
            "the default-on browser test directory"
        )
    markers: List[Marker] = []
    seen_names: Dict[str, Marker] = {}
    for path in sorted(tests_root.rglob("*.js")):
        if "node_modules" in path.parts:
            continue
        rel = path.relative_to(ROOT).as_posix()
        lines = path.read_text(encoding="utf-8").splitlines()
        for index, line in enumerate(lines):
            match = MARKER_RE.fullmatch(line)
            if not match:
                continue
            contract_id, screen, name = match.groups()
            marker = Marker(contract_id, screen, name, rel, index + 1)
            previous = seen_names.get(name)
            if previous is not None:
                raise GuardError(
                    f"{rel}:{index + 1}: duplicate UOC_ASSERT name {name}; "
                    f"already used at {previous.path}:{previous.line}; next action: "
                    "give each named assertion a stable unique name"
                )
            seen_names[name] = marker
            next_index = index + 1
            while next_index < len(lines) and not lines[next_index].strip():
                next_index += 1
            if next_index >= len(lines) or not re.match(
                r"^(?:await\s+)?expect\s*\(", lines[next_index].strip()
            ):
                raise GuardError(
                    f"{rel}:{index + 1}: UOC_ASSERT {name} is not attached to a "
                    "Playwright expect; next action: put the marker immediately "
                    "before its named expect assertion"
                )
            assertion_window = "\n".join(lines[next_index : next_index + 32])
            if not re.search(r"\.to[A-Za-z][A-Za-z0-9_]*\s*\(", assertion_window):
                raise GuardError(
                    f"{rel}:{index + 1}: UOC_ASSERT {name} has no Playwright "
                    "matcher in the following assertion; next action: bind the "
                    "marker to a real expect(...).to... assertion"
                )
            markers.append(marker)
    if not markers:
        raise GuardError(
            f"{tests_root}: no UOC_ASSERT markers found; next action: bind the "
            "contract entries to named default-on browser assertions"
        )
    return markers


def validate_bindings(blocks: Sequence[ContractBlock], markers: Sequence[Marker]) -> None:
    expected: Dict[Tuple[str, str, str], ContractBlock] = {}
    for block in blocks:
        for assertion in block.assertions:
            key = (block.contract_id, assertion.screen, assertion.marker)
            expected[key] = block
    actual = {(marker.contract_id, marker.screen, marker.name): marker for marker in markers}
    missing = sorted(set(expected) - set(actual))
    if missing:
        contract_id, screen, name = missing[0]
        block = expected[missing[0]]
        raise GuardError(
            f"{CONTRACT_REL}:{block.start_line}: {contract_id} scope screen "
            f"{screen} has no named e2e assertion marker={name}; next action: "
            "restore or add the matching UOC_ASSERT in a default-on browser spec"
        )
    extra = sorted(set(actual) - set(expected))
    if extra:
        contract_id, screen, name = extra[0]
        marker = actual[extra[0]]
        raise GuardError(
            f"{marker.path}:{marker.line}: UOC_ASSERT {name} ({contract_id}, "
            f"screen={screen}) is not listed in the contract; next action: add "
            "its screen/marker binding to the contract or remove the marker"
        )


def validate_surface_scopes(
    blocks: Sequence[ContractBlock], surfaces: Sequence[Surface]
) -> None:
    expected = {surface.screen for surface in surfaces}
    if not expected:
        raise GuardError(
            "ReplyCardBody caller enumeration is empty; next action: restore a "
            "production surface before evaluating contract scope"
        )
    for block in blocks:
        actual = set(block.scope)
        if actual == expected:
            continue
        missing = sorted(expected - actual)
        extra = sorted(actual - expected)
        detail: List[str] = []
        if missing:
            detail.append("missing enumerated screen(s) " + ", ".join(missing))
        if extra:
            detail.append("not found in enumeration " + ", ".join(extra))
        raise GuardError(
            f"{CONTRACT_REL}:{block.start_line}: {block.contract_id} scope does not "
            f"equal the reverse ReplyCardBody caller enumeration ({'; '.join(detail)}); "
            "next action: list every discovered caller in this entry and bind one "
            "named assertion to each screen"
        )


def resolve_base() -> str:
    candidates: List[str] = []
    explicit = os.environ.get("OC_UOC_BASE_SHA", "").strip()
    if explicit:
        candidates.append(explicit)
    # A feature worktree can legitimately be behind a moving origin/main.  Use
    # the common ancestor first so a later main merge does not silently become
    # this branch's baseline.  The explicit override above is only for
    # hermetic selftests; CI's real baseline is this merge-base.
    merge_base = git("merge-base", "HEAD", "origin/main")
    if merge_base.returncode == 0 and merge_base.stdout.strip():
        candidates.append(merge_base.stdout.strip())
    github_base = os.environ.get("GITHUB_BASE_SHA", "").strip()
    if github_base:
        candidates.append(github_base)
    event_path = os.environ.get("GITHUB_EVENT_PATH", "").strip()
    if event_path:
        try:
            payload = json.loads(Path(event_path).read_text(encoding="utf-8"))
            event_base = (
                payload.get("pull_request", {}).get("base", {}).get("sha", "")
            )
            if event_base:
                candidates.append(str(event_base))
        except (OSError, json.JSONDecodeError):
            pass
    for candidate in candidates:
        checked = git("rev-parse", "--verify", f"{candidate}^{{commit}}")
        if checked.returncode == 0:
            return checked.stdout.strip()
    raise GuardError(
        "cannot resolve the PR contract baseline; next action: fetch origin/main "
        "so merge-base is available, or set OC_UOC_BASE_SHA only for a hermetic "
        "selftest"
    )


def baseline_text(base: str) -> Optional[str]:
    result = git("show", f"{base}:{CONTRACT_REL}")
    if result.returncode == 0:
        return result.stdout
    if "does not exist" in result.stderr or "exists on disk, but not in" in result.stderr:
        return None
    raise GuardError(
        f"cannot read contract baseline {base}:{CONTRACT_REL}; next action: "
        "fetch the base commit and retry"
    )


def added_lines_against(base: str, current_text: str) -> Set[str]:
    tracked = git("ls-files", "--error-unmatch", CONTRACT_REL)
    if tracked.returncode != 0:
        return set(current_text.splitlines())
    diff = git("diff", "--no-ext-diff", "--unified=0", base, "--", CONTRACT_REL)
    if diff.returncode != 0:
        raise GuardError(
            f"cannot compare {CONTRACT_REL} with baseline {base}; next action: "
            "fetch the baseline commit and retry"
        )
    return {
        line[1:]
        for line in diff.stdout.splitlines()
        if line.startswith("+") and not line.startswith("+++")
    }


def added_lines_between(old_ref: str, new_ref: str) -> Set[str]:
    diff = git(
        "diff",
        "--no-ext-diff",
        "--unified=0",
        old_ref,
        new_ref,
        "--",
        CONTRACT_REL,
    )
    if diff.returncode != 0:
        raise GuardError(
            f"cannot compare {CONTRACT_REL} between {old_ref} and {new_ref}; "
            "next action: fetch the baseline commits and retry"
        )
    return {
        line[1:]
        for line in diff.stdout.splitlines()
        if line.startswith("+") and not line.startswith("+++")
    }


def validate_snapshot_change(
    current_blocks: Sequence[ContractBlock],
    current_retired: Dict[str, str],
    previous_text: Optional[str],
    baseline_label: str,
    added: Set[str],
) -> None:
    """Require a new owner ruling for every changed full contract block.

    `ContractBlock.raw` deliberately includes the metadata line, sentence, every
    assertion, and the closing marker.  Comparing only metadata would let a
    sentence reverse its meaning while the row's id/scope/ruling stayed the
    same.  A changed existing block is authorized only when its `ruling=` value
    changes to a different value; moving an `evidence=` line is not a new
    ruling.  A newly-added block must add its metadata line, and a deletion
    must add a retired ruling record.
    """
    previous_blocks: List[ContractBlock] = []
    previous_retired: Dict[str, str] = {}
    if previous_text is not None:
        previous_blocks, previous_retired = parse_contract(
            previous_text, baseline_label, allow_legacy_surface_set=True
        )
    previous_by_id = {block.contract_id: block for block in previous_blocks}
    current_by_id = {block.contract_id: block for block in current_blocks}
    for contract_id in sorted(set(previous_by_id) | set(current_by_id)):
        old = previous_by_id.get(contract_id)
        new = current_by_id.get(contract_id)
        if old is not None and new is not None and old.raw == new.raw:
            continue
        if new is None:
            ruling_line = current_retired.get(contract_id)
            if ruling_line is None or ruling_line not in added:
                raise GuardError(
                    f"{CONTRACT_REL}: contract {contract_id} was removed without "
                    "an owner ruling source; narrowing/deletion is a literal "
                    "change too. Next action: keep an added "
                    "user-operation-contract-ruling record naming the owner rc-... "
                    "card, or restore the entry"
                )
            retired_match = RETIRED_RE.fullmatch(ruling_line)
            old_retired = previous_retired.get(contract_id)
            old_ruling = old.ruling if old is not None else None
            new_ruling = retired_match.group(2) if retired_match else None
            if old_ruling is not None and new_ruling == old_ruling:
                raise GuardError(
                    f"{CONTRACT_REL}: contract {contract_id} was removed with "
                    f"the same owner ruling {old_ruling}; next action: add a "
                    "different ruling=rc-... source in the retired record"
                )
            if old_retired is not None and old_retired == ruling_line:
                raise GuardError(
                    f"{CONTRACT_REL}: retired ruling for {contract_id} was not "
                    "new in this change; next action: add a different owner "
                    "ruling record or restore the entry"
                )
            continue
        if old is None:
            if new.metadata_line not in added:
                raise GuardError(
                    f"{CONTRACT_REL}:{new.start_line}: full contract block for "
                    f"{contract_id} was added without an owner ruling source "
                    f"in the {baseline_label} diff; next action: add the block "
                    "metadata line with ruling=rc-... from the owner裁定"
                )
            continue
        if "surface_set=" not in old.metadata_line:
            # This is the one schema migration in the existing T-46 history:
            # surface_set became mandatory when reverse enumeration was added.
            # Do not demand a second owner ruling merely for introducing that
            # required field; all current snapshots are still strict.
            continue
        if new.ruling == old.ruling:
            raise GuardError(
                f"{CONTRACT_REL}:{new.start_line}: full contract block for "
                f"{contract_id} changed in the {baseline_label} diff, but "
                "the sentence/scope/assertions have no new owner ruling source: "
                f"ruling={new.ruling} did not change from the previous value; "
                "evidence-only metadata edits are not a new owner ruling. "
                "Next action: change ruling= to a different rc-... value from "
                "the new owner裁定 before changing any literal"
            )


def validate_change_sources(
    current_text: str,
    current_blocks: Sequence[ContractBlock],
    current_retired: Dict[str, str],
    base: str,
) -> None:
    # Validate the complete committed branch range.  Looking only at HEAD^ is
    # insufficient: an unauthorized sentence change can be committed first
    # and hidden under an unrelated follow-up commit.  Each snapshot is parsed
    # at the commit where it changed, so a base that predates this new file is
    # not allowed to make every later edit look like an initial addition.
    commits = git("rev-list", "--reverse", f"{base}..HEAD")
    if commits.returncode != 0:
        raise GuardError(
            f"cannot enumerate committed contract changes from baseline {base}; "
            "next action: fetch the complete PR history and retry"
        )
    for commit in (line for line in commits.stdout.splitlines() if line.strip()):
        parent = git("rev-parse", f"{commit}^")
        if parent.returncode != 0:
            raise GuardError(
                f"cannot resolve parent of contract commit {commit}; next action: "
                "fetch the complete PR history and retry"
            )
        parent_ref = parent.stdout.strip()
        previous_text = baseline_text(parent_ref)
        committed_text = baseline_text(commit)
        if previous_text == committed_text:
            continue
        if committed_text is None:
            committed_blocks: List[ContractBlock] = []
            committed_retired: Dict[str, str] = {}
        else:
            committed_blocks, committed_retired = parse_contract(
                committed_text,
                f"{commit}:{CONTRACT_REL}",
                allow_legacy_surface_set=True,
            )
        validate_snapshot_change(
            committed_blocks,
            committed_retired,
            previous_text,
            f"committed {parent_ref}..{commit}",
            added_lines_between(parent_ref, commit),
        )

    # The committed range above protects a clean CI checkout.  Also compare
    # the working tree to HEAD so local development cannot hide an uncommitted
    # sentence change (including a newly-created contract file).
    head_text = baseline_text("HEAD")
    if head_text != current_text:
        validate_snapshot_change(
            current_blocks,
            current_retired,
            head_text,
            "working tree vs HEAD",
            added_lines_against("HEAD", current_text),
        )


def run() -> None:
    if not CONTRACT_PATH.is_file():
        raise GuardError(
            f"{CONTRACT_REL}: contract list is missing; next action: add the "
            "canonical user-operation contract file before adding assertions"
        )
    current_text = CONTRACT_PATH.read_text(encoding="utf-8")
    blocks, retired = parse_contract(current_text, CONTRACT_REL)
    try:
        surfaces = enumerate_and_report(ROOT)
    except SurfaceEnumerationError as exc:
        raise GuardError(str(exc)) from exc
    markers = scan_markers()
    validate_bindings(blocks, markers)
    validate_surface_scopes(blocks, surfaces)
    base = resolve_base()
    validate_change_sources(current_text, blocks, retired, base)
    print(
        f"[user-operation-contract-guard] OK — {len(blocks)} contract entries, "
        f"{len(markers)} named screen assertions, baseline {base}"
    )


def main() -> int:
    try:
        run()
    except (GuardError, OSError) as exc:
        print(f"[user-operation-contract-guard] FAIL — {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
