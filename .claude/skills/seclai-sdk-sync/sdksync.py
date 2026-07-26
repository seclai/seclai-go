#!/usr/bin/env python3
"""Analysis helpers for syncing a Seclai SDK to a new OpenAPI spec.

Subcommands:
  parity     spec paths that have no request call in the hand-written client
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

# ── Language table ───────────────────────────────────────────────────────────
# `sources` are HAND-WRITTEN client files only. Generated trees must never be
# scanned: they contain a module per endpoint and would make parity always pass.
LANGS = {
    "python": {
        "detect": "seclai/seclai.py",
        "sources": ["seclai/seclai.py"],
        "method_re": r"^[ \t]+(?:async )?def ([a-z][a-z0-9_]*)\(",
        "verb_re": r'"(GET|POST|PUT|PATCH|DELETE)"',
    },
    "javascript": {
        "detect": "src/client.ts",
        "sources": ["src/client.ts"],
        "method_re": r"^[ \t]+(?:async )?\*?([a-zA-Z_][a-zA-Z0-9_]*)\s*\(",
        "verb_re": r'"(GET|POST|PUT|PATCH|DELETE)"',
    },
    "go": {
        "detect": "client.go",
        "sources": ["*.go"],
        "exclude": ["*_test.go"],
        "method_re": r"^func \(c \*Client\) ([A-Z][A-Za-z0-9]*)\(",
        "verb_re": r"http\.Method(Get|Post|Put|Patch|Delete)",
    },
    "csharp": {
        "detect": "src/Seclai/SeclaiClient.cs",
        "sources": ["src/Seclai/*.cs"],
        "method_re": r"public (?:async )?[\w<>,?\[\]. ]+ ([A-Z][A-Za-z0-9]*)\s*\(",
        "verb_re": r"HttpMethod\.(Get|Post|Put|Patch|Delete)",
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
    for m in re.finditer(r"""["'`](/[A-Za-z0-9_\-/{}$%.]*)["'`]""", text):
        p = normalise(m.group(1))
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
