#!/usr/bin/env python3
"""Compile the README examples of a Seclai SDK. Never executes them.

    docexamples.py list [repo]     every fence, with its marker state
    docexamples.py check [repo]    compile the marked fences

Opt-in by marker. Most fences are deliberate fragments — 3 of 39 TypeScript
fences and 2 of 34 Go fences carry an import — so compiling everything would
mean rewriting the READMEs first. Mark a fence by putting this immediately
above it; it is an HTML comment, so it stays invisible on GitHub, npm and
pkg.go.dev:

    <!-- sdksync:check -->

`list` prints the marked fraction so coverage is a number that can be driven up
over time rather than a cliff to be turned off.

Unlike sdksync.py this shells out to the repo's own toolchain, so it only runs
where that toolchain exists. It is deliberately a separate script: a doc check
that needs `tsc` must never be able to break the spec audit that does not.
"""
from __future__ import annotations

import argparse
import re
import shutil
import subprocess
import sys
from pathlib import Path

MARKER = "<!-- sdksync:check -->"
SCRATCH = ".doccheck"

LANGS = {
    "javascript": {"detect": "src/client.ts", "fence": "ts", "tool": "npx"},
    "go": {"detect": "client.go", "fence": "go", "tool": "go"},
}


def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    raise SystemExit(2)


def detect(repo: Path) -> str | None:
    for name, cfg in LANGS.items():
        if (repo / cfg["detect"]).exists():
            return name
    return None


def fences(readme: str, tag: str) -> list[tuple[int, bool, str]]:
    """(1-indexed start line, marked, body) for every ```<tag> fence."""
    out = []
    for m in re.finditer(rf"```{tag}\n(.*?)```", readme, re.S):
        line = readme[:m.start()].count("\n") + 1
        before = readme[:m.start()].rstrip().rsplit("\n", 1)[-1].strip()
        out.append((line, before == MARKER, m.group(1)))
    return out


# ── per-language compilation ─────────────────────────────────────────────────
def check_javascript(repo: Path, marked: list[tuple[int, str]]) -> list[str]:
    scratch = repo / SCRATCH
    shutil.rmtree(scratch, ignore_errors=True)
    scratch.mkdir()
    # (README line of the fence, number of lines prepended before the body).
    origin: list[tuple[int, int]] = []
    try:
        for i, (line, body) in enumerate(marked):
            parts = []
            if "import " not in body:
                parts.append('import { Seclai } from "../src/index";')
            if not re.search(r"\b(const|let|var)\s+client\b", body):
                parts.append("declare const client: Seclai;")
            # Wrapped so top-level `await` is legal in a fragment.
            parts.append("export async function _example() {")
            origin.append((line, len(parts)))
            parts += [body, "}"]
            (scratch / f"ex{i}.ts").write_text("\n".join(parts))

        r = subprocess.run(
            ["npx", "tsc", "--noEmit", "--strict", "--skipLibCheck",
             "--target", "es2022", "--module", "esnext",
             "--moduleResolution", "bundler", "--lib", "es2022,dom",
             "--noUnusedLocals", "false",
             *[str(p) for p in sorted(scratch.glob("*.ts"))]],
            cwd=repo, capture_output=True, text=True)
        if r.returncode == 0:
            return []
        # Map "ex3.ts(12,5): error ..." back to a README line number. SCRATCH is
        # escaped because it starts with a `.`, which as a regex metachar would
        # match any character. The tsc line number is used rather than discarded:
        # without it every error in a fence pointed at the fence header, so a
        # 30-line example gave the reader one line number for all of them.
        pat = re.compile(rf"^{re.escape(SCRATCH)}/ex(\d+)\.ts\((\d+),\d+\):\s*(.*)$")
        out = []
        for ln in (r.stdout + r.stderr).splitlines():
            m = pat.match(ln)
            if m:
                readme_line, prefix = origin[int(m.group(1))]
                # Body line 1 sits at generated line prefix+1 and at README line
                # readme_line+1, so the two differ by exactly `prefix`.
                out.append(f"README.md:{max(readme_line, readme_line + int(m.group(2)) - prefix)}"
                           f" — {m.group(3)}")
            elif ln.strip():
                out.append(ln)
        return out
    finally:
        shutil.rmtree(scratch, ignore_errors=True)


def check_go(repo: Path, marked: list[tuple[int, str]]) -> list[str]:
    gomod = (repo / "go.mod").read_text()
    module = re.search(r"^module\s+(\S+)", gomod, re.M)
    if not module:
        die("cannot read module path from go.mod")
    scratch = repo / SCRATCH
    shutil.rmtree(scratch, ignore_errors=True)
    scratch.mkdir()
    try:
        (scratch / "go.mod").write_text(
            f"module doccheck\n\ngo 1.22\n\n"
            f"require {module.group(1)} v0.0.0\n\n"
            f"replace {module.group(1)} => ..\n")
        if (repo / "go.sum").exists():
            shutil.copy(repo / "go.sum", scratch / "go.sum")

        # Every example goes into one file, so remember where each body landed.
        # `go build` reports `./main.go:42:3: ...` and the finally block deletes
        # main.go, so an unmapped diagnostic points the reader at a file that no
        # longer exists. (first generated line, last generated line, README line)
        text = ("package main\n\nimport (\n\t\"context\"\n\t\"fmt\"\n\n"
                f"\t\"{module.group(1)}\"\n)\n\n"
                "var (\n\tctx = context.Background()\n\t_   = fmt.Sprint\n"
                f"\tclient, _ = seclai.NewClient(seclai.Options{{APIKey: \"k\"}})\n)\n\n")
        spans: list[tuple[int, int, int]] = []
        for i, (line, b) in enumerate(marked):
            if i:
                text += "\n\n"
            text += f"func _example{i}() {{\n"
            first = text.count("\n") + 1
            text += f"{b}\n"
            spans.append((first, text.count("\n"), line))
            text += "}"
        text += "\n\nfunc main() {}\n"
        (scratch / "main.go").write_text(text)

        subprocess.run(["go", "mod", "tidy"], cwd=scratch,
                       capture_output=True, text=True)
        r = subprocess.run(["go", "build", "./..."], cwd=scratch,
                           capture_output=True, text=True)
        if r.returncode == 0:
            return []
        pat = re.compile(r"^\.?/?main\.go:(\d+):(?:\d+:)?\s*(.*)$")
        out = []
        for ln in (r.stdout + r.stderr).splitlines():
            m = pat.match(ln.strip())
            if not m:
                if ln.strip():
                    out.append(ln)
                continue
            gen = int(m.group(1))
            hit = next(((f, la, rl) for f, la, rl in spans if f <= gen <= la), None)
            if hit is None:
                out.append(ln)
            else:
                first, _last, readme_line = hit
                out.append(f"README.md:{readme_line + gen - first + 1} — {m.group(2)}")
        return out
    finally:
        shutil.rmtree(scratch, ignore_errors=True)


CHECKERS = {"javascript": check_javascript, "go": check_go}


def main() -> int:
    ap = argparse.ArgumentParser(prog="docexamples", description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name, help_ in (("list", "every fence and its marker state"),
                        ("check", "compile the marked fences")):
        p = sub.add_parser(name, help=help_)
        p.add_argument("repo", nargs="?", default=".")
        p.add_argument("--readme", default="README.md")

    args = ap.parse_args()
    repo = Path(args.repo).resolve()
    lang = detect(repo)
    if not lang:
        print(f"{repo.name}: no TypeScript or Go client here — nothing to compile")
        return 0

    readme = repo / args.readme
    if not readme.exists():
        die(f"no {args.readme} in {repo}")
    found = fences(readme.read_text(), LANGS[lang]["fence"])
    marked = [(line, body) for line, mark, body in found if mark]

    if args.cmd == "list":
        print(f"{repo.name} [{lang}] — {len(found)} fence(s), "
              f"{len(marked)} marked ({len(marked) * 100 // max(len(found), 1)}%)")
        for line, mark, _ in found:
            print(f"   README.md:{line:<5} {'checked' if mark else '-'}")
        return 0

    if not marked:
        print(f"{repo.name} [{lang}] — no fences marked with {MARKER}; nothing to check")
        return 0

    # The docstring promises this "only runs where that toolchain exists"; say so
    # instead of letting subprocess raise FileNotFoundError at the reader.
    tool = LANGS[lang]["tool"]
    if not shutil.which(tool):
        die(f"{tool} is not on PATH; `docexamples check` needs the {lang} toolchain")

    problems = CHECKERS[lang](repo, marked)
    print(f"{repo.name} [{lang}] — compiled {len(marked)} marked fence(s)")
    if problems:
        print(f"\n{len(problems)} problem(s):")
        for p in problems:
            print(f"   {p}")
        return 1
    print("all marked examples compile")
    return 0


if __name__ == "__main__":
    sys.exit(main())
