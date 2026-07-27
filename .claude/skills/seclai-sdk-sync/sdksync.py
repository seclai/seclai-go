#!/usr/bin/env python3
"""Analysis helpers for syncing a Seclai SDK to a new OpenAPI spec.

Subcommands:
  parity     spec paths that have no request call in the hand-written client
  params     query params the client sends that the endpoint does not declare
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
        "detect": "seclai/seclai.py",
        "sources": ["seclai/seclai.py"],
        "method_re": r"^[ \t]+(?:async )?def ([a-z][a-z0-9_]*)\(",
        "verb_re": r'"(GET|POST|PUT|PATCH|DELETE)"',
        "dict_at": [r"params\s*=\s*(?:_strip_none\()?\s*"],
        "dict_key_re": r'"([a-zA-Z_][a-zA-Z0-9_]*)"\s*:',
        "skip": {"request", "request_raw", "stream", "paginate"},
    },
    "javascript": {
        "detect": "src/client.ts",
        "sources": ["src/client.ts"],
        # Exactly two spaces: class members sit at that indent, while statements
        # inside a body sit at four or more. A looser `^[ \t]+` also matches
        # `return (await this.request(...)` and attributes findings to "return".
        "method_re": r"^  (?:async )?\*?([a-zA-Z_][a-zA-Z0-9_]*)\s*[(<]",
        "verb_re": r'"(GET|POST|PUT|PATCH|DELETE)"',
        "dict_at": [r"query:\s*"],
        "dict_key_re": r'["\']?([a-zA-Z_][a-zA-Z0-9_]*)["\']?\s*:',
        "skip": {"request", "requestRaw", "uploadFile", "paginate"},
    },
    "go": {
        "detect": "client.go",
        "sources": ["*.go"],
        "exclude": ["*_test.go"],
        "method_re": r"^func \(c \*Client\) ([A-Z][A-Za-z0-9]*)\(",
        "verb_re": r"http\.Method(Get|Post|Put|Patch|Delete)",
        "key_re": r'q\["([a-zA-Z_][a-zA-Z0-9_]*)"\]\s*=',
        "helpers": {"listQuery(": ["page", "limit"]},
        "skip": {"Do", "buildURL"},
    },
    "csharp": {
        "detect": "src/Seclai/SeclaiClient.cs",
        "sources": ["src/Seclai/*.cs"],
        "method_re": r"public (?:async )?[\w<>,?\[\]. ]+ ([A-Z][A-Za-z0-9]*)\s*\(",
        "verb_re": r"HttpMethod\.(Get|Post|Put|Patch|Delete)",
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


def method_blocks(text: str, method_re: str):
    """Yield (name, body). Each block runs to the next method anchor."""
    ms = list(re.finditer(method_re, text, re.M))
    for i, m in enumerate(ms):
        end = ms[i + 1].start() if i + 1 < len(ms) else len(text)
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

    for f in files:
        text = f.read_text(errors="replace")
        for mname, body in method_blocks(text, cfg["method_re"]):
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
            raw, declared, _required = index[call]
            exposed.setdefault((call[0], raw), set()).update(keys)
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

    errors = len(undeclared) + len(not_in_spec) + len(unparsed)
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

    changed = []
    for p in sorted(np_ & op):
        if json.dumps(old["paths"][p], sort_keys=True) != json.dumps(new["paths"][p], sort_keys=True):
            oo, nn = set(ops(old, p)), set(ops(new, p))
            note = "verbs " + ", ".join(sorted(nn - oo)) if nn - oo else "description/params only"
            changed.append(f"   {p}  ({note})")
    if changed:
        print(f"\nCHANGED PATHS ({len(changed)}):")
        print("\n".join(changed))

    if ns_ - os_:
        print(f"\nADDED SCHEMAS ({len(ns_ - os_)}):")
        for s in sorted(ns_ - os_):
            print(f"   {s}")
    if os_ - ns_:
        print(f"\nREMOVED SCHEMAS ({len(os_ - ns_)}):")
        for s in sorted(os_ - ns_):
            print(f"   {s}")

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
