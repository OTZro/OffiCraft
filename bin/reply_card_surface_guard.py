#!/usr/bin/env python3
"""Reverse-enumerate every production caller of the shared reply-card body.

The user-operation contracts cover a rendered behavior, not a component name.
That means the scope must come from the tree in which the shared body is used:

    production source files that import ReplyCardBody
        == the checked-in source-to-screen mapping

The mapping supplies the human-facing screen name; it is not allowed to hide a
caller.  A caller absent from the mapping, or a mapping row whose import was
removed, is a failure.  The caller set is discovered on every run, and the
count is printed so a narrowed scan cannot look like a healthy fixed number.
"""

from __future__ import annotations

import json
import os
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Sequence, Set, Tuple


SURFACE_MANIFEST_REL = os.environ.get(
    "OC_REPLY_CARD_SURFACES_MANIFEST",
    "docs/design/user-operation-contract-surfaces.json",
)
SURFACE_SET = "reply-card-body-callers"
SCREEN_RE = re.compile(r"^[a-z][a-z0-9-]*$")
SOURCE_RE = re.compile(r"^frontend/src/[A-Za-z0-9_./-]+\.(?:ts|tsx)$")
TEST_FILE_RE = re.compile(r"(?:\.test|\.spec)\.[^.]+$")

# This intentionally names the module, not today's three paths.  A fourth
# production caller is therefore in the discovered set without any edit to
# this predicate; the manifest comparison then makes the missing claim red.
IMPORT_RE = re.compile(
    r"(?:\bfrom\s*|\bimport\s*)[\"']([^\"']*(?:^|/)ReplyCardBody)[\"']"
)


class SurfaceEnumerationError(Exception):
    """An actionable reverse-enumeration failure."""


@dataclass(frozen=True)
class Surface:
    source: str
    screen: str


def _manifest_path(root: Path) -> Path:
    return root / SURFACE_MANIFEST_REL


def _load_manifest(root: Path) -> List[Surface]:
    path = _manifest_path(root)
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise SurfaceEnumerationError(
            f"{SURFACE_MANIFEST_REL}: cannot read surface enumeration input: {exc}; "
            "next action: restore a JSON surfaces array with source and screen "
            "for every production ReplyCardBody caller"
        ) from exc
    if not isinstance(payload, dict) or set(payload) != {"surfaces"}:
        raise SurfaceEnumerationError(
            f"{SURFACE_MANIFEST_REL}: expected exactly one top-level surfaces field; "
            "next action: keep the source-to-screen enumeration input machine-readable"
        )
    rows = payload["surfaces"]
    if not isinstance(rows, list) or not rows:
        raise SurfaceEnumerationError(
            f"{SURFACE_MANIFEST_REL}: surfaces must be a non-empty array; next action: "
            "list every production caller rather than recording a hand-count"
        )

    result: List[Surface] = []
    seen_sources: Set[str] = set()
    seen_screens: Set[str] = set()
    for index, row in enumerate(rows, 1):
        if not isinstance(row, dict) or set(row) != {"source", "screen"}:
            raise SurfaceEnumerationError(
                f"{SURFACE_MANIFEST_REL}: surfaces[{index}] must contain only source "
                "and screen; next action: keep one explicit mapping per caller"
            )
        source = row["source"]
        screen = row["screen"]
        if not isinstance(source, str) or not SOURCE_RE.fullmatch(source):
            raise SurfaceEnumerationError(
                f"{SURFACE_MANIFEST_REL}: surfaces[{index}] has invalid source {source!r}; "
                "next action: point at a production frontend/src .ts/.tsx file"
            )
        if TEST_FILE_RE.search(source):
            raise SurfaceEnumerationError(
                f"{SURFACE_MANIFEST_REL}: {source} is a test file, not a production "
                "surface; next action: enumerate the rendered caller"
            )
        if not isinstance(screen, str) or not SCREEN_RE.fullmatch(screen):
            raise SurfaceEnumerationError(
                f"{SURFACE_MANIFEST_REL}: {source} has invalid screen {screen!r}; "
                "next action: use one explicit lower-case screen name"
            )
        if source in seen_sources:
            raise SurfaceEnumerationError(
                f"{SURFACE_MANIFEST_REL}: duplicate caller {source}; next action: "
                "keep exactly one row per production import"
            )
        if screen in seen_screens:
            raise SurfaceEnumerationError(
                f"{SURFACE_MANIFEST_REL}: duplicate screen {screen}; next action: "
                "give each rendered surface one distinct screen name"
            )
        seen_sources.add(source)
        seen_screens.add(screen)
        result.append(Surface(source, screen))
    return result


def _is_production_source(path: Path, root: Path) -> bool:
    try:
        rel = path.relative_to(root).as_posix()
    except ValueError:
        return False
    return (
        path.suffix in {".ts", ".tsx"}
        and rel.startswith("frontend/src/")
        and not TEST_FILE_RE.search(path.name)
    )


def _imports_shared_body(text: str) -> bool:
    # The source tree currently uses ordinary ES module imports, including
    # multi-line named imports.  This shape is deliberately module-based rather
    # than path-based: renaming/moving a caller does not retire the scan.
    return IMPORT_RE.search(text) is not None


def discover_callers(root: Path) -> Tuple[str, ...]:
    source_root = root / "frontend" / "src"
    if not source_root.is_dir():
        raise SurfaceEnumerationError(
            f"{source_root}: frontend source tree is missing; next action: restore "
            "the production callers before evaluating reply-card coverage"
        )
    callers: List[str] = []
    for path in sorted(source_root.rglob("*")):
        if not path.is_file() or not _is_production_source(path, root):
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except OSError as exc:
            raise SurfaceEnumerationError(
                f"{path.relative_to(root)}: cannot read production source: {exc}; "
                "next action: make the source readable to the enumeration"
            ) from exc
        if _imports_shared_body(text):
            callers.append(path.relative_to(root).as_posix())
    if not callers:
        raise SurfaceEnumerationError(
            f"{source_root}: reverse enumeration found zero production ReplyCardBody "
            "callers; next action: verify the import shape or restore the shared-body "
            "callers (a zero set is not a healthy contract scope)"
        )
    return tuple(callers)


def enumerate_surfaces(root: Path) -> Tuple[Surface, ...]:
    """Discover callers, verify the input mapping, and return screen mappings."""

    manifest = _load_manifest(root)
    discovered = set(discover_callers(root))
    declared = {surface.source for surface in manifest}
    unclaimed = sorted(discovered - declared)
    stale = sorted(declared - discovered)
    if unclaimed or stale:
        details: List[str] = []
        if unclaimed:
            details.append(
                "unclaimed production ReplyCardBody caller(s): "
                + ", ".join(unclaimed)
            )
        if stale:
            details.append(
                "surface manifest row(s) no longer import ReplyCardBody: "
                + ", ".join(stale)
            )
        raise SurfaceEnumerationError(
            "; ".join(details)
            + "; next action: reconcile the manifest with the reverse tree scan, "
            "then give every resulting screen a named contract assertion"
        )
    by_source = {surface.source: surface for surface in manifest}
    return tuple(by_source[source] for source in sorted(discovered))


def report(surfaces: Sequence[Surface], discovered_count: int | None = None) -> str:
    count = len(surfaces) if discovered_count is None else discovered_count
    lines = [
        f"[reply-card-surface-enumerator] discovered {count} production "
        "ReplyCardBody caller(s):"
    ]
    for surface in surfaces:
        lines.append(f"  - {surface.source} -> {surface.screen}")
    return "\n".join(lines)


def enumerate_and_report(root: Path) -> Tuple[Surface, ...]:
    """Print the discovered count/list and then enforce the mapping."""

    discovered = discover_callers(root)
    manifest = _load_manifest(root)
    by_source = {surface.source: surface for surface in manifest}
    preview = tuple(
        Surface(source, by_source[source].screen if source in by_source else "<unclaimed>")
        for source in discovered
    )
    print(report(preview, len(discovered)))
    return enumerate_surfaces(root)


if __name__ == "__main__":
    root = Path(
        os.environ.get(
            "OC_USER_OPERATION_CONTRACT_ROOT",
            str(Path(__file__).resolve().parents[1]),
        )
    ).resolve()
    try:
        enumerate_and_report(root)
    except (OSError, SurfaceEnumerationError) as exc:
        print(f"[reply-card-surface-enumerator] FAIL — {exc}")
        raise SystemExit(1)
