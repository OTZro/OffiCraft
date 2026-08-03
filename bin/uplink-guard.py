#!/usr/bin/env python3
"""Every place the CLI can send a request body must be a committed, tested uplink.

The incident this guards against: one commit tightened 30+ endpoint schemas and
killed four uplinks the shipped clients were actually sending, with the whole CI
green — because nothing compared "what a live client sends" against "what the
server's schema accepts".

The shape of the guard matters more than its coverage today. The obvious design —
list the uplinks we know about and check each one has a test — was tried, and it
fails silently in the one direction that matters: a list that is missing an entry
does not go red, it just covers less. Every round of review found one more hole of
exactly that kind (a new module; then a second call through an already-listed
module's helper; then `http.Post`, which the enumeration could not see at all),
because a forward-only list can only ever be extended by somebody remembering to
extend it.

So this asks the question backwards, as a query that must come back empty:

    enumerate every callsite in cli/** that can put a body on the wire, and
    require each one to be claimed by exactly one row in cli/uplinks.json.

Adding an uplink means adding a callsite, and a callsite nobody claimed is a
non-empty answer to that query — which is the gate going red.

Two properties are load-bearing, and both were absent from earlier versions that
looked finished:

  * **The claim is per callsite, not per file.** Rows name the actual line they
    account for, so "delete one uplink and add another in the same file" cannot
    balance the books, and a row cannot be pointed at some harmless line while the
    real send goes unclaimed.
  * **Everything is matched against LEXED code, never raw text.** Go's comments,
    interpreted strings and raw strings are stripped by a real scanner. A regex
    approximation was tried and was wrong in both directions: a backtick string
    containing `/*` blanked out arbitrary amounts of real code, and `"a//b"` ate
    the rest of its own line.

`bin/tests/uplink-guard-selftest.py` holds one fixture per known bypass and
requires this scanner to name it. That file is the reason a future edit cannot
quietly narrow the scan: shrinking the enumeration reddens the selftest.
"""
import json, os, re, sys
from pathlib import Path
from typing import NoReturn

ROOT = Path(__file__).resolve().parents[1]
manifest_path = Path(os.environ.get("OC_UPLINKS_MANIFEST", ROOT / "cli/uplinks.json"))

# Anything that can put a body on the wire. Deliberately WIDE — an over-match
# costs one committed row with a reason, while a pattern narrow enough to be
# exactly right is a pattern the next uplink can be written just outside of.
#
# The list is wide in the specific directions that were shown to be exploitable:
# stdlib's capitalised helpers (`http.Post`, `PostForm`), a hand-built
# `&http.Request{}` handed to any `Do`, and a method invocation on any receiver.
# `Do(` alone brings in downloads and SSE streams; those get a row saying so,
# which is cheaper than a rule clever enough to tell them apart.
SINK_PATTERNS = [
    # The package qualifier is optional everywhere: `import nh "net/http"` renames
    # it, and a pattern that hard-codes `http.` stops seeing the whole file.
    re.compile(r"\b(?:\w+\.)?NewRequest(?:WithContext)?\s*\("),
    re.compile(r"\b(?:\w+\.)?(?:Post|PostForm|Put|Patch)\s*\("),
    # ANY receiver, not \w+ — `(&http.Client{Timeout: t}).Do(req)` is the shape
    # this repo already uses in two live places, and requiring a bare identifier
    # before the dot made the guard blind to its own codebase.
    re.compile(r"\.\s*(?:Post|PostForm|Do)\s*\("),
    re.compile(r"&\s*(?:\w+\.)?Request\s*{"),
    re.compile(r"\bnew\s*\(\s*(?:\w+\.)?Request\s*\)"),
    # ── everything above this line touches net/http DIRECTLY (HTTP_LEVEL) ──
    # `[` too: a generic helper is instantiated as post[T](…), and the call would
    # otherwise sit just outside the pattern.
    re.compile(r"\b(?:\w+\.)?(?:post|postJSON|reportPost)\s*[\(\[]"),
    # A send seam MENTIONED without being called on the spot is a function VALUE:
    # `emit := s.post` then `emit(route, body)` compiles, sends for real, and every
    # pattern above sees nothing — the call is spelled `emit(`, a name no rule can
    # know. Independent review landed a live uplink this way with zero manifest
    # edits, using the repo's own seam and no new import, so it was CHEAPER than
    # declaring honestly. Taking the alias itself as the callsite is what closes
    # it: the value has to be PRODUCED somewhere, and every way of producing it
    # mentions the seam without calling it on the spot.
    #
    # Scoping this to the right-hand side of `=`/`:=` was tried and was wrong: Go
    # takes a function value without any assignment at all. Review shipped three
    # live uplinks past that version with ZERO manifest edits — `apply(s.post, …)`
    # (argument), `return s.post` (result), and `[]poster{s.post}` (composite
    # literal element) — three routes, real bodies on a real test server, guard
    # green. So the rule is simply: a seam name not immediately followed by `(` or
    # `[` is a value being taken. That over-matches parameter declarations and nil
    # comparisons, and the cost is one committed row each — which is the trade this
    # file makes everywhere, because a narrowed scan is the one edit nothing here
    # can catch.
    # `:=` / `=` / `:` after the name mean this occurrence is the name being
    # DEFINED (a variable, a struct field key), not a value being taken — and the
    # value it is being defined AS is a send in its own right, claimed on the same
    # line by the derived-sink layer below. Without this, `post := httpPoster(...)`
    # counts twice and no anchor can name exactly one.
    re.compile(r"\b(?:\w+\.)?(?:post|postJSON|reportPost|Post|PostForm|Do"
               r"|NewRequest|NewRequestWithContext)\b(?![ \t]*(?:[\(\[\w]|:?=|:))"),
]
# A sink name that IS this line's function declaration is a declaration, not a
# call: the shared seams are themselves named `post`/`postJSON`. The test is on
# the text BEFORE the match, so a real call inside a one-line function body is
# still a call — checking only "does the line start with func" hid exactly that.
# How many of SINK_PATTERNS are net/http-level. derived_sink_patterns keys on these
# rather than on all of them: keying on the seam names too makes every caller of a
# seam a sink, which is the transitive closure by another route.
HTTP_LEVEL = 5
FUNC_DECL_PREFIX = re.compile(r"^\s*func\s*(\([^)]*\)\s*)?$")
TEST_FUNC = re.compile(r"^func\s+Test\w*\s*\(", re.M)
# The runtime half of "the list equals what was actually confronted". A wire test
# names itself in cli/uplinks.json; this is the call that makes it read that file
# back and refuse to pass while committed-but-undriven rows exist. Static counting
# here could never do it: every row this file validates is validated in the same
# pass, so a purely static compared-vs-committed comparison is provably vacuous —
# an earlier version carried one under nine lines of comment calling it the
# load-bearing check, and instrumenting its failure branch showed it was never
# once reached.
JOIN_CALL = "manifestUplinkPaths("

def fail(message) -> NoReturn:
    print("[uplink-guard] FAIL — " + message, file=sys.stderr)
    raise SystemExit(1)

def code_only(text, mask_strings=True):
    """Blank out comments — and optionally string contents — keeping line numbers.

    Two views of the same file, on the same line numbering:

      * `mask_strings=True` is what the sink patterns match against. A URL path or
        a message inside a string is data; nothing here should match on it.
      * `mask_strings=False` is what an anchor is located in. Anchors have to tell
        `s.post("/api/agent/context", …)` apart from `s.post("/api/reply-cards", …)`,
        and with strings masked those two lines are byte-identical.

    Both views strip comments, so an anchor can never be satisfied by a comment —
    that was a real bypass: a retired uplink's anchor left behind as a comment kept
    the row looking honest while a brand new, unclaimed send went live.

    A real scanner rather than a regex pass, because Go has four constructs that
    all contain characters the patterns above care about, and every shortcut tried
    here was wrong in a way that made the gate SMALLER without saying so:

      * `` `…/*…` `` — a raw string holding a comment opener. Treated as a comment
        start, it blanked every line until the next `*/` anywhere in the file.
      * `"a//b"` — an interpreted string holding a line-comment opener. Treated as
        a comment, it ate the rest of the line, including a real send after it.
      * `// see https://x — never post(anything)` — a comment holding `://`. An
        exception carved out for URLs let the comment be read as code instead.

    """
    out, i, n = [], 0, len(text)
    while i < n:
        ch = text[i]
        if ch == "/" and i + 1 < n and text[i + 1] == "/":
            while i < n and text[i] != "\n":
                out.append(" ")
                i += 1
        elif ch == "/" and i + 1 < n and text[i + 1] == "*":
            out.append("  ")
            i += 2
            while i < n and not (text[i] == "*" and i + 1 < n and text[i + 1] == "/"):
                out.append("\n" if text[i] == "\n" else " ")
                i += 1
            out.append("  ")
            i += 2
        elif ch in ('"', "`", "'"):
            closer, escapes = ch, ch != "`"
            out.append(ch)
            i += 1
            while i < n and text[i] != closer:
                if escapes and text[i] == "\\" and i + 1 < n:
                    out.append("  " if mask_strings else text[i:i + 2])
                    i += 2
                    continue
                if text[i] == "\n":
                    out.append("\n")
                else:
                    out.append(" " if mask_strings else text[i])
                i += 1
            if i < n:
                out.append(closer)
                i += 1
        else:
            out.append(ch)
            i += 1
    return "".join(out)

FUNC_DECL = re.compile(r"^func\s+(?:\([^)]*\)\s*)?(\w+)\s*[\(\[]", re.M)

def enclosing_funcs(code):
    """[(name, decl_start, decl_end, body_end)] for every top-level func."""
    spans = []
    for match in FUNC_DECL.finditer(code):
        open_brace = code.find("{", match.end())
        if open_brace < 0:
            continue
        depth, i = 0, open_brace
        while i < len(code):
            if code[i] == "{":
                depth += 1
            elif code[i] == "}":
                depth -= 1
                if depth == 0:
                    break
            i += 1
        spans.append((match.group(1), match.start(), match.end(), i))
    return spans

def derived_sink_patterns(sources):
    """Patterns for functions that TOUCH net/http, derived from the code itself.

    The hard-coded names in SINK_PATTERNS are a list, and a list is exactly what
    this file exists to distrust. Independent review landed a live uplink in each
    module by routing it through a send helper whose NAME is not on that list —
    `httpPoster(...)` (its own constructor: no `post` substring, and `\\bPost` does
    not match inside `httpPoster`) and `httpRequest(...)`. Both compile, both send
    for real, both were rc=0 with the manifest untouched.

    So the second layer is not more names: it is a question asked of the tree.
    Any function whose body reaches net/http directly becomes a sink itself, so
    calling it is a callsite. Deliberately ONE layer and keyed on net/http rather
    than on the transitive closure — the closure pulls in `run` and `main` (173
    callsites, measured), which buries the signal in paperwork; this layer costs
    13 and every one of them is a call to something that really does send.
    """
    names = set()
    for source in sources:
        code = code_only(source.read_text())
        funcs = enclosing_funcs(code)
        for pattern in SINK_PATTERNS[:HTTP_LEVEL]:
            for match in pattern.finditer(code):
                for name, start, _, end in funcs:
                    if start <= match.start() <= end:
                        names.add(name)
    return [re.compile(r"\b(?:\w+\.)?" + re.escape(name) + r"\s*[\(\[]")
            for name in sorted(names)]

def sink_callsites(text, extra=()):
    """(line, offset) of every send in already-lexed code.

    Offsets, not a set of lines: `s.post(a); s.post(b)` on one line is TWO sends,
    and a line-keyed claim covers both with one row — which is a whole uplink
    nobody ever compared, hidden behind a semicolon that gofmt will not split.
    """
    spans = []
    for pattern in list(SINK_PATTERNS) + list(extra):
        for match in pattern.finditer(text):
            line_start = text.rfind("\n", 0, match.start()) + 1
            if FUNC_DECL_PREFIX.match(text[line_start:match.start()]):
                continue
            spans.append((match.start(), match.end()))
    # `http.Post(` matches both the qualified pattern and the any-receiver one, at
    # two different offsets. Counted separately they make ONE send look like two,
    # and then no anchor can be right: cover both and it "covers 2 sends", cover
    # one and the other is "unclaimed". Overlapping matches are one send.
    hits, reach = set(), -1
    for start, end in sorted(spans):
        if start <= reach:
            continue
        hits.add((text[:start].count("\n") + 1, start))
        reach = end - 1
    return hits

def cli_sources():
    """Every non-test Go file under cli/, at ANY depth.

    Depth matters: an earlier version globbed `cli/*/*.go`, so a file one directory
    deeper was outside the enumeration entirely.
    """
    return sorted(src for src in (ROOT / "cli").rglob("*.go")
                  if not src.name.endswith("_test.go"))

def locate(code, anchor, what):
    """The 1-based line where `anchor` appears in lexed code, requiring uniqueness.

    Uniqueness is what makes an anchor evidence instead of a hint: a substring that
    matches in two places names neither of them.
    """
    found = code.find(anchor)
    if found < 0 or code.find(anchor, found + 1) >= 0:
        fail(f"{what}: its anchor matches {code.count(anchor)} places in the code, needs "
             "exactly 1. An anchor matching nothing is stale — usually the code was "
             "renamed and the row was not, which is the row quietly ceasing to be about "
             "anything. One matching twice names neither; extend it across the next line "
             "until it is unique.")
    return code[:found].count("\n") + 1, found, found + len(anchor)

def main():
    try:
        manifest = json.loads(manifest_path.read_text())["uplinks"]
        spec = json.loads((ROOT / "spec/openapi.json").read_text())
    except (OSError, KeyError, json.JSONDecodeError) as err:
        fail(f"read manifest/spec: {err}")

    ids = [item.get("id") for item in manifest]
    if not ids or len(ids) != len(set(ids)) or any(not one for one in ids):
        fail("manifest ids must be non-empty and unique")
    for item in manifest:
        if item.get("kind") not in ("json", "seam", "skip"):
            fail(f"{item.get('id')}: kind must be json, seam, or skip")
        if not item.get("source") or not item.get("callsite"):
            fail(f"{item.get('id')}: every row must name its source file and the callsite line it claims")
        if item["kind"] == "skip" and not item.get("skip_reason"):
            fail(f"{item['id']}: an out-of-scope callsite needs its reason committed")
        if item["kind"] == "seam" and not item.get("seam_reason"):
            fail(f"{item['id']}: a send seam needs its reason committed")
        # `skip` and `seam` are the only opt-outs, so they are where a real uplink
        # would go to never be compared: write one plausible sentence and the row
        # is done. Nothing can machine-check whether a REASON is true, but this
        # much is checkable — a callsite that spells an API path in its own text
        # is sending to that API, and no committed sentence makes it otherwise.
        if item["kind"] in ("seam", "skip") and '"/api/' in item["callsite"]:
            fail(f"{item['id']}: this callsite names an API path in its own source line, "
                 f"so it IS an uplink — declare it kind=json with that route, the OpenAPI "
                 f"$ref, and a wire test that compares ITS body. kind=seam is the shared "
                 f"send implementation (its path arrives as a parameter) and kind=skip is "
                 f"for a nil body or bytes that are not JSON.")

    # Two uplinks may not point at the same piece of wire-test evidence. Without
    # this, a new uplink can be declared and aimed at an ALREADY-USED assertion:
    # every check passes, the compared/committed join balances, and the new body
    # was never once compared — the original incident wearing the paperwork of a fix.
    # Allowlisted rows are excluded: they carry no evidence by construction, so they
    # would all collide on (None, None) and the message would tell you to write an
    # assertion for a row that is forbidden from naming one — and the cheapest way
    # out of that wrong message is kind=skip, which is a real hiding place.
    witnesses = [(item.get("wire_test"), item.get("wire_needle")) for item in manifest
                 if item["kind"] == "json" and not item.get("allow_missing_spec")]
    # The runtime join keys on (wire test, producer run, route). Two rows sharing that
    # triple are interchangeable to it: review added a live uplink whose body would 422
    # in production, gave it the same route AND the same producer run as an existing
    # row, drove that producer one extra time, and the join balanced with the new body
    # never once sent. A row's slot in the join has to be its own.
    slots = [(item.get("wire_test"), item.get("wire_case", ""), item.get("path"))
             for item in manifest
             if item["kind"] == "json" and not item.get("allow_missing_spec")]
    if len(slots) != len(set(slots)):
        clash = sorted({s for s in slots if slots.count(s) > 1})
        fail(f"two JSON uplinks occupy the same slot in the runtime join {clash} — same "
             f"wire test, same producer run, same route. The join counts sends per slot, "
             f"so one of them can be paid for by driving the other twice. Give this "
             f"uplink its own producer run and name it in wire_case.")
    if len(witnesses) != len(set(witnesses)):
        shared = sorted({w for w in witnesses if witnesses.count(w) > 1})
        fail(f"two JSON uplinks name the same wire-test evidence {shared} — each needs an "
             "assertion that compares ITS body, or the second is only borrowing the "
             "first one's green")

    # ── the coverage query: every callsite claimed by exactly one row ──────────
    rows_by_source = {}
    for item in manifest:
        rows_by_source.setdefault(item["source"], []).append(item)
    unclaimed, misclaimed = [], []
    sending_modules = set()
    sources = cli_sources()
    derived = derived_sink_patterns(sources)
    for source in sources:
        rel = str(source.relative_to(ROOT))
        raw = source.read_text()
        callsites = sink_callsites(code_only(raw), derived)
        code = code_only(raw, mask_strings=False)
        if callsites:
            sending_modules.add(source.parent)
        claimed = {}
        for item in rows_by_source.pop(rel, []):
            line, lo, hi = locate(code, item["callsite"], item["id"])
            # A row claims the sends its anchor TEXT covers, not the sends that
            # happen to share its line. Anything looser lets one row absorb a
            # second send sitting next to the one it names.
            covered = {c for c in callsites if lo <= c[1] < hi}
            if not covered:
                misclaimed.append(
                    f"{item['id']} claims {rel}:{line}, but its anchor does not cover any "
                    f"send (this file sends at line(s) {sorted({l for l, _ in callsites})})")
            elif len(covered) > 1:
                misclaimed.append(
                    f"{item['id']}'s anchor covers {len(covered)} sends at once — one row per "
                    "send, so shorten the anchor until it names exactly one")
            for one in covered:
                if one in claimed:
                    misclaimed.append(f"{item['id']} and {claimed[one]} both claim {rel}:{one[0]}")
                else:
                    claimed[one] = item["id"]
        for line, _ in sorted(callsites - set(claimed)):
            unclaimed.append(f"{rel}:{line}")
    for rel, rows in sorted(rows_by_source.items()):
        misclaimed.append(f"{[r['id'] for r in rows]} name {rel}, which no longer exists under cli/")
    if unclaimed:
        fail("these CLI callsites can put a body on the wire and no uplinks.json row "
             "claims them:\n  " + "\n  ".join(unclaimed) +
             "\nEvery send has to be declared there — with its OpenAPI route and the "
             "wire test that compares a real producer's body against the frozen "
             "schema — because a send nobody compared is how the shipped clients were "
             "broken by a green build.\nIf a listed callsite genuinely puts no JSON "
             "body on the wire (a stream, a download, a function value that is never "
             "called), add a row with kind=skip or kind=seam and its reason. Do NOT "
             "narrow SINK_PATTERNS: over-matching costs one committed line, while a "
             "narrowed scan silently covers less and nothing here goes red for it.")
    if misclaimed:
        fail("uplinks.json does not line up with the code:\n  " + "\n  ".join(misclaimed))

    # A module that sends anything must own at least one wire test. The condition is
    # "has a body-capable callsite", derived from the enumeration rather than from
    # the manifest — a module absent from the manifest is exactly the case this is
    # for, so keying on the manifest would exempt it.
    # "Owns a wire test" is a claim about TESTS, so the condition is a test function,
    # not a filename. `printf 'package relay\n' > relay_wire_test.go` is 14 bytes and
    # satisfied the earlier filename-only version — independent review shipped a whole
    # sending module past this gate that way, which is the original incident's own
    # shape (a send path no test had ever driven) reappearing inside its own fix.
    untested = [str(m.relative_to(ROOT)) for m in sorted(sending_modules)
                if not any(TEST_FUNC.search(code_only(f.read_text()))
                           for f in m.glob("*_wire_test.go"))]
    if untested:
        fail(f"{untested} can put a body on the wire but own no *_wire_test.go with a "
             f"test function in it. The "
             "original incident was not one missing assertion — it was a whole send "
             "path no test had ever driven, so a schema change could break it with "
             "every suite green.")

    # ── each declared row still has to be true ────────────────────────────────
    paths = spec.get("paths", {})
    compared = 0
    for item in manifest:
        if item["kind"] != "json":
            continue
        ident, path, method = item["id"], item.get("path"), item.get("method", "").lower()
        # BEFORE the allowlist branch, deliberately. A row that spells an API path in
        # its own callsite text is sending THERE, whatever it claims — and while this
        # sat after the branch, "allow_missing_spec: retired server-side" was an
        # unconditional exemption any live uplink could take: independent review moved
        # a real, spec'd, tested reply-card uplink behind it (dropping path, $ref and
        # wire test entirely) and both gates stayed green. Checked first, the callsite
        # forces the path, the path is in OpenAPI, and the stale-allowlist rule fires.
        if not path:
            fail(f"{ident}: a JSON uplink must name the route it posts to")
        quoted = re.search(r'"(/api/[^"]*)"', item["callsite"])
        if quoted and quoted.group(1) != path:
            fail(f"{ident}: its callsite sends to {quoted.group(1)} but the row claims "
                 f"{path}, so the schema being compared is another route's")

        operation = paths.get(path, {}).get(method)
        # A row for a route the server does not have cannot be compared against
        # anything, so it must NOT be made to produce evidence: the earlier version
        # demanded a $ref and a wire test from it anyway, and the only way to satisfy
        # that was to name an unrelated DTO and an unrelated test — this file shipped
        # with exactly that, a row whose committed "evidence" drove a different
        # command entirely. Requiring paperwork that cannot be true manufactures lies.
        if item.get("allow_missing_spec"):
            if operation is not None:
                fail(f"{ident}: allowlist entry is stale; its route now exists — drop "
                     f"allow_missing_spec and give this uplink a real $ref and wire test")
            if item.get("request_schema") or item.get("wire_test"):
                fail(f"{ident}: an uplink whose route is not in OpenAPI has nothing to "
                     f"compare against, so it must not claim a request_schema or a wire "
                     f"test — those fields can only be filled in with something untrue")
            continue
        # The expected ref is committed beside the callsite, but the value it is
        # compared against is READ FROM OpenAPI — never a schema name spelled by
        # hand. Hand-spelled names are why an operation could be repointed at a
        # different DTO with this layer staying green: the general form of the
        # original incident.
        expected_ref = item.get("request_schema")
        if not isinstance(expected_ref, str) or not expected_ref.startswith("#/components/schemas/"):
            fail(f"{ident}: JSON uplink needs its OpenAPI request_schema $ref")
        wire_test, wire_needle = item.get("wire_test"), item.get("wire_needle")
        test_path = ROOT / wire_test if isinstance(wire_test, str) else None
        if not test_path or not test_path.is_file() or not isinstance(wire_needle, str) or not wire_needle:
            fail(f"{ident}: JSON uplink needs a readable wire-test evidence mapping")
        # Lexed, like everything else: a needle that only occurs in a comment is a
        # claim about a test that does not exist.
        test_code = code_only(test_path.read_text(), mask_strings=False)
        locate(test_code, wire_needle, f"{ident} wire-test evidence")
        # And that test has to perform the runtime join, or the row's evidence is
        # only "a string exists in a file". See JOIN_CALL. Looked for in the
        # strings-masked view: with strings kept, `var _ = "manifestUplinkCount(…)"`
        # satisfied it — two lines that retire the join while the guard reports it
        # is still wired in. That is the same "a comment/string can satisfy the
        # scanner" defect this file already fixed once for anchors.
        if JOIN_CALL not in code_only(test_path.read_text()):
            fail(f"{ident}: {wire_test} never calls {JOIN_CALL}…), so nothing makes it "
                 f"compare what it actually drove against what this manifest commits — "
                 f"a row added here would pass while its body was never once sent")

        if operation is None:
            fail(f"{ident}: {method.upper()} {path} is not in OpenAPI")
        schema = operation.get("requestBody", {}).get("content", {}).get("application/json", {}).get("schema", {})
        actual_ref = schema.get("$ref")
        if not actual_ref:
            fail(f"{ident}: OpenAPI requestBody has no application/json $ref")
        if actual_ref != expected_ref:
            fail(f"{ident}: OpenAPI requestBody $ref is {actual_ref}, manifest expects {expected_ref}")
        compared += 1

    # No compared-vs-committed total lives here any more. Every row this loop reaches
    # is validated by this same loop, so any such total is the same set counted twice
    # and can never disagree with itself — provably vacuous, however load-bearing the
    # comment above it claims to be. The real join is at RUNTIME, in the wire tests
    # (JOIN_CALL), where "committed" and "actually put on the wire" are genuinely two
    # different sources; this file's job is to make sure that call is still wired in.
    kinds = {kind: sum(1 for item in manifest if item["kind"] == kind)
             for kind in ("json", "seam", "skip")}
    print(f"[uplink-guard] all green ({compared} JSON uplinks compared against OpenAPI; "
          f"{kinds['seam']} send seam(s); {kinds['skip']} declared out of scope)")

if __name__ == "__main__":
    main()
