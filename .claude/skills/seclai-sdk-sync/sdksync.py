#!/usr/bin/env python3
"""Analysis helpers for syncing a Seclai SDK to a new OpenAPI spec.

Subcommands:
  parity     spec paths that have no request call in the hand-written client
  params     query params the client sends that the endpoint does not declare
  returns    client return types that disagree with the spec's response schema
  models     hand-written models missing properties their schema declares
  surface    public API surface diff against a released tag
  spec-diff  paths and schemas added/removed/changed between two spec revisions
  api-delta  public client methods added/removed between two git revisions

Stdlib only, so it runs in every SDK repo regardless of language toolchain.

Scope: parity and api-delta understand the four SDKs that issue HTTP requests
directly — python, javascript, go, csharp. seclai-cli wraps @seclai/sdk and
seclai-mcp ships no client source, so both are reported as not-applicable
rather than silently passing.
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

VERBS = ("GET", "POST", "PUT", "PATCH", "DELETE")

# A quoted path literal. Matched as "everything up to the closing quote" rather
# than a character class: C# interpolations embed calls —
# $"/agents/runs/{Uri.EscapeDataString(runId)}/cancel" — and a class that omits
# parentheses silently fails to match the whole literal, so the path is never
# extracted and the endpoint looks unimplemented.
PATH_LITERAL_RE = r"""(?:"(/[^"\n]*)"|`(/[^`\n]*)`|'(/[^'\n]*)')"""

# ── Language table ───────────────────────────────────────────────────────────
# `sources` are HAND-WRITTEN client files only. Generated trees must never be
# scanned: they contain a module per endpoint and would make parity always pass.
# Query-key extraction, used by `params`. Two forms:
#   key_re    — the key is captured directly (go: q["k"] = ; csharp: ["k"] = )
#   dict_at   — the keys live inside a brace-delimited literal that follows an
#               anchor. These MUST be sliced by brace matching, not regex: the
#               multi-line `params=_strip_none(\n    {...}\n)` form defeats a
#               non-greedy regex and yields an empty key set, which reads as
#               "this method sends no params" — a silent false pass.
#   helpers   — positional helper calls that expand to a fixed, ordered key list.
#               Expansion is arity-aware: only as many keys as arguments supplied.
LANGS = {
    "python": {
        "sig_re": r"\n    (?:async )?def ([a-z]\w*)\(([^)]*)\)\s*->\s*([^:]+):",
        "sig_groups": ("name", "params", "ret"),
        "detect": "seclai/seclai.py",
        "sources": ["seclai/seclai.py"],
        "method_re": r"^[ \t]+(?:async )?def ([a-z][a-z0-9_]*)\(",
        # Private helpers start with `_` and so never anchor a block of their
        # own; without this the public method above them swallows their request
        # calls. `api_key`, a plain property, was credited with GET /sources.
        "boundary_re": r"^[ \t]*(?:async )?def |^class ",
        "verb_re": r'"(GET|POST|PUT|PATCH|DELETE)"',
        "return_re": r"\s*->\s*([^:\n]+):",
        "dict_at": [r"params\s*=\s*(?:_strip_none\()?\s*"],
        "dict_key_re": r'"([a-zA-Z_][a-zA-Z0-9_]*)"\s*:',
        "skip": {"request", "request_raw", "stream", "paginate"},
    },
    "javascript": {
        "sig_re": r"\n  (?:async )?(\w+)\(([^)]*)\)\s*:\s*([^{\n]+?)\s*\{",
        "sig_groups": ("name", "params", "ret"),
        "detect": "src/client.ts",
        "sources": ["src/client.ts"],
        # Exactly two spaces: class members sit at that indent, while statements
        # inside a body sit at four or more. A looser `^[ \t]+` also matches
        # `return (await this.request(...)` and attributes findings to "return".
        "method_re": r"^  (?:async )?\*?([a-zA-Z_][a-zA-Z0-9_]*)\s*[(<]",
        "verb_re": r'"(GET|POST|PUT|PATCH|DELETE)"',
        "return_re": r"\s*:\s*([^{\n]+?)\s*\{",
        "dict_at": [r"query:\s*"],
        "dict_key_re": r'["\']?([a-zA-Z_][a-zA-Z0-9_]*)["\']?\s*:',
        "skip": {"request", "requestRaw", "uploadFile", "paginate"},
    },
    "go": {
        "sig_re": r"func \(c \*Client\) (\w+)\(([^)]*)\) \(([^)]*)\)",
        "sig_groups": ("name", "params", "ret"),
        "detect": "client.go",
        "sources": ["*.go"],
        "exclude": ["*_test.go"],
        "method_re": r"^func \(c \*Client\) ([A-Z][A-Za-z0-9]*)\(",
        # Free functions and type decls end a block; without this the helpers
        # between two client methods are read as part of the earlier one.
        "boundary_re": r"^(?:func|type|var|const)\b",
        "verb_re": r"http\.Method(Get|Post|Put|Patch|Delete)",
        # Go returns `(*T, error)`; take the first element of the tuple.
        "return_re": r"\s*\(?\s*([^,{\n]+?)\s*(?:,\s*error)?\s*\)?\s*\{",
        "key_re": r'q\["([a-zA-Z_][a-zA-Z0-9_]*)"\]\s*=',
        # Go builds a query either by subscript assignment (key_re) or as a map
        # literal. Reading only the former made Search and SearchDocs — whose
        # required `q` is set in the literal — report as never sending it, the
        # false positive twin of the bug this check exists to find.
        "dict_at": [r"map\[string\]string"],
        "dict_key_re": r'"([a-zA-Z_][a-zA-Z0-9_]*)"\s*:',
        "helpers": {"listQuery(": ["page", "limit"]},
        "skip": {"Do", "buildURL"},
    },
    "csharp": {
        "sig_re": r"public (?:async )?([\w<>,?\[\]. ]+?) ([A-Z]\w*)\(([^)]*)\)",
        "sig_groups": ("ret", "name", "params"),
        "detect": "src/Seclai/SeclaiClient.cs",
        "sources": ["src/Seclai/*.cs"],
        "method_re": r"public (?:async )?[\w<>,?\[\]. ]+ ([A-Z][A-Za-z0-9]*)\s*\(",
        # Same reason as python: only `public` anchors a block, so the private
        # Send*/BuildUri helpers fall inside whichever method precedes them.
        "boundary_re": r"^    (?:private|internal|protected|static)\b",
        "verb_re": r"HttpMethod\.(Get|Post|Put|Patch|Delete)",
        # C# puts the return type BEFORE the method name, so unlike the other
        # three it is read from the block head, not from after the parameters.
        "return_head_re": r"^\s*public (?:async )?(.+?)\s+[A-Z][A-Za-z0-9]*\s*\(",
        "key_re": r'\["([a-zA-Z_][a-zA-Z0-9_]*)"\]\s*=',
        "helpers": {"PaginationQuery(": ["page", "limit", "sort", "order"]},
        "skip": {"SendJsonAsync", "SendNoContentAsync", "SendRawAsync", "BuildUri", "PaginationQuery"},
    },
}

NOT_APPLICABLE = {
    "seclai-cli": "wraps @seclai/sdk; coverage is SDK-method-to-command, not spec-path",
    "seclai-mcp": "ships no client source",
}


def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    raise SystemExit(2)


def detect_lang(repo: Path) -> str | None:
    for name, cfg in LANGS.items():
        if (repo / cfg["detect"]).exists():
            return name
    return None


def source_files(repo: Path, cfg: dict) -> list[Path]:
    out: list[Path] = []
    for pat in cfg["sources"]:
        out.extend(sorted(repo.glob(pat)) if "*" in pat else ([repo / pat] if (repo / pat).exists() else []))
    for pat in cfg.get("exclude", []):
        excl = set(repo.glob(pat))
        out = [p for p in out if p not in excl]
    return out


def normalise(path: str) -> str:
    """Collapse every placeholder form to `{}` so paths compare across languages."""
    path = re.sub(r"\$\{[^}]*\}", "{}", path)   # JS template  ${agentId}
    path = re.sub(r"\{[^}]*\}", "{}", path)     # py f-string / C# interpolation / spec
    path = re.sub(r"%[sdv]", "{}", path)        # go fmt.Sprintf
    return path.rstrip("/") or "/"


def extract_paths(text: str, verb_re: str) -> dict[str, set[str]]:
    """Map normalised path -> set of verbs seen near its occurrences.

    Verb association is best-effort: it scans a window around each occurrence.
    Absence of a verb is reported as a warning, never as a hard miss.
    """
    found: dict[str, set[str]] = {}
    for m in re.finditer(PATH_LITERAL_RE, text):
        p = normalise(next(g for g in m.groups() if g is not None))
        if p == "/" or not p.startswith("/"):
            continue
        window = text[max(0, m.start() - 220): m.end() + 60]
        verbs = {v.upper() for v in re.findall(verb_re, window)}
        found.setdefault(p, set()).update(verbs)
    return found


def load_spec(ref: str | None, path: str, repo: Path) -> dict:
    if ref:
        try:
            blob = subprocess.check_output(
                ["git", "-C", str(repo), "show", f"{ref}:{path}"], stderr=subprocess.DEVNULL)
        except subprocess.CalledProcessError:
            die(f"cannot read {path} at {ref}")
        return json.loads(blob)
    f = Path(path) if Path(path).is_absolute() else repo / path
    if not f.exists():
        die(f"no spec at {f}\n"
            "       Only seclai-python, seclai-javascript and seclai-go bundle the spec.\n"
            "       For the others, point at one explicitly, e.g.\n"
            "         --spec ../seclai-python/openapi/seclai.openapi.json")
    return json.loads(f.read_text())


# ── params ───────────────────────────────────────────────────────────────────
def balanced_slice(text: str, at: int, opener: str = "{", closer: str = "}") -> str | None:
    """Return the brace-delimited literal starting at or after `at`.

    Regex cannot do this: query literals span lines and nest, and a non-greedy
    match silently truncates at the first inner `}`.
    """
    i = text.find(opener, at)
    if i == -1:
        return None
    depth, j = 0, i
    while j < len(text):
        if text[j] == opener:
            depth += 1
        elif text[j] == closer:
            depth -= 1
            if depth == 0:
                return text[i + 1: j]
        j += 1
    return None


def spec_query_index(spec: dict) -> dict[tuple[str, str], tuple[str, set[str], set[str]]]:
    """(VERB, normalised path) -> (raw path, declared query names, required names).

    Resolves `$ref` against components.parameters and merges path-level params
    into each operation, both of which the spec uses.
    """
    comp = spec.get("components", {}).get("parameters", {})

    def resolve(p: dict) -> dict:
        ref = p.get("$ref")
        if ref and ref.startswith("#/components/parameters/"):
            return comp.get(ref.rsplit("/", 1)[-1], {})
        return p

    index: dict[tuple[str, str], tuple[str, set[str], set[str]]] = {}
    for raw, ops in spec.get("paths", {}).items():
        shared = [resolve(p) for p in ops.get("parameters", [])]
        for verb, op in ops.items():
            if verb not in ("get", "post", "put", "patch", "delete"):
                continue
            params = shared + [resolve(p) for p in op.get("parameters", [])]
            q = {p["name"] for p in params if p.get("in") == "query" and "name" in p}
            req = {p["name"] for p in params
                   if p.get("in") == "query" and p.get("required") and "name" in p}
            index[(verb.upper(), normalise(raw))] = (raw, q, req)
    return index


def method_blocks(text: str, method_re: str, boundary_re: str | None = None):
    """Yield (name, body). Each block ends at the next method OR declaration.

    Ending only at the next *method* anchor makes a block swallow everything
    between two methods, so any free function declared in the gap is attributed
    to the method above it. In seclai-go that made `GetMe` — which sends no query
    at all — appear to send page/limit/sort/order, because the `listQuery` and
    `sortableListQuery` helpers sit between it and the next client method. Four
    of six reported Go errors were that one artefact, and a check that cries wolf
    is a check somebody turns off.
    """
    ms = list(re.finditer(method_re, text, re.M))
    bounds = [m.start() for m in re.finditer(boundary_re, text, re.M)] if boundary_re else []
    for i, m in enumerate(ms):
        end = ms[i + 1].start() if i + 1 < len(ms) else len(text)
        nxt = next((b for b in bounds if b > m.start()), None)
        if nxt is not None:
            end = min(end, nxt)
        yield m.group(1), text[m.start():end]


def block_call(body: str, verb_re: str) -> tuple[str, str] | None:
    """First (VERB, normalised path) issued inside a method body.

    The path must appear AFTER the verb: taking the first path-like literal
    anywhere in the block picks up example paths out of docstrings and prose.
    """
    v = re.search(verb_re, body)
    if not v:
        return None
    p = re.search(PATH_LITERAL_RE, body[v.end():v.end() + 400])
    if not p:
        return None
    path = normalise(next(g for g in p.groups() if g is not None))
    if path == "/":
        return None
    return v.group(1).upper(), path


def block_query_keys(body: str, cfg: dict) -> tuple[set[str], bool]:
    """(keys, parsed_ok). parsed_ok is False when a construction site was found
    but could not be read — reported as an error, never as "sends nothing"."""
    keys: set[str] = set()
    ok = True

    for anchor, ordered in (cfg.get("helpers") or {}).items():
        for m in re.finditer(re.escape(anchor), body):
            args = balanced_slice(body, m.end() - 1, "(", ")")
            if args is None:
                ok = False
                continue
            n = len([a for a in args.split(",") if a.strip()])
            keys |= set(ordered[:n])

    if "key_re" in cfg:
        keys |= set(re.findall(cfg["key_re"], body))

    indirect = False
    for anchor in cfg.get("dict_at", []):
        for m in re.finditer(anchor, body):
            rest = body[m.end():]
            stripped = rest.lstrip()
            if not stripped.startswith("{"):
                # `params=params` / `query: someVar` — a reference, not a literal.
                # Not readable here, but the literal is usually assigned earlier in
                # the same block, so only treat it as unaudited if nothing was found.
                indirect = True
                continue
            lit = balanced_slice(body, m.end())
            if lit is None:
                ok = False
                continue
            keys |= set(re.findall(cfg["dict_key_re"], lit))

    if indirect and not keys:
        ok = False
    return keys, ok


def cmd_params(args) -> int:
    repo = Path(args.repo).resolve()
    name = repo.name
    if name in NOT_APPLICABLE:
        print(f"{name}: not applicable — {NOT_APPLICABLE[name]}")
        return 0
    lang = args.lang or detect_lang(repo)
    if not lang:
        die(f"cannot detect SDK language in {repo}")
    cfg = LANGS[lang]
    files = source_files(repo, cfg)
    if not files:
        die(f"no client sources found for {lang} in {repo}")

    spec = load_spec(args.rev, args.spec, repo)
    index = spec_query_index(spec)

    undeclared, not_in_spec, unparsed, exposed = set(), set(), set(), {}
    missing_required: set[tuple[str, str, str]] = set()

    for f in files:
        text = f.read_text(errors="replace")
        for mname, body in method_blocks(text, cfg["method_re"], cfg.get("boundary_re")):
            if mname in cfg.get("skip", ()):
                continue
            call = block_call(body, cfg["verb_re"])
            if not call:
                continue
            keys, ok = block_query_keys(body, cfg)
            if not ok:
                unparsed.add((mname, f"{call[0]} {call[1]}"))
            if call not in index:
                not_in_spec.add((mname, f"{call[0]} {call[1]}"))
                continue
            raw, declared, required = index[call]
            exposed.setdefault((call[0], raw), set()).update(keys)
            if ok:
                # Only trust this when the query construction parsed: an
                # unreadable site would otherwise look like "sends nothing".
                for k in sorted(required - keys):
                    missing_required.add((mname, f"{call[0]} {raw}", k))
            for k in sorted(keys - declared):
                undeclared.add((mname, f"{call[0]} {raw}", k, tuple(sorted(declared))))

    print(f"{name} [{lang}] — {len(files)} client file(s), "
          f"{len(index)} spec operations")

    if undeclared:
        print(f"\nUNDECLARED ({len(undeclared)}) — sent but the endpoint does not accept it:")
        for m, op, k, decl in sorted(undeclared):
            print(f"   {m}  ({op})")
            print(f"       sends: {k}")
            print(f"       accepts: {', '.join(decl) or '(none)'}")

    if not_in_spec:
        print(f"\nNOT IN SPEC ({len(not_in_spec)}) — client calls a path the spec does not declare:")
        for m, op in sorted(not_in_spec):
            print(f"   {m}  ({op})")

    if missing_required:
        print(f"\nMISSING REQUIRED ({len(missing_required)}) — the endpoint requires this"
              f" and no code path sends it:")
        for m, op, k in sorted(missing_required):
            print(f"   {m}  ({op})  needs: {k}")

    if unparsed:
        print(f"\nUNPARSED ({len(unparsed)}) — query construction could not be read;"
              f" treat as unaudited, not as clean:")
        for m, op in sorted(unparsed):
            print(f"   {m}  ({op})")

    if not args.quiet_unexposed:
        gaps = []
        for (verb, _npath), (raw, declared, _req) in index.items():
            got = exposed.get((verb, raw))
            if got is None:
                continue          # endpoint not implemented at all — parity's job
            missing = declared - got
            if missing:
                gaps.append((f"{verb} {raw}", sorted(missing)))
        if gaps:
            print(f"\nUNEXPOSED ({len(gaps)}) — declared query params no method sends:")
            for op, names in sorted(gaps):
                print(f"   {op}: {', '.join(names)}")

    errors = len(undeclared) + len(not_in_spec) + len(unparsed) + len(missing_required)
    print()
    if errors:
        print(f"{errors} error(s)")
        return 1
    print("no parameter mismatches")
    return 0


# ── parity ───────────────────────────────────────────────────────────────────
def cmd_parity(args) -> int:
    repo = Path(args.repo).resolve()
    name = repo.name
    if name in NOT_APPLICABLE:
        print(f"{name}: not applicable — {NOT_APPLICABLE[name]}")
        return 0
    lang = args.lang or detect_lang(repo)
    if not lang:
        die(f"cannot detect SDK language in {repo} (looked for "
            + ", ".join(c["detect"] for c in LANGS.values()) + ")")
    cfg = LANGS[lang]
    files = source_files(repo, cfg)
    if not files:
        die(f"no client sources found for {lang} in {repo}")

    spec = load_spec(args.rev, args.spec, repo)
    text = "\n".join(f.read_text(errors="replace") for f in files)
    impl = extract_paths(text, cfg["verb_re"])

    missing, partial = [], []
    total = 0
    for p, ops in sorted(spec.get("paths", {}).items()):
        verbs = {v.upper() for v in ops if v in ("get", "post", "put", "patch", "delete")}
        if not verbs:
            continue
        total += len(verbs)
        norm = normalise(p)
        if norm not in impl:
            missing += [f"{v} {p}" for v in sorted(verbs)]
        else:
            seen = impl[norm]
            if seen and not verbs <= seen:
                partial += [f"{v} {p}" for v in sorted(verbs - seen)]

    print(f"{name} [{lang}] — {len(files)} client file(s), "
          f"{total} spec operations across {len(spec.get('paths', {}))} paths")
    if missing:
        print(f"\nMISSING — no request call for this path ({len(missing)}):")
        for m in missing:
            print(f"   {m}")
    if partial and not args.quiet_partial:
        print(f"\nverb not detected near an existing path ({len(partial)}) "
              f"— best-effort, verify by hand:")
        for m in partial:
            print(f"   {m}")
    if not missing:
        print("\nfull path parity")
    return 1 if missing else 0


# ── spec-diff ────────────────────────────────────────────────────────────────
def _schema_label(sch: dict | None) -> str:
    """A short, comparable name for a response/request schema."""
    if not sch:
        return "none"
    if "$ref" in sch:
        return sch["$ref"].rsplit("/", 1)[-1]
    if sch.get("type") == "array":
        return f"array<{_schema_label(sch.get('items'))}>"
    if sch.get("type") == "object" or "properties" in sch:
        return "object"
    return sch.get("type", "unknown")


def _json_schema(op: dict, section: str, code: str | None = None) -> dict | None:
    node = op.get("requestBody") if section == "request" else op.get("responses", {}).get(code, {})
    return (node or {}).get("content", {}).get("application/json", {}).get("schema")


def diff_operation(old: dict, new: dict, path: str, verb: str) -> list[str]:
    """What actually changed on one operation, beyond its prose.

    The original implementation labelled every non-verb change
    "description/params only", which reads as "nothing to do". In the 2026-07
    fast-follow two endpoints silently changed their 200 body from a bare array
    to a paginated envelope — a change that breaks every shipped client at
    runtime — and the tool reported them as prose edits. Anything a client can
    observe gets named here; only genuine prose edits fall through to "docs only".
    """
    lo = spec_query_index(old).get((verb, normalise(path)))
    ln = spec_query_index(new).get((verb, normalise(path)))
    oop = old["paths"][path][verb.lower()]
    nop = new["paths"][path][verb.lower()]
    out: list[str] = []

    if lo and ln:
        _, oq, oreq = lo
        _, nq, nreq = ln
        if nq - oq:
            out.append("+query " + ", ".join(sorted(nq - oq)))
        if oq - nq:
            out.append("-query " + ", ".join(sorted(oq - nq)))
        for name in sorted((nreq - oreq) & (oq & nq)):
            out.append(f"~query {name} is now required")
        for name in sorted((oreq - nreq) & (oq & nq)):
            out.append(f"~query {name} is no longer required")

    ob, nb = _schema_label(_json_schema(oop, "request")), _schema_label(_json_schema(nop, "request"))
    if ob != nb:
        out.append(f"~request {ob} -> {nb}")

    for code in sorted(set(oop.get("responses", {})) | set(nop.get("responses", {}))):
        if code.startswith("4") or code.startswith("5"):
            continue
        a = _schema_label(_json_schema(oop, "response", code))
        b = _schema_label(_json_schema(nop, "response", code))
        if a != b:
            out.append(f"~response {code}: {a} -> {b}")
    return out


def cmd_spec_diff(args) -> int:
    repo = Path(args.repo).resolve()
    old = load_spec(args.old, args.spec, repo)
    new = load_spec(args.new, args.spec, repo) if args.new else load_spec(None, args.spec, repo)

    op, np_ = set(old.get("paths", {})), set(new.get("paths", {}))
    os_, ns_ = set(old.get("components", {}).get("schemas", {})), set(new.get("components", {}).get("schemas", {}))

    def ops(spec, p):
        return sorted(v.upper() for v in spec["paths"][p] if v in ("get", "post", "put", "patch", "delete"))

    print(f"paths: {len(op)} -> {len(np_)}   schemas: {len(os_)} -> {len(ns_)}")

    if np_ - op:
        print(f"\nADDED PATHS ({len(np_ - op)}):")
        for p in sorted(np_ - op):
            print(f"   {p}  [{', '.join(ops(new, p))}]")
    if op - np_:
        print(f"\nREMOVED PATHS ({len(op - np_)}):")
        for p in sorted(op - np_):
            print(f"   {p}")

    changed, breaking = [], 0
    for p in sorted(np_ & op):
        if json.dumps(old["paths"][p], sort_keys=True) == json.dumps(new["paths"][p], sort_keys=True):
            continue
        oo, nn = set(ops(old, p)), set(ops(new, p))
        lines = []
        if nn - oo:
            lines.append("+verbs " + ", ".join(sorted(nn - oo)))
        if oo - nn:
            lines.append("-verbs " + ", ".join(sorted(oo - nn)))
        for verb in sorted(oo & nn):
            lines += [f"{verb} {d}" for d in diff_operation(old, new, p, verb)]
        breaking += sum(1 for ln in lines if "~response" in ln or ln.startswith("-verbs"))
        changed.append(f"   {p}" + ("\n" + "\n".join(f"       {ln}" for ln in lines) if lines
                                    else "  (docs only)"))
    if changed:
        print(f"\nCHANGED PATHS ({len(changed)}):")
        print("\n".join(changed))
        if breaking:
            print(f"\n   !! {breaking} response/verb change(s) above can break a shipped client.")
            print("      Check the SDK's declared return type, not just its query params.")

    if ns_ - os_:
        print(f"\nADDED SCHEMAS ({len(ns_ - os_)}):")
        for s in sorted(ns_ - os_):
            print(f"   {s}")
    if os_ - ns_:
        print(f"\nREMOVED SCHEMAS ({len(os_ - ns_)}):")
        for s in sorted(os_ - ns_):
            print(f"   {s}")

    ov = old.get("x-seclai-versions") or {}
    nv = new.get("x-seclai-versions") or {}
    if ov != nv:
        print("\nAPI VERSIONS CHANGED:")
        for key in ("default", "latest"):
            if ov.get(key) != nv.get(key):
                print(f"   {key}: {ov.get(key)} -> {nv.get(key)}")
        gained = [v for v in nv.get("known", []) if v not in ov.get("known", [])]
        if gained:
            print(f"   +known {', '.join(gained)}")
        print("   Each SDK pins these as constants and rejects an unknown version, so "
              "update them\n   or the new version cannot be selected. Their tests assert "
              "the constants match this block.")

    prop_changes = []
    for k in sorted(ns_ & os_):
        o = set((old["components"]["schemas"][k].get("properties") or {}))
        n = set((new["components"]["schemas"][k].get("properties") or {}))
        if o != n:
            bits = []
            if n - o:
                bits.append("+" + ",".join(sorted(n - o)))
            if o - n:
                bits.append("-" + ",".join(sorted(o - n)))
            prop_changes.append(f"   {k}: {' '.join(bits)}")
    if prop_changes:
        print(f"\nSCHEMA PROPERTY CHANGES ({len(prop_changes)}):")
        print("\n".join(prop_changes))
    return 0


# ── api-delta ────────────────────────────────────────────────────────────────
def verify_rev(repo: Path, rev: str | None) -> None:
    """Fail loudly on an unresolvable rev.

    Without this, an unreadable rev yields an empty method set and the delta
    reports every method in the SDK as newly added — a confident wrong answer.
    """
    if rev is None:
        return
    r = subprocess.run(["git", "-C", str(repo), "rev-parse", "--verify", "--quiet", f"{rev}^{{commit}}"],
                       capture_output=True, text=True)
    if r.returncode != 0:
        die(f"not a git revision in {repo.name}: {rev!r}")


def methods_at(repo: Path, rev: str | None, cfg: dict, files: list[Path]) -> set[str]:
    names: set[str] = set()
    for f in files:
        rel = f.relative_to(repo)
        if rev:
            try:
                text = subprocess.check_output(
                    ["git", "-C", str(repo), "show", f"{rev}:{rel}"],
                    stderr=subprocess.DEVNULL).decode("utf-8", "replace")
            except subprocess.CalledProcessError:
                continue
        else:
            text = f.read_text(errors="replace")
        for m in re.finditer(cfg["method_re"], text, re.M):
            n = m.group(1)
            if not n.startswith("_"):
                names.add(n)
    return names


def cmd_api_delta(args) -> int:
    repo = Path(args.repo).resolve()
    name = repo.name
    if name in NOT_APPLICABLE:
        print(f"{name}: not applicable — {NOT_APPLICABLE[name]}")
        return 0
    lang = args.lang or detect_lang(repo)
    if not lang:
        die(f"cannot detect SDK language in {repo}")
    cfg = LANGS[lang]
    files = source_files(repo, cfg)

    verify_rev(repo, args.old)
    verify_rev(repo, args.new)
    old = methods_at(repo, args.old, cfg, files)
    new = methods_at(repo, args.new, cfg, files)

    added, removed = sorted(new - old), sorted(old - new)
    label_new = args.new or "working tree"
    print(f"{name} [{lang}] {args.old} -> {label_new}")
    print(f"\nADDED ({len(added)}):")
    for n in added:
        print(f"   {n}")
    if removed:
        print(f"\nREMOVED ({len(removed)}):")
        for n in removed:
            print(f"   {n}")
        print("\n   note: a name in both lists was renamed or re-signatured, not deleted —"
              "\n   check the signature diff before writing a Removed changelog entry.")
    return 0


# ── returns ──────────────────────────────────────────────────────────────────
# The third axis. `parity` walks spec->client paths, `params` walks client->spec
# query keys, and neither can see a response shape. In 2026-07 two endpoints
# changed their 200 body from a bare array to a paginated envelope; every shipped
# SDK still declared a list, so all four broke at runtime and both checks stayed
# green. This compares the client's DECLARED return type against the spec's.

# Untyped escape hatches. Returning one is legal but means the caller gets no
# help from the compiler, so it is reported as a warning when the spec has a name.
UNTYPED = {
    "csharp": {"JsonElement", "JsonDocument", "object", "string", "Stream", "byte[]"},
    "go": {"json.RawMessage", "any", "interface{}", "map[string]any", "[]byte"},
    "python": {"Any", "dict", "dict[str, Any]", "Dict[str, Any]", "object", "bytes",
               "JSONValue", "JSONObject"},
    "javascript": {"unknown", "any", "object", "Blob", "ArrayBuffer", "string"},
}
NO_RETURN = {"csharp": {"Task", "void"}, "go": {"error", ""},
             "python": {"None"}, "javascript": {"void"}}


def short_label(label: str) -> str:
    """Drop the spec's module qualification: `routers__api__agents__X` -> `X`.

    No SDK reproduces that qualification in its own type names, so comparing the
    full name marks every qualified schema as a mismatch — 38 of 38 C# findings
    on the first run, which is a broken check rather than a broken SDK.
    """
    m = re.match(r"^array<(.+)>$", label)
    if m:
        return f"array<{short_label(m.group(1))}>"
    return label.rsplit("__", 1)[-1]


def strip_generated_prefix(name: str) -> str:
    """`RoutersApiAgentsSetEmailTriggerConfigRequest` -> the spec's short name.

    oapi-codegen flattens the spec's `routers__api__agents__X` qualification into
    the type name, so a literal comparison against the schema name never matches.
    """
    return re.sub(r"^Routers(?:Api|Authenticated)[A-Z][a-zA-Z]*?(?=[A-Z])", "", name)


def normalise_type(t: str, lang: str) -> str:
    """A client return type expressed the way `_schema_label` labels a schema."""
    t = t.strip().rstrip(";").strip()
    for wrapper in (r"^Task<(.+)>$", r"^Promise<(.+)>$", r"^Awaitable\[(.+)\]$"):
        m = re.match(wrapper, t)
        if m:
            t = m.group(1).strip()
    t = t.lstrip("*&").replace(" | null", "").replace(" | undefined", "")
    t = re.sub(r"\s*\|\s*None$", "", t)
    t = t.rstrip("?")
    for arr in (r"^List<(.+)>$", r"^IReadOnlyList<(.+)>$", r"^\[\](.+)$",
                r"^list\[(.+)\]$", r"^List\[(.+)\]$", r"^(.+)\[\]$"):
        m = re.match(arr, t)
        if m:
            return f"array<{normalise_type(m.group(1), lang)}>"
    if t in NO_RETURN.get(lang, set()):
        return "none"
    if t in UNTYPED.get(lang, set()):
        return "?"
    return strip_generated_prefix(t.lstrip("*"))


def block_return(body: str, cfg: dict) -> str | None:
    """The declared return type of the method a block starts with.

    Read by slicing the parameter list with paren matching rather than by regex:
    signatures wrap across lines and contain nested parens and generics, both of
    which truncate a regex silently — and a silent truncation here reads as
    "this method declares nothing to check".
    """
    if "return_head_re" in cfg:
        m = re.match(cfg["return_head_re"], body, re.S)
        return m.group(1).strip() if m else None
    # Start at the paren the method anchor itself ends on. Taking the first `(`
    # in the block instead picks up Go's receiver — `func (c *Client) GetMe(` —
    # so the "return type" came back as the rest of the signature.
    anchor = re.match(cfg["method_re"], body, re.M)
    if not anchor:
        return None
    open_paren = body.find("(", anchor.end() - 1)
    if open_paren == -1:
        return None
    params = balanced_slice(body, open_paren, "(", ")")
    if params is None:
        return None
    rest = body[open_paren + len(params) + 2:]
    m = re.match(cfg["return_re"], rest, re.S)
    return m.group(1).strip() if m else None


def cmd_returns(args) -> int:
    repo = Path(args.repo).resolve()
    lang = args.lang or detect_lang(repo)
    cfg = LANGS[lang]
    if "return_re" not in cfg and "return_head_re" not in cfg:
        print(f"{repo.name} [{lang}] — return types are not declared; nothing to check.")
        return 0
    spec = load_spec(args.rev, args.spec, repo)

    want: dict[tuple[str, str], str] = {}
    for raw, ops in spec.get("paths", {}).items():
        for verb, op in ops.items():
            if verb not in ("get", "post", "put", "patch", "delete"):
                continue
            for code in ("200", "201", "202"):
                if code in op.get("responses", {}):
                    want[(verb.upper(), normalise(raw))] = short_label(
                        _schema_label(_json_schema(op, "response", code)))
                    break

    mismatched, renamed, untyped, n = [], [], [], 0
    for path in source_files(repo, cfg):
        text = path.read_text(encoding="utf-8", errors="replace")
        for mname, body in method_blocks(text, cfg["method_re"], cfg.get("boundary_re")):
            if mname in cfg.get("skip", set()):
                continue
            call = block_call(body, cfg["verb_re"])
            if not call or call not in want:
                continue
            declared = block_return(body, cfg)
            if declared is None:
                continue
            n += 1
            got, expect = normalise_type(declared, lang), want[call]
            if expect in ("object", "none", "unknown", "?"):
                continue          # the spec itself declares nothing to match
            if got == expect or strip_generated_prefix(got) == expect:
                continue
            # Separate the two failure modes. A list where the API returns an
            # envelope throws at deserialization; a type NAMED differently from
            # its schema is only cosmetic, and the SDKs rename by convention —
            # all four drop the spec's `Model` and `Api` affixes. Ranking those
            # equally buried the one real finding under eleven harmless ones.
            # A wholly untyped return (`JsonElement`, `unknown`, `JSONValue`)
            # commits to no shape at all and deserializes anything, so it is a
            # coverage warning, never a shape error.
            if got == "?":
                untyped.append((mname, call, expect, declared))
            # Shape next. `list[dict[str, Any]]` IS a commitment to a list and is
            # also untyped; testing untyped-ness first classified seclai-python's
            # list_evaluation_criteria as a soft warning and reported the repo
            # clean, when it is exactly as broken as the Go and JS ones.
            elif got.startswith("array<") != expect.startswith("array<"):
                mismatched.append((mname, call, expect, declared))
            elif "?" in got:
                untyped.append((mname, call, expect, declared))
            else:
                renamed.append((mname, call, expect, declared))

    print(f"{repo.name} [{lang}] — {n} method(s) with both a declared return type "
          f"and a named response schema")

    if mismatched:
        print(f"\nSHAPE MISMATCH ({len(mismatched)}) — list vs object; this fails at deserialization:")
        for name, (verb, p), expect, declared in mismatched:
            print(f"   {name}  ({verb} {p})\n       spec: {expect}\n       code: {declared}")
    if renamed and not args.quiet_renamed:
        print(f"\nWARN — NAME DIFFERS ({len(renamed)}) — same shape, different type name:")
        for name, (verb, p), expect, declared in renamed:
            print(f"   {name}  ({verb} {p}): {declared}, spec calls it {expect}")
    if untyped and not args.quiet_untyped:
        print(f"\nWARN — UNTYPED ({len(untyped)}) — spec names a schema, the client returns a blob:")
        for name, (verb, p), expect, declared in untyped:
            print(f"   {name}  ({verb} {p}): {declared}, spec declares {expect}")

    if mismatched:
        print(f"\n{len(mismatched)} return type(s) disagree with the spec in SHAPE.")
        return 1
    print("\nno return type disagrees with the spec in shape")
    return 0



# ── surface ──────────────────────────────────────────────────────────────────
def public_surface(text: str, cfg: dict) -> dict[str, set[tuple[str, str]]]:
    """method name -> {(return type, parameter list)}. A set, because C# and Go
    both allow overloads/variants under one name."""
    out: dict[str, set[tuple[str, str]]] = {}
    order = cfg["sig_groups"]
    for m in re.finditer(cfg["sig_re"], text, re.S):
        parts = dict(zip(order, m.groups()))
        norm = lambda x: " ".join((x or "").split())
        out.setdefault(norm(parts["name"]), set()).add((norm(parts["ret"]), norm(parts["params"])))
    return out



def _params(sig: str) -> list[str]:
    """Split a parameter list, ignoring nesting inside generics and defaults."""
    out, depth, cur = [], 0, ""
    for ch in sig:
        if ch in "<([{":
            depth += 1
        elif ch in ">)]}":
            depth -= 1
        if ch == "," and depth == 0:
            out.append(cur.strip())
            cur = ""
        else:
            cur += ch
    if cur.strip():
        out.append(cur.strip())
    return [p for p in out if p and p != "*"]


def classify_change(lang: str, old: tuple[str, str], new: tuple[str, str]) -> tuple[str, str]:
    """(severity, label) for one signature change. severity is 'break' or 'warn'.

    Not every signature change breaks a caller, and a check that says it does is
    a check people switch off. Narrowing a return from `unknown` to a concrete
    type is safe for every TypeScript consumer; a Python return annotation is not
    enforced at all. Adding an optional parameter still compiles everywhere — but
    it changes the method's metadata token, so a C# consumer that does not
    recompile breaks at runtime. That last case is real and easy to miss.
    """
    old_ret, old_par = old
    new_ret, new_par = new
    if old_par != new_par:
        op, np_ = _params(old_par), _params(new_par)
        added_optional = (np_[:len(op)] == op
                          and all("=" in a for a in np_[len(op):]))
        if added_optional:
            if lang == "csharp":
                return "warn", "optional parameters added — source compatible, but binary breaking for consumers that do not recompile"
            return "warn", "optional parameters added — source compatible"
        return "break", "parameter list changed"
    if old_ret != new_ret:
        if lang == "javascript" and "unknown" in old_ret and "unknown" not in new_ret:
            return "warn", "return narrowed from unknown — safe for consumers"
        if lang == "python":
            return "warn", "return annotation changed — not enforced at runtime, but check the value did not change"
        return "break", "return type changed"
    return "warn", "signature reordered"


def cmd_surface(args) -> int:
    """Diff the public API surface against a released tag.

    The check that answers "is this release breaking?". Reading the changelog
    cannot answer it, and neither can the compiler: adding an optional parameter
    in C# still compiles for every caller but breaks any consumer that does not
    recompile. That is exactly how a binary-breaking change reached an
    otherwise-additive release in 2026-07.
    """
    repo = Path(args.repo).resolve()
    if repo.name in NOT_APPLICABLE:
        print(f"{repo.name}: not applicable — {NOT_APPLICABLE[repo.name]}")
        return 0
    lang = args.lang or detect_lang(repo)
    if not lang:
        die(f"cannot detect SDK language in {repo}")
    cfg = LANGS[lang]
    if "sig_re" not in cfg:
        print(f"{repo.name} [{lang}] — no signature pattern; nothing to compare.")
        return 0
    verify_rev(repo, args.rev)

    files = source_files(repo, cfg)
    rel = [str(f.relative_to(repo)) for f in files]
    old_text = "".join(
        subprocess.run(["git", "-C", str(repo), "show", f"{args.rev}:{r}"],
                       capture_output=True, text=True).stdout for r in rel)
    new_text = "".join(f.read_text(encoding="utf-8", errors="replace") for f in files)

    old, new = public_surface(old_text, cfg), public_surface(new_text, cfg)
    removed = sorted(set(old) - set(new))
    added = sorted(set(new) - set(old))
    changed = [k for k in sorted(set(old) & set(new)) if old[k] - new[k]]

    print(f"{repo.name} [{lang}] — {len(old)} public method(s) at {args.rev}, {len(new)} now")

    if removed:
        print(f"\nREMOVED ({len(removed)}) — every caller breaks:")
        for k in removed:
            print(f"   {k}")
    breaks, warns = [], []
    for k in changed:
        gone = sorted(old[k] - new[k])
        now = sorted(new[k] - old[k])
        sev, label = ("break", "signature removed")
        if len(gone) == 1 and len(now) == 1:
            sev, label = classify_change(lang, gone[0], now[0])
        (breaks if sev == "break" else warns).append((k, label, gone, now))

    def show(rows: list) -> None:
        for k, label, gone, now in rows:
            print(f"   {k} — {label}")
            for ret, params in gone:
                print(f"       was: {ret} ({params[:100]})")
            for ret, params in now:
                print(f"       now: {ret} ({params[:100]})")

    if breaks:
        print(f"\nBREAKING ({len(breaks)}) — a released signature no longer works:")
        show(breaks)
    if warns and not args.quiet_warn:
        print(f"\nWARN — CHANGED ({len(warns)}) — compatible, but verify:")
        show(warns)
    if added and not args.quiet_added:
        print(f"\nADDED ({len(added)}):")
        print("   " + ", ".join(added))

    if removed or breaks:
        print(f"\n{len(removed) + len(breaks)} breaking change(s). Keep the old form and add "
              "alongside it, or bump the major.")
        return 1
    print("\nno breaking change — every released signature still works")
    return 0


# ── Model completeness ───────────────────────────────────────────────────────
# Only for SDKs whose models are written by hand. Where a generator owns the
# models, regeneration keeps them in step and this check has nothing to say.
MODEL_LANGS = {
    "csharp": {
        "glob": "src/Seclai/Models/*.cs",
        "class_re": r"public (?:sealed |abstract )?class (\w+)",
        "prop_re": r'\[JsonPropertyName\("([^"]+)"\)\]',
    },
}


def git_location(spec: Path, fallback: Path) -> tuple[Path, str]:
    """(git work tree, path relative to its root) for a spec file.

    `git show REV:path` resolves `path` from the repo ROOT, so pointing -C at
    the file's own directory silently fails. seclai-csharp bundles no spec and
    is always audited with --spec pointing into seclai-python, so this is the
    normal case, not the edge case.
    """
    if not spec.is_absolute():
        return fallback, str(spec)
    try:
        top = subprocess.check_output(
            ["git", "-C", str(spec.parent), "rev-parse", "--show-toplevel"],
            stderr=subprocess.DEVNULL, text=True).strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        die(f"{spec} is not inside a git work tree; --since needs one")
    root = Path(top)
    return root, str(spec.resolve().relative_to(root.resolve()))


def spec_schema_index(spec: dict) -> dict[str, list[tuple[str, set[str]]]]:
    """Short schema name -> [(full name, property names)].

    The spec qualifies some schemas by module (`routers__api__agents__X`), and
    two modules can export the same trailing name, so a short name may resolve
    to more than one schema.
    """
    index: dict[str, list[tuple[str, set[str]]]] = {}
    for full, sch in (spec.get("components", {}).get("schemas") or {}).items():
        short = full.rsplit("__", 1)[-1]
        index.setdefault(short, []).append((full, set(sch.get("properties") or {})))
    return index


def cmd_models(args) -> int:
    repo = Path(args.repo).resolve()
    lang = args.lang or detect_lang(repo)
    cfg = MODEL_LANGS.get(lang)
    if not cfg:
        print(f"{repo.name} [{lang}] — models are generated; nothing to check by hand.")
        return 0

    spec = load_spec(args.rev, args.spec, repo)
    index = spec_schema_index(spec)

    # `--since` narrows the report to properties the schema GAINED since a rev.
    # Without it the C# models show 27 findings, every one a coverage gap that
    # predates this sync — a signal nobody can act on, and a gate nobody keeps.
    # With it the question becomes the one that actually bit: did this sync
    # leave a model behind? Six were, in 2026-07, and only review caught them.
    gained: dict[str, set[str]] | None = None
    if args.since:
        spec_repo, rel = git_location(Path(args.spec), repo)
        before = spec_schema_index(load_spec(args.since, rel, spec_repo))
        gained = {}
        for short, cands in index.items():
            now = set.union(*(p for _, p in cands))
            was = set.union(*(p for _, p in before[short])) if short in before else set()
            if now - was:
                gained[short] = now - was

    files = sorted(repo.glob(cfg["glob"]))
    scope = f", {len(gained)} schema(s) changed since {args.since}" if gained is not None else ""
    print(f"{repo.name} [{lang}] — {len(files)} model file(s), {len(index)} spec schema names{scope}")

    missing, extra, unmatched = [], [], []
    for f in files:
        text = f.read_text(encoding="utf-8", errors="replace")
        m = re.search(cfg["class_re"], text)
        if not m:
            continue
        cls = m.group(1)
        have = set(re.findall(cfg["prop_re"], text))
        cands = index.get(cls)
        if not cands:
            unmatched.append(cls)
            continue
        # With more than one candidate, only properties common to all of them are
        # unambiguously missing. A gate that guesses is a gate that gets muted.
        want = set.intersection(*(p for _, p in cands))
        if gained is not None:
            want &= gained.get(cls, set())
        gone = want - have
        if gone:
            missing.append((cls, sorted(gone), [n for n, _ in cands]))
        if len(cands) == 1 and gained is None:
            surplus = have - cands[0][1]
            if surplus:
                extra.append((cls, sorted(surplus)))

    if missing:
        print(f"\nMISSING PROPERTIES ({len(missing)}) — the spec declares these and the model does not:")
        for cls, props, full in missing:
            where = f"  [{', '.join(full)}]" if full != [cls] else ""
            print(f"   {cls}{where}\n       {', '.join(props)}")
    if extra:
        print(f"\nWARN — EXTRA PROPERTIES ({len(extra)}) — in the model, not in the schema:")
        for cls, props in extra:
            print(f"   {cls}: {', '.join(props)}")
    if unmatched and not args.quiet_unmatched and gained is None:
        print(f"\nWARN — NOT IN SPEC ({len(unmatched)}) — model classes with no matching schema:")
        print("   " + ", ".join(sorted(unmatched)))

    if unmatched:
        # Not a finding: the spec declares some request bodies inline rather than
        # as a named component, so CreateSourceRequest and UpdateSourceRequest —
        # both of which WERE left stale in 2026-07 — have nothing to match
        # against. Say so, so the count is never read as full coverage.
        print(f"\n   note: {len(unmatched)} model class(es) have no named schema "
              f"and are unchecked (inline request bodies land here).")

    if missing:
        print(f"\n{len(missing)} model(s) behind the spec.")
        return 1
    print("\nall models carry every property their schema declares")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(prog="sdksync", description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)

    p = sub.add_parser("parity", help="spec paths with no request call in the client")
    p.add_argument("repo", nargs="?", default=".")
    p.add_argument("--spec", default="openapi/seclai.openapi.json")
    p.add_argument("--rev", help="read the spec from this git rev instead of the working tree")
    p.add_argument("--lang", choices=list(LANGS))
    p.add_argument("--quiet-partial", action="store_true", help="suppress the best-effort verb warnings")
    p.set_defaults(func=cmd_parity)

    p = sub.add_parser("params", help="query params the client sends that the endpoint does not declare")
    p.add_argument("repo", nargs="?", default=".")
    p.add_argument("--spec", default="openapi/seclai.openapi.json")
    p.add_argument("--rev", help="read the spec from this git rev instead of the working tree")
    p.add_argument("--lang", choices=list(LANGS))
    p.add_argument("--quiet-unexposed", action="store_true",
                   help="suppress the declared-but-never-sent report")
    p.set_defaults(func=cmd_params)

    p = sub.add_parser("returns", help="client return types that disagree with the spec response")
    p.add_argument("repo", nargs="?", default=".")
    p.add_argument("--spec", default="openapi/seclai.openapi.json")
    p.add_argument("--rev", help="read the spec from this git rev instead of the working tree")
    p.add_argument("--lang", choices=list(LANGS))
    p.add_argument("--quiet-untyped", action="store_true",
                   help="suppress the returns-a-blob report")
    p.add_argument("--quiet-renamed", action="store_true",
                   help="suppress the same-shape-different-name report")
    p.set_defaults(func=cmd_returns)

    p = sub.add_parser("surface", help="public API surface diff against a released tag")
    p.add_argument("rev", help="git rev of the last release, e.g. 1.3.0 or v1.5.0")
    p.add_argument("repo", nargs="?", default=".")
    p.add_argument("--lang", choices=list(LANGS))
    p.add_argument("--quiet-added", action="store_true", help="suppress the added-methods list")
    p.add_argument("--quiet-warn", action="store_true", help="suppress the compatible-change report")
    p.set_defaults(func=cmd_surface)

    p = sub.add_parser("models", help="hand-written models missing properties their schema declares")
    p.add_argument("repo", nargs="?", default=".")
    p.add_argument("--spec", default="openapi/seclai.openapi.json")
    p.add_argument("--rev", help="read the spec from this git rev instead of the working tree")
    p.add_argument("--lang", choices=list(LANGS))
    p.add_argument("--since", metavar="REV",
                   help="only report properties the schema gained since this spec rev "
                        "(the sync gate; without it every long-standing gap is listed)")
    p.add_argument("--quiet-unmatched", action="store_true",
                   help="suppress the model-classes-with-no-schema report")
    p.set_defaults(func=cmd_models)

    p = sub.add_parser("spec-diff", help="paths/schemas added, removed or changed between revisions")
    p.add_argument("old", help="git rev of the older spec")
    p.add_argument("new", nargs="?", help="git rev of the newer spec (default: working tree)")
    p.add_argument("--repo", default=".")
    p.add_argument("--spec", default="openapi/seclai.openapi.json")
    p.set_defaults(func=cmd_spec_diff)

    p = sub.add_parser("api-delta", help="public client methods added/removed between revisions")
    p.add_argument("old", help="git rev")
    p.add_argument("new", nargs="?", help="git rev (default: working tree)")
    p.add_argument("--repo", default=".")
    p.add_argument("--lang", choices=list(LANGS))
    p.set_defaults(func=cmd_api_delta)

    args = ap.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
