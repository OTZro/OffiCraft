#!/usr/bin/env python3
"""Reverse-enumerate every production caller of the shared reply-card body.

The user-operation contracts cover a rendered behavior, not a component name.
That means the scope must come from the tree in which the shared body is used:

    rendered production callers that import ReplyCardBody directly or through
    a re-export barrel
        == the checked-in source-to-screen mapping

The mapping supplies the human-facing screen name; it is not allowed to hide a
caller or substitute the barrel itself for downstream callers.  A caller absent
from the mapping, or a mapping row whose import was removed, is a failure.  The
caller set is discovered on every run, and the count is printed so a narrowed
scan cannot look like a healthy fixed number.
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

# This intentionally names the module, not today's three paths.  It accepts
# ordinary imports, lazy imports, and explicit source extensions.  A fourth
# production caller is therefore in the discovered set without any edit to
# this predicate; the manifest comparison then makes the missing claim red.
IMPORT_RE = re.compile(
    r"(?:\bfrom\s*|\bimport\s*(?:\(\s*)?)[\"']"
    r"(?:[^\"']*/)?ReplyCardBody(?:\.[A-Za-z0-9_-]+)?[\"']"
)
MODULE_REFERENCE_RE = re.compile(
    r"(?:\bfrom\s*|\bimport\s*(?:\(\s*)?)[\"']([^\"']+)[\"']"
)
REEXPORT_RE = re.compile(
    r"\bexport\s+(?:\*|\*\s+as\s+[A-Za-z_$][A-Za-z0-9_$]*|\{[^}]*\})"
    r"\s+from\s*[\"']([^\"']+)[\"']"
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
    # This is deliberately module-based rather than path-based: renaming or
    # moving a caller does not retire the scan.  The optional parentheses cover
    # dynamic import("./ReplyCardBody"), and the optional suffix covers imports
    # such as from "./ReplyCardBody.tsx".
    return IMPORT_RE.search(text) is not None


def _module_references(text: str) -> Tuple[str, ...]:
    return tuple(match.group(1) for match in MODULE_REFERENCE_RE.finditer(text))


def _reexport_references(text: str) -> Tuple[str, ...]:
    return tuple(match.group(1) for match in REEXPORT_RE.finditer(text))


def _resolve_relative_module(source: Path, specifier: str) -> Path | None:
    """Resolve a local TS/JS module reference when its target is in the tree."""

    if not specifier.startswith("."):
        return None
    candidate = source.parent / specifier
    candidates = [candidate]
    if candidate.suffix == "":
        candidates.extend(
            candidate.with_suffix(suffix)
            for suffix in (".ts", ".tsx", ".js", ".jsx")
        )
    candidates.extend(
        candidate / f"index{suffix}"
        for suffix in (".ts", ".tsx", ".js", ".jsx")
    )
    for resolved in candidates:
        if resolved.is_file():
            return resolved.resolve()
    return None


def _discover_rendered_callers(
    root: Path, source_texts: Dict[Path, str]
) -> Set[Path]:
    """Find body callers while keeping re-export-only barrels out of scope.

    A barrel is an export-only hop, not a rendered surface.  When a production
    file imports that barrel, the importer is the discovered caller.  The
    fixed-point walk handles more than one barrel in the chain and prevents a
    manifest entry for the barrel itself from hiding downstream screens.
    """

    body_modules = {path for path in source_texts if path.stem == "ReplyCardBody"}
    references = {
        path: tuple(
            resolved
            for specifier in _module_references(text)
            if (resolved := _resolve_relative_module(path, specifier)) is not None
        )
        for path, text in source_texts.items()
    }
    reexports = {
        path: tuple(
            resolved
            for specifier in _reexport_references(text)
            if (resolved := _resolve_relative_module(path, specifier)) is not None
        )
        for path, text in source_texts.items()
    }

    barrels: Set[Path] = {
        path for path, targets in reexports.items() if set(targets) & body_modules
    }
    changed = True
    while changed:
        changed = False
        for path, targets in reexports.items():
            if path not in barrels and set(targets) & barrels:
                barrels.add(path)
                changed = True

    direct_callers = {
        path
        for path, text in source_texts.items()
        if _imports_shared_body(text)
        and path not in body_modules
        and path not in barrels
    }
    barrel_callers = {
        path
        for path, targets in references.items()
        if set(targets) & barrels and path not in barrels and path not in body_modules
    }
    return direct_callers | barrel_callers


def discover_callers(root: Path) -> Tuple[str, ...]:
    source_root = root / "frontend" / "src"
    if not source_root.is_dir():
        raise SurfaceEnumerationError(
            f"{source_root}: frontend source tree is missing; next action: restore "
            "the production callers before evaluating reply-card coverage"
        )
    source_texts: Dict[Path, str] = {}
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
        source_texts[path.resolve()] = text
    caller_paths = _discover_rendered_callers(root, source_texts)
    callers = sorted(path.relative_to(root).as_posix() for path in caller_paths)
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
                "surface manifest row(s) no longer represent a rendered "
                "ReplyCardBody caller: "
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
