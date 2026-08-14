#!/usr/bin/env python3
"""Add parameter descriptions into spec/openapi.json's x-mcp legacy descriptors.

The descriptors are JSON *strings*, and the file's escaping convention is not
even consistent inside one descriptor: get_chat carries a tool-level
description with an escaped arrow and parameter descriptions with a literal
one. Re-serializing therefore cannot preserve the file, so nothing here is
re-serialized. The new key is spliced into the raw descriptor text at the
position alphabetical key order puts it, and every other byte is left alone.

The splice is checked by parsing before and after: the only difference the two
objects may show is exactly the descriptions being added.
"""
import json
import sys

SPEC = "spec/openapi.json"


PROP_IND = " " * 10  # a parameter name inside inputSchema.properties
KEY_IND = " " * 12  # that parameter's own keys


def splice(raw, param, desc):
    """Insert a description key into `param`'s object inside the raw descriptor.

    Alphabetical key order is preserved, and nothing outside the inserted line
    is touched — the file's escaping is inconsistent enough that re-serializing
    would rewrite bytes this change has no business rewriting.
    """
    lines = raw.split("\n")
    open_line = f'{PROP_IND}"{param}": {{'
    encoded = json.dumps(desc)

    # Some descriptors write a property inline: `"key": {"type": "string"},`.
    inline = [i for i, l in enumerate(lines)
              if l.startswith(open_line) and l.rstrip(",").endswith("}")]
    if inline:
        if len(inline) != 1:
            sys.exit(f"{param}: property object is not uniquely locatable")
        i = inline[0]
        head, _, rest = lines[i].partition("{")
        inner = rest.rstrip()[: rest.rstrip().rindex("}")]
        tail = lines[i][len(head) + 1 + len(inner):]
        keys = [k for k in json.loads("{" + inner + "}")]
        after = [k for k in keys if k > "description"]
        if after:
            at = inner.index(f'"{after[0]}"')
            inner = inner[:at] + f'"description": {encoded}, ' + inner[at:]
        else:
            inner = inner.rstrip() + f', "description": {encoded}'
        return "\n".join(lines[:i] + [head + "{" + inner + tail] + lines[i + 1:])

    try:
        start = lines.index(open_line)
    except ValueError:
        sys.exit(f"{param}: could not locate its property object")
    if lines.count(open_line) != 1:
        sys.exit(f"{param}: property object is not uniquely locatable")

    close = f"{PROP_IND}}}"
    end = next(i for i in range(start + 1, len(lines))
               if lines[i] in (close, close + ","))

    at = end  # default: after the last key
    for i in range(start + 1, end):
        line = lines[i]
        if not line.startswith(KEY_IND + '"') or line[len(KEY_IND)] != '"':
            continue
        key = json.loads(line[len(KEY_IND):line.index('":', len(KEY_IND)) + 1])
        if key > "description":
            at = i
            break

    if at == end:  # appended last — the previous key now needs a comma
        lines[end - 1] += ","
        new = f'{KEY_IND}"description": {encoded}'
    else:
        new = f'{KEY_IND}"description": {encoded},'
    lines.insert(at, new)
    return "\n".join(lines)


def load():
    with open(SPEC, encoding="utf-8") as f:
        return f.read()


def tools(spec):
    """Yield (raw_descriptor, parsed, tool_name)."""
    for ops in spec["paths"].values():
        for op in ops.values():
            if not isinstance(op, dict):
                continue
            x = op.get("x-mcp")
            if not x:
                continue
            raw = x.get("legacy", {}).get("descriptor")
            if raw is None:
                continue
            yield raw, json.loads(raw), x.get("name")


def apply(descs):
    """descs: {tool: {param: description}} -> writes SPEC in place."""
    text = load()
    spec = json.loads(text)
    by_tool = {name: (raw, obj) for raw, obj, name in tools(spec)}

    unknown_tools = sorted(set(descs) - set(by_tool))
    if unknown_tools:
        sys.exit(f"unknown tools: {unknown_tools}")

    touched = 0
    for tool, params in descs.items():
        raw, obj = by_tool[tool]
        props = obj["inputSchema"]["properties"]
        missing = sorted(set(params) - set(props))
        if missing:
            sys.exit(f"{tool}: no such parameters: {missing}")

        new_raw = raw
        for param, desc in params.items():
            if props[param].get("description"):
                sys.exit(f"{tool}.{param}: already has a description; refusing to overwrite")
            new_raw = splice(new_raw, param, desc)

        # The splice is text surgery on a JSON blob: prove it changed exactly
        # what it was asked to and nothing else.
        after = json.loads(new_raw)
        for param, desc in params.items():
            got = after["inputSchema"]["properties"][param].pop("description", None)
            if got != desc:
                sys.exit(f"{tool}.{param}: splice did not land the description")
        if after != obj:
            sys.exit(f"{tool}: splice changed something other than the descriptions")

        old_json = json.dumps(raw, ensure_ascii=False)
        new_json = json.dumps(new_raw, ensure_ascii=False)
        if text.count(old_json) != 1:
            sys.exit(f"{tool}: descriptor string is not uniquely locatable in the file")
        text = text.replace(old_json, new_json)
        touched += 1

    with open(SPEC, "w", encoding="utf-8") as f:
        f.write(text)
    return touched


def audit():
    """Print totals: params, described, blank."""
    spec = json.loads(load())
    total = described = 0
    for _, obj, _ in tools(spec):
        for p in obj["inputSchema"].get("properties", {}).values():
            total += 1
            if p.get("description"):
                described += 1
    print(f"params={total} described={described} blank={total - described}")


if __name__ == "__main__":
    if sys.argv[1:] == ["audit"]:
        audit()
    else:
        payload = json.load(open(sys.argv[1], encoding="utf-8"))
        print(f"rewrote {apply(payload)} descriptors")
