#!/usr/bin/env python3
"""Validate a Common Changelog file as used by the Seclai SDK repos.

Usage: python3 validate.py [CHANGELOG.md]

Exits non-zero and prints one line per problem. Warnings do not affect the
exit code.
"""
import re
import sys

GROUPS = ["Changed", "Added", "Removed", "Fixed"]
RELEASE_RE = re.compile(r"^## \[([^\]]+)\] - (\d{4}-\d{2}-\d{2})\s*$")
GROUP_RE = re.compile(r"^### (.+?)\s*$")
LINKDEF_RE = re.compile(r"^\[([^\]]+)\]:\s+(\S+)\s*$")
SEMVER_RE = re.compile(r"^(\d+)\.(\d+)\.(\d+)$")


def semver_key(v):
    m = SEMVER_RE.match(v)
    return tuple(int(g) for g in m.groups()) if m else (-1, -1, -1)


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else "CHANGELOG.md"
    try:
        lines = open(path, encoding="utf-8").read().split("\n")
    except OSError as e:
        print(f"error: cannot read {path}: {e}")
        return 1

    errors, warnings = [], []
    releases, linkdefs = [], []
    cur = None
    seen_groups = []
    group_entries = []
    in_group = None

    def close_group():
        if in_group is None:
            return
        if not group_entries:
            errors.append(f"{cur}: '### {in_group}' has no entries")
        breaking = [i for i, e in enumerate(group_entries) if e.startswith("**Breaking:**")]
        if breaking and breaking != list(range(len(breaking))):
            errors.append(f"{cur}: breaking changes must sort first under '### {in_group}'")

    if not lines or lines[0].strip() != "# Changelog":
        errors.append("file must start with '# Changelog'")

    for n, line in enumerate(lines, 1):
        if line.strip().lower().startswith("## [unreleased") or line.strip().lower() == "## unreleased":
            errors.append(f"line {n}: Common Changelog has no Unreleased section")
            continue

        m = RELEASE_RE.match(line)
        if m:
            close_group()
            cur, seen_groups, in_group, group_entries = m.group(1), [], None, []
            if not SEMVER_RE.match(cur):
                errors.append(f"line {n}: '{cur}' is not a bare semver version")
            releases.append(cur)
            continue

        if line.startswith("## "):
            errors.append(f"line {n}: malformed release heading: {line.strip()!r}")
            continue

        m = GROUP_RE.match(line)
        if m:
            close_group()
            g = m.group(1)
            in_group, group_entries = g, []
            if cur is None:
                errors.append(f"line {n}: '### {g}' before any release heading")
                continue
            if g not in GROUPS:
                errors.append(f"{cur}: unknown group '{g}' (allowed: {', '.join(GROUPS)})")
                continue
            i = GROUPS.index(g)
            if g in [GROUPS[j] for j in seen_groups]:
                errors.append(f"{cur}: duplicate group '{g}'")
            elif seen_groups and i < seen_groups[-1]:
                errors.append(
                    f"{cur}: '{g}' out of order — required order is {' > '.join(GROUPS)}"
                )
            seen_groups.append(i)
            continue

        m = LINKDEF_RE.match(line)
        if m:
            linkdefs.append(m.group(1))
            continue

        if line.startswith("- ") and in_group is not None:
            entry = line[2:].strip()
            group_entries.append(entry)
            if entry.endswith("."):
                warnings.append(f"{cur}: entry ends with a period: {entry[:60]!r}")

    close_group()

    ordered = sorted(releases, key=semver_key, reverse=True)
    if releases != ordered:
        errors.append(f"releases not sorted latest-first: {' '.join(releases)}")

    for v in releases:
        c = linkdefs.count(v)
        if c == 0:
            errors.append(f"{v}: missing link definition at the bottom of the file")
        elif c > 1:
            errors.append(f"{v}: {c} link definitions, expected 1")
    for d in linkdefs:
        if d not in releases:
            errors.append(f"orphan link definition [{d}] with no matching release")

    for w in warnings:
        print(f"warning: {w}")
    for e in errors:
        print(f"error: {e}")

    if errors:
        print(f"\n{len(errors)} error(s) in {path}")
        return 1
    print(f"{path}: OK — {len(releases)} releases, format valid")
    return 0


if __name__ == "__main__":
    sys.exit(main())
