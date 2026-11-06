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
# Signatures, used by `surface`:
#   sig_re      — a public method's head, ending on its opening `(`. The
#                 parameter list is then sliced by paren matching, never by a
#                 `[^)]*` class: a callback type or a `(default: 2000)` doc
#                 comment would end it early, the method would drop out of the
#                 surface and be reported REMOVED.
#   The return type after the closing `)` is read by `_read_return_tail`, not a
#   regex: `Promise<{ id: string }>` and Go's single, untupled `error` both
#   defeated one.
#   sig_groups  — head groups, then "params", then "ret" when it comes after.
#   type_params — a `<...>` type-parameter list may sit between the name and
#                 `(`; it is read and recorded with the return type.
#   line_comment — the line-comment marker; `public_surface` strips these, and
#                 `/* */` too when it is `//`, from the whole file. Default `//`.
# Endpoint helpers, used by `params` and `returns` (see `request_blocks`):
#   helper_re   — a NON-public method declaration on the client, name captured.
#   helper_call — how the client calls one of its own methods; `{name}` is filled
#                 in. Receiver-qualified so `_client.X(` and `t.c.x(` — a typed
#                 façade calling in from another class — are not followed.
_JS_HELPER_RE = (r"^  (?:(?:private|protected) (?:static )?(?:async )?\*?|(?:async )?\*?#)"
                 r"([a-zA-Z_][a-zA-Z0-9_]*)\s*[(<]")

LANGS = {
    "python": {
        "sig_re": r"\n    (?:async )?def ([a-z]\w*)\(",
        "line_comment": "#",
        "sig_groups": ("name", "params", "ret"),
        "detect": "seclai/seclai.py",
        "sources": ["seclai/seclai.py"],
        "method_re": r"^[ \t]+(?:async )?def ([a-z][a-z0-9_]*)\(",
        # Private helpers start with `_` and so never anchor a block of their
        # own; without this the public method above them swallows their request
        # calls. `api_key`, a plain property, was credited with GET /sources.
        "boundary_re": r"^[ \t]*(?:async )?def |^class ",
        "verb_re": r'"(GET|POST|PUT|PATCH|DELETE)"',
        "return_after_params": True,
        "dict_at": [r"params\s*=\s*(?:_strip_none\()?\s*"],
        "dict_key_re": r'"([a-zA-Z_][a-zA-Z0-9_]*)"\s*:',
        "helper_re": r"^[ \t]+(?:async )?def (_[a-z0-9][a-z0-9_]*)\(",
        "helper_call": r"\bself\.{name}\(",
        "skip": {"request", "request_raw", "stream", "paginate"},
    },
    "javascript": {
        # `async *name(` (an async generator, e.g. `runStreamingAgent`) and
        # `name<T>(` too: either one missing from the pattern left the method out
        # of the surface, so removing it went unreported. The match stops before
        # the `<` or `(`; `public_surface` reads a type-parameter list with
        # `balanced_slice`, so parens and prettier's line wrapping inside it are
        # fine, and records it with the return type (see `_split_type_params`).
        "sig_re": r"\n  (?:async )?\*?(\w+)(?=[<(])",
        "type_params": True,
        "sig_groups": ("name", "params", "ret"),
        "detect": "src/client.ts",
        "sources": ["src/client.ts"],
        # Exactly two spaces: class members sit at that indent, while statements
        # inside a body sit at four or more. A looser `^[ \t]+` also matches
        # `return (await this.request(...)` and attributes findings to "return".
        # The indent alone is not enough: the module-level helpers above the class
        # put `if (` and `for (` at exactly two spaces as well, and without the
        # lookahead `api-delta` lists methods named `if` and `for`.
        "method_re": (r"^  (?:async )?\*?"
                      r"(?!(?:if|for|while|switch|catch|do|else|try|return|new|typeof|await)\b)"
                      r"([a-zA-Z_][a-zA-Z0-9_]*)\s*[(<]"),
        "verb_re": r'"(GET|POST|PUT|PATCH|DELETE)"',
        "return_after_params": True,
        "dict_at": [r"query:\s*"],
        "dict_key_re": r'["\']?([a-zA-Z_][a-zA-Z0-9_]*)["\']?\s*:',
        # Read a query literal member by member rather than with dict_key_re:
        # `{ download }` and `{ offset, limit: opts.limit }` send keys the regex
        # cannot see (both were reported UNEXPOSED), and a `...spread` or
        # `[computed]` member sends keys nobody can read here — UNPARSED, never
        # clean. See `object_literal_keys`.
        "dict_members": True,
        # `private`/`protected`/`#name` members only. An unmodified `_name(` is
        # matched by method_re and already audited as a block of its own.
        "helper_re": _JS_HELPER_RE,
        # method_re skips helpers, so without this a method's block runs on into
        # the helper below it and takes that helper's request as its own.
        "boundary_re": _JS_HELPER_RE,
        "helper_call": r"\bthis\.#?{name}\s*[(<]",
        "skip": {"request", "requestRaw", "uploadFile", "paginate"},
    },
    "go": {
        # Exported only: a lowercase method is unreachable outside the package,
        # so a change to one breaks no caller. `alertConfigsBody` and
        # `modelAlertsBody` were graded BREAKING for that reason alone.
        "sig_re": r"func \(c \*Client\) ([A-Z]\w*)\(",
        "sig_groups": ("name", "params", "ret"),
        "detect": "client.go",
        "sources": ["*.go"],
        "exclude": ["*_test.go"],
        "method_re": r"^func \(c \*Client\) ([A-Z][A-Za-z0-9]*)\(",
        # Free functions and type decls end a block; without this the helpers
        # between two client methods are read as part of the earlier one.
        "boundary_re": r"^(?:func|type|var|const)\b",
        "verb_re": r"http\.Method(Get|Post|Put|Patch|Delete)",
        # Go returns `(*T, error)`; `block_return` takes the tuple's first element.
        "return_after_params": True,
        "key_re": r'q\["([a-zA-Z_][a-zA-Z0-9_]*)"\]\s*=',
        # Go builds a query either by subscript assignment (key_re) or as a map
        # literal. Reading only the former made Search and SearchDocs — whose
        # required `q` is set in the literal — report as never sending it, the
        # false positive twin of the bug this check exists to find.
        "dict_at": [r"map\[string\]string"],
        "dict_key_re": r'"([a-zA-Z_][a-zA-Z0-9_]*)"\s*:',
        # Package-level functions that build and return a query map, such as
        # `listQuery(page, limit)` and `sortableListQuery(opts)`. Their keys are
        # read from their own bodies (see `query_builders`) rather than listed
        # here: a hard-coded list missed `sortableListQuery`, so every method
        # using it was reported as never sending page/limit/sort/order.
        "builder_re": r"^func ([a-z][A-Za-z0-9]*)\([^)]*\)\s*map\[string\]string\s*\{",
        "helper_re": r"^func \(c \*Client\) ([a-z][A-Za-z0-9]*)\(",
        "helper_call": r"(?<![\w.])c\.{name}\(",
        # The receiver type scopes a Go method, not the file it sits in.
        "helper_scope": "package",
        "skip": {"Do", "buildURL"},
    },
    "csharp": {
        "sig_re": r"public (?:async )?([\w<>,?\[\]. ]+?) ([A-Z]\w*)\(",
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
        "helper_re": (r"^    (?:(?:private|internal|protected|static|async) )+"
                      r"[\w<>,?\[\]. ]+ ([A-Z][A-Za-z0-9]*)\s*\("),
        "helper_call": r"(?<![\w.])(?:this\.)?{name}\s*\(",
        "skip": {"SendJsonAsync", "SendNoContentAsync", "SendRawAsync", "BuildUri", "PaginationQuery"},
    },
}

# The scanner each language's source needs (see `strip_comments`).
for _name, _cfg in LANGS.items():
    _cfg["lexer"] = _name

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
def _quoted_end(text: str, i: int) -> int | None:
    """Index just past the string literal opening at `text[i]`, or None.

    Only a literal that closes on the same line counts. Every scanner below runs
    over raw source, comments included, so an apostrophe in `// don't` must stay
    an ordinary character rather than open a string that swallows the rest of
    the block. A backslash escapes the next character.
    """
    q = text[i]
    j = i + 1
    while j < len(text):
        ch = text[j]
        if ch == "\\":
            j += 2
            continue
        if ch == q:
            return j + 1
        if ch == "\n":
            return None
        j += 1
    return None


def _norm(x: str | None) -> str:
    """Whitespace-normalised text: every run of whitespace becomes one space."""
    return " ".join((x or "").split())


def _norm_ts(x: str | None) -> str:
    """`_norm` for a piece of TypeScript signature, with prettier's wrapped angle
    brackets joined first: `Record<\n  string,\n  unknown,\n>` becomes
    `Record<string, unknown>`, as it is written on one line.

    Plain `_norm` turned the wrapped closer's line break into a space on each
    side of the `>`, which `_bracket` then read as a comparison, so a later scan
    of the normalised text lost its depth. It assumes a `<` ending a line and a
    `>` starting one are brackets. That is wrong for a comparison broken across
    lines in a parameter default (`a <\n b`): the join then miscounts depth, which
    can only produce a false BREAKING, never a missed one. No SDK file has it."""
    x = re.sub(r"<[ \t]*\n\s*", "<", x or "")
    x = re.sub(r",?\s*\n[ \t]*>", ">", x)
    return _norm(x)


def _bracket(s: str, j: int) -> int:
    """+1 if `s[j]` opens a bracket, -1 if it closes one, 0 otherwise.

    The one place that decides whether `<` and `>` are brackets, shared by every
    depth-counting scanner here. They are, as generics (`Map<K, List<V>>`), except:
    `=>` and `->` (an arrow), `<=` and `>=`, `<-` (Go's `<-chan`, `chan<-`), a
    shift `<<`, and a `<` or `>` with a space on both sides (`a > b` in a default
    value). Counting those drove the depth negative or left it open, so the
    commas after them stopped splitting and arity came out wrong.

    A `>` that starts its line is a closer even with a space on both sides:
    prettier wraps a long generic as `Promise<\n    A | B\n  > {`, and reading
    that `>` as a comparison left the return type open, so the method fell out
    of the surface and removing it reported nothing. A comparison does not start
    a line: prettier breaks a binary expression after its operator.
    """
    c = s[j]
    if c in "([{":
        return 1
    if c in ")]}":
        return -1
    if c not in "<>":
        return 0
    prev = s[j - 1] if j else ""
    nxt = s[j + 1] if j + 1 < len(s) else ""
    if prev == " " and nxt == " " and not (c == ">" and _starts_line(s, j)):
        return 0
    if c == "<":
        return 0 if (nxt in "-=<" or prev == "<") else 1
    return 0 if (prev in "=-" or nxt == "=") else -1


def _starts_line(s: str, j: int) -> bool:
    """True when only spaces or tabs precede `s[j]` on its line."""
    k = j - 1
    while k >= 0 and s[k] in " \t":
        k -= 1
    return k < 0 or s[k] == "\n"


def balanced_slice(text: str, at: int, opener: str = "{", closer: str = "}") -> str | None:
    """Return the bracket-delimited literal starting at or after `at`.

    Regex cannot do this: query literals span lines and nest, and a non-greedy
    match silently truncates at the first inner `}`. Brackets inside a string
    literal (`fmt?: "{id}"`, `sep = ")"`) are text, not structure. For `<`/`>`,
    which of them count is `_bracket`'s call: the `>` of `=>` does not.
    """
    i = text.find(opener, at)
    if i == -1:
        return None
    angle = opener == "<"
    depth, j = 0, i
    while j < len(text):
        ch = text[j]
        if ch in "\"'`":
            end = _quoted_end(text, j)
            if end is not None:
                j = end
                continue
        if ch == opener and (not angle or _bracket(text, j) == 1):
            depth += 1
        elif ch == closer and (not angle or _bracket(text, j) == -1):
            depth -= 1
            if depth == 0:
                return text[i + 1: j]
        j += 1
    return None


def _triple_end(text: str, i: int) -> int | None:
    """Index just past the Python triple-quoted string opening at `text[i]`."""
    q = text[i:i + 3]
    j = i + 3
    while j < len(text):
        if text[j] == "\\":
            j += 2
            continue
        if text.startswith(q, j):
            return j + 3
        j += 1
    return None


def _template_end(text: str, i: int) -> int | None:
    """Index just past the JavaScript template literal opening at `text[i]`.
    It may span lines, and a `${...}` inside may hold strings and templates."""
    j, n = i + 1, len(text)
    while j < n:
        ch = text[j]
        if ch == "\\":
            j += 2
            continue
        if ch == "`":
            return j + 1
        if text.startswith("${", j):
            depth, j = 1, j + 2
            while j < n and depth:
                c = text[j]
                if c == "`":
                    end = _template_end(text, j)
                    if end is None:
                        return None
                    j = end
                    continue
                if c in "\"'":
                    end = _quoted_end(text, j)
                    j = end if end is not None else j + 1
                    continue
                depth += 1 if c == "{" else -1 if c == "}" else 0
                j += 1
            continue
        j += 1
    return None


# After one of these characters or words a `/` starts a regex literal, not a division.
_REGEX_AFTER_CHARS = set("(,=:[!&|?{};+-*%<>~^")
_REGEX_AFTER_WORDS = {"return", "typeof", "case", "do", "else", "in", "of", "new",
                      "delete", "void", "throw", "yield", "await", "instanceof"}


def _regex_end(text: str, i: int, before: str) -> int | None:
    """Index just past the JavaScript regex literal opening at `text[i]`, or None
    when the `/` is a division. `before` is the code preceding it.

    Which one a `/` is depends on the grammar; this is the usual previous-token
    heuristic. It is still a limit: a regex directly after `)` (`if (x) /re/.test(s)`)
    or `]` is read as a division, and its body as code. A literal must close on
    its own line, as the grammar requires.
    """
    if text.startswith(("//", "/*"), i):
        return None
    tail = before.rstrip()
    word = re.search(r"[A-Za-z_$][\w$]*$", tail)
    if tail and tail[-1] not in _REGEX_AFTER_CHARS and not (word and word.group() in _REGEX_AFTER_WORDS):
        return None
    j, in_class = i + 1, False
    while j < len(text):
        c = text[j]
        if c == "\\":
            j += 2
            continue
        if c == "\n":
            return None
        if in_class:
            in_class = c != "]"
        elif c == "[":
            in_class = True
        elif c == "/":
            j += 1
            while j < len(text) and (text[j].isalnum() or text[j] in "_$"):
                j += 1
            return j
        j += 1
    return None


def strip_comments(text: str, line: str = "//", lexer: str | None = None,
                   drop_triple: bool = False) -> str:
    """`text` with `/* ... */` and `line`-to-end-of-line comments replaced by a
    space. String literals are left intact, so `"https://x"` is not a comment.

    Python passes `line="#"` and has no block comments; `/*` there would be
    an operator sequence, never a comment, so it is only honoured for `//`.

    `lexer` (a `LANGS` key) adds the literals that span lines or hold comment
    markers, so a `/*` in one cannot swallow method heads up to the next `*/`:
    Python triple-quoted strings (replaced by `""` with `drop_triple`, which is
    how docstrings are kept out of request scanning), JavaScript template and
    regex literals, Go raw strings. Without it, a literal must close on its own
    line. C# verbatim strings are a known limit.
    """
    out, j, n = [], 0, len(text)
    recent = ""   # the last code emitted, for the regex-literal heuristic
    while j < n:
        ch = text[j]
        end = None
        if lexer == "python" and text.startswith(('"""', "'''"), j):
            end = _triple_end(text, j)
            if end is not None:
                out.append('""' if drop_triple else text[j:end])
                recent, j = '""', end
                continue
        if ch == "`" and lexer == "javascript":
            end = _template_end(text, j)
        elif ch == "`" and lexer == "go":
            close = text.find("`", j + 1)
            end = None if close == -1 else close + 1
        elif ch in "\"'`":
            end = _quoted_end(text, j)
        elif ch == "/" and lexer == "javascript":
            end = _regex_end(text, j, recent)
        if end is not None:
            out.append(text[j:end])
            recent, j = text[j:end], end
            continue
        if line == "//" and text.startswith("/*", j):
            end = text.find("*/", j + 2)
            j = n if end == -1 else end + 2
            out.append(" ")
            continue
        if text.startswith(line, j):
            end = text.find("\n", j)
            j = n if end == -1 else end
            out.append(" ")
            continue
        out.append(ch)
        recent = (recent + ch)[-24:]
        j += 1
    return "".join(out)


def split_args(sig: str, seps: str = ",", keep_star: bool = False) -> list[str]:
    """Split a comma-separated argument or parameter list at depth 0.

    A naive `split(",")` turns `Dictionary<string, string> q` into two entries,
    which over-counts arity for the query helpers and reports a phantom
    signature change for `surface`. Which `<`/`>` are brackets is `_bracket`'s
    call: the `>` of `cb: (a: A, b: B) => void`, Go's `<-chan` and the `>=` of a
    default value are not. `seps` lets an object-literal type body split on `;`
    too. Separators and brackets inside string literals are text.
    """
    out, depth, cur = [], 0, []
    j, n = 0, len(sig)
    while j < n:
        ch = sig[j]
        if ch in "\"'`":
            end = _quoted_end(sig, j)
            if end is not None:
                cur.append(sig[j:end])
                j = end
                continue
        depth += _bracket(sig, j)
        if ch in seps and depth == 0:
            out.append("".join(cur).strip())
            cur = []
        else:
            cur.append(ch)
        j += 1
    if "".join(cur).strip():
        out.append("".join(cur).strip())
    return [p for p in out if p and (keep_star or p != "*")]


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


def code_only(text: str, cfg: dict) -> str:
    """`text` without comments or Python docstrings, for request scanning.

    Every scan strips comments with `strip_comments` and the language's lexer:
    `surface` and the TypeScript declaration readers (`ts_declarations`,
    `_ts_object_members`, `_ts_typed_members`) and the JavaScript query-literal
    reader `object_literal_keys` do too, so a multi-line template
    literal holding `/*` is read the same way everywhere. A
    docstring's `"GET"` and `"/sources"` example made seclai-python's
    `_raise_for_openapi_response` look like the request behind twenty methods
    that never issue one, and a trailing `// (default: 2000)` held a `)` that
    ended a parameter list early. Triple-quoted strings are dropped (as `""`)
    rather than kept, which is right for docstrings and harmless elsewhere:
    no request path or query key is written in one.
    """
    return strip_comments(text, cfg.get("line_comment", "//"), cfg.get("lexer"), drop_triple=True)


def endpoint_helpers(text: str, cfg: dict) -> dict[str, str]:
    """Non-public client method name -> body, for those that DEFINE a request.

    Defining one means holding a verb and a path literal. Transport plumbing
    (`doBytes`, `SendRawResponseAsync`) takes both as arguments and holds neither,
    which is what keeps it out of this index.
    """
    if "helper_re" not in cfg:
        return {}
    # dict.fromkeys: the JavaScript boundary_re IS helper_re; scan it once.
    patterns = dict.fromkeys(p for p in (cfg["helper_re"], cfg["method_re"], cfg.get("boundary_re")) if p)
    ends = sorted({m.start() for pat in patterns for m in re.finditer(pat, text, re.M)})
    out: dict[str, str] = {}
    for m in re.finditer(cfg["helper_re"], text, re.M):
        name = m.group(1)
        if name in cfg.get("skip", ()) or name in out:
            continue
        end = next((e for e in ends if e > m.start()), len(text))
        body = code_only(text[m.start():end], cfg)
        if block_call(body, cfg["verb_re"]):
            out[name] = body
    return out


def helper_called(body: str, cfg: dict, helpers: dict[str, str]) -> str | None:
    """Name of the first endpoint helper a method body calls, if any."""
    code = code_only(body, cfg)   # a comment naming a helper is not a call to it
    first: tuple[int, str] | None = None
    for name in helpers:
        m = re.search(cfg["helper_call"].replace("{name}", re.escape(name)), code)
        if m and (first is None or m.start() < first[0]):
            first = (m.start(), name)
    return first[1] if first else None


def request_blocks(texts: list[str], cfg: dict):
    """Yield (name, body, call, request_body, helper) per public method with a request.

    A method with no request call of its own that calls a non-public method
    defining one is read through that helper — ONE hop. `body` stays the public
    method's, so its declared return type is what `returns` compares;
    `request_body` is where query keys are read. `helper` is None for own requests.
    Every text is read without comments or docstrings (see `code_only`), so a
    commented-out request is not a request and a `)` in a comment is not code.
    """
    texts = [code_only(t, cfg) for t in texts]
    shared: dict[str, str] = {}
    if cfg.get("helper_scope") == "package":
        for text in texts:
            for k, v in endpoint_helpers(text, cfg).items():
                shared.setdefault(k, v)
    for text in texts:
        helpers = shared or endpoint_helpers(text, cfg)
        for mname, body in method_blocks(text, cfg["method_re"], cfg.get("boundary_re")):
            if mname in cfg.get("skip", ()):
                continue
            call = block_call(body, cfg["verb_re"])
            if call:
                yield mname, body, call, body, None
                continue
            helper = helper_called(body, cfg, helpers)
            if helper:
                hbody = helpers[helper]
                yield mname, body, block_call(hbody, cfg["verb_re"]), body + "\n" + hbody, helper


def via_helper_line(n: int) -> str:
    return (f"   {n} of the methods read were resolved through a non-public helper"
            f" holding the request (one hop)")


def object_literal_keys(lit: str) -> tuple[set[str], bool]:
    """Keys of a JavaScript object literal's body, read member by member.

    `a: x`, `"a": x` and the shorthand `a` each name a key. A `...spread`, a
    `[computed]` key or anything else unreadable makes the literal unparsed:
    it can send keys nobody can see, so it must never read as clean. Nested
    literals are a member's value and contribute no keys of their own.
    """
    keys: set[str] = set()
    ok = True
    for member in split_args(strip_comments(lit, "//", "javascript")):  # JS only (`dict_members`)
        m = re.match(r"""(?:"([^"]+)"|'([^']+)'|([A-Za-z_$][\w$]*))\s*:""", member)
        if m:
            keys.add(next(g for g in m.groups() if g))
        elif re.fullmatch(r"[A-Za-z_$][\w$]*", member):
            keys.add(member)
        else:
            ok = False
    return keys, ok


def query_builders(texts: list[str], cfg: dict) -> dict[str, tuple[set[str], bool]]:
    """Package-level query builder name -> (keys it can set, parsed_ok).

    Keys come from the builder's own body, including any builder it calls, so
    `sortableListQuery` inherits `listQuery`'s page/limit and adds sort/order —
    and a method calling only `listQuery` is never credited with sort/order.
    A builder whose body cannot be read, or that recurses, is not ok: every
    method using it is then UNPARSED rather than credited with nothing.
    """
    if "builder_re" not in cfg:
        return {}
    bodies: dict[str, str] = {}
    for text in texts:
        for m in re.finditer(cfg["builder_re"], text, re.M):
            body = balanced_slice(text, m.end() - 1)
            bodies.setdefault(m.group(1), body if body is not None else "")
    out: dict[str, tuple[set[str], bool]] = {}

    def resolve(name: str, stack: tuple[str, ...]) -> tuple[set[str], bool]:
        if name in out:
            return out[name]
        if name in stack or not bodies[name]:
            return set(), False
        nested = {n: resolve(n, stack + (name,)) for n in bodies
                  if n != name and _calls(bodies[name], n)}
        res = block_query_keys(bodies[name], cfg, nested)
        if any(_calls(bodies[name], n) for n in stack + (name,)):
            res = (res[0], False)
        out[name] = res
        return res

    for name in bodies:
        resolve(name, ())
    return out


def _calls(body: str, name: str) -> bool:
    """A call to the free function `name(` — not `x.name(`, not `othername(`.
    Only Go has query builders, so Go's comment syntax is the one stripped."""
    return re.search(rf"(?<![\w.]){re.escape(name)}\(", strip_comments(body, "//", "go")) is not None


def block_query_keys(body: str, cfg: dict,
                     builders: dict[str, tuple[set[str], bool]] | None = None
                     ) -> tuple[set[str], bool]:
    """(keys, parsed_ok). parsed_ok is False when a construction site was found
    but could not be read — reported as an error, never as "sends nothing".
    `builders` is `query_builders(...)`: calls to them add their keys."""
    keys: set[str] = set()
    ok = True

    for name, (bkeys, bok) in (builders or {}).items():
        if _calls(body, name):
            keys |= bkeys
            ok = ok and bok

    for anchor, ordered in (cfg.get("helpers") or {}).items():
        for m in re.finditer(re.escape(anchor), body):
            args = balanced_slice(body, m.end() - 1, "(", ")")
            if args is None:
                ok = False
                continue
            keys |= set(ordered[:len(split_args(args))])

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
            if cfg.get("dict_members"):
                lkeys, lok = object_literal_keys(lit)
                keys |= lkeys
                ok = ok and lok
            else:
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

    via_helper = 0
    texts = [f.read_text(errors="replace") for f in files]
    builders = query_builders(texts, cfg)
    for mname, _body, call, request_body, helper in request_blocks(texts, cfg):
        via_helper += helper is not None
        keys, ok = block_query_keys(request_body, cfg, builders)
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
    if via_helper:
        print(via_helper_line(via_helper))

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

    # A method whose query could not be read AND whose path is not in the spec is
    # one problem, not two; counting it twice inflated the gate's error total.
    errors = (len(undeclared) + len(not_in_spec) + len(missing_required)
              + len(unparsed - not_in_spec))
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


def diff_operation(old: dict, new: dict, path: str, verb: str,
                   old_q: dict | None = None, new_q: dict | None = None) -> list[str]:
    """What actually changed on one operation, beyond its prose.

    `old_q`/`new_q` are prebuilt `spec_query_index` results. Rebuilding them here
    walks and $ref-resolves the whole 124-path spec twice for every changed
    operation; the caller builds them once.

    The original implementation labelled every non-verb change
    "description/params only", which reads as "nothing to do". In the 2026-07
    fast-follow two endpoints silently changed their 200 body from a bare array
    to a paginated envelope — a change that breaks every shipped client at
    runtime — and the tool reported them as prose edits. Anything a client can
    observe gets named here; only genuine prose edits fall through to "docs only".
    """
    old_q = spec_query_index(old) if old_q is None else old_q
    new_q = spec_query_index(new) if new_q is None else new_q
    lo = old_q.get((verb, normalise(path)))
    ln = new_q.get((verb, normalise(path)))
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
    old_q, new_q = spec_query_index(old), spec_query_index(new)
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
            lines += [f"{verb} {d}"
                      for d in diff_operation(old, new, p, verb, old_q, new_q)]
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
    # The escape hatches come FIRST. `byte[]` and `[]byte` are whole-blob
    # returns, but they also match the array patterns below, so testing shape
    # first turned every binary endpoint into an `array<byte>` and graded it a
    # SHAPE MISMATCH against the spec's object — a hard error, exit 1, on code
    # that is correct. Nothing in UNTYPED is a real list, so this cannot mask
    # `list[dict[str, Any]]`, which is not in the table.
    if t in NO_RETURN.get(lang, set()):
        return "none"
    if t in UNTYPED.get(lang, set()):
        return "?"
    for arr in (r"^List<(.+)>$", r"^IReadOnlyList<(.+)>$", r"^\[\](.+)$",
                r"^list\[(.+)\]$", r"^List\[(.+)\]$", r"^(.+)\[\]$"):
        m = re.match(arr, t)
        if m:
            return f"array<{normalise_type(m.group(1), lang)}>"
    return strip_generated_prefix(t)


def block_return(body: str, cfg: dict) -> str | None:
    """The declared return type of the method a block starts with; None when it
    declares none or cannot be read (`read_block_return` tells them apart)."""
    return read_block_return(body, cfg)[0]


def read_block_return(body: str, cfg: dict) -> tuple[str | None, bool]:
    """(declared return type, unreadable) of the method a block starts with.
    The block is known to be a method that makes a request, so anything short of
    reading its return — or finding it has no annotation — is unreadable, and
    `returns` must not count it as "nothing to check".

    Read by slicing the parameter list with paren matching rather than by regex:
    signatures wrap across lines and contain nested parens and generics, both of
    which truncate a regex silently — and a silent truncation here reads as
    "this method declares nothing to check".
    """
    if "return_head_re" in cfg:
        m = re.match(cfg["return_head_re"], body, re.S)
        return (m.group(1).strip(), False) if m else (None, True)
    # Start at the paren the method anchor itself ends on. Taking the first `(`
    # in the block instead picks up Go's receiver — `func (c *Client) GetMe(` —
    # so the "return type" came back as the rest of the signature.
    anchor = re.match(cfg["method_re"], body, re.M)
    if not anchor:
        return None, True
    sliced = _after_params(body, body.find("(", anchor.end() - 1))
    if sliced is None:
        return None, True
    ret, _ = _read_return_tail(body, sliced[1], cfg["lexer"])
    if ret is None:
        return None, True
    if cfg["lexer"] == "go":
        parts = split_args(ret)   # `*T, error` -> `*T`
        return (parts[0] if parts else ""), False
    return (ret or None), False   # no annotation declares nothing to check


def cmd_returns(args) -> int:
    repo = Path(args.repo).resolve()
    if repo.name in NOT_APPLICABLE:
        print(f"{repo.name}: not applicable — {NOT_APPLICABLE[repo.name]}")
        return 0
    lang = args.lang or detect_lang(repo)
    if not lang:
        die(f"cannot detect SDK language in {repo}")
    cfg = LANGS[lang]
    if "return_after_params" not in cfg and "return_head_re" not in cfg:
        print(f"{repo.name} [{lang}] — return types are not declared; nothing to check.")
        return 0
    files = source_files(repo, cfg)
    if not files:
        # Silence here is a false pass: no sources means no methods means
        # "nothing disagrees with the spec", which is what a mis-pointed repo
        # argument looks like.
        die(f"no client sources found for {lang} in {repo}")
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

    mismatched, renamed, untyped, unreadable, n, via_helper = [], [], [], [], 0, 0
    texts = [path.read_text(encoding="utf-8", errors="replace") for path in files]
    for mname, body, call, _request_body, helper in request_blocks(texts, cfg):
        if call not in want:
            continue
        declared, unread = read_block_return(body, cfg)
        if unread:
            unreadable.append((mname, call))
            continue
        if declared is None:
            continue
        n += 1
        via_helper += helper is not None
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
    if via_helper:
        print(via_helper_line(via_helper))

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

    if unreadable:
        print(f"\nUNREADABLE ({len(unreadable)}) — request methods whose return type this "
              "scan could not read; a shape mismatch there would go unreported:")
        for name, (verb, p) in unreadable:
            print(f"   {name}  ({verb} {p})")
    if mismatched:
        print(f"\n{len(mismatched)} return type(s) disagree with the spec in SHAPE.")
        return 1
    if unreadable:
        print(f"\n{len(unreadable)} unreadable return type(s) — the result above is incomplete.")
        return 1
    print("\nno return type disagrees with the spec in shape")
    return 0



# ── surface ──────────────────────────────────────────────────────────────────
def _after_params(text: str, open_paren: int) -> tuple[str, int] | None:
    """(parameter list, index just past its closing `)`) for the `(` at
    `open_paren`, sliced by paren matching. Shared by `surface` and `returns`, so
    the offset past the `)` is written once."""
    if open_paren < 0:
        return None
    params = balanced_slice(text, open_paren, "(", ")")
    if params is None:
        return None
    return params, open_paren + len(params) + 2


def _read_return_tail(text: str, i: int, lexer: str) -> tuple[str | None, bool]:
    """(return type, unreadable). The type is None with unreadable False when
    what follows is a statement, not a method; with unreadable True when the
    scan never reached the body — the case `surface` must not stay quiet about.

    A scan, not a regex. The type ends at the `{` that opens the body, at depth
    0 — so `Promise<{ id: string }>` (its `{` is inside `<>`), an object type
    literal right after `:` or a `|`, and Go's `interface{}` are read whole. The
    regexes this replaced stopped at the first `{` and recorded `Promise<`, so a
    change inside that type was invisible, and Go's matched only a `(...)` tuple,
    so every method returning a lone `error` was missing from the surface.
    """
    if lexer == "csharp":
        return "", False     # read from the head; see sig_groups
    if lexer == "python":
        m = re.compile(r"\s*(?:->\s*([^:]+?))?\s*:").match(text, i)
        # Every match of Python's pattern is a `def`, so a miss is a misread.
        return (_norm(m.group(1)), False) if m else (None, True)
    j, n = i, len(text)
    if lexer == "javascript":
        m = re.compile(r"\s*:").match(text, j)
        if m is None:
            m = re.compile(r"\s*\{").match(text, j)
            return ("", False) if m else (None, False)   # `foo(x);` is a call
        j = m.end()
    start, depth = j, 0
    while j < n:
        c = text[j]
        if c in "\"'`":
            end = _quoted_end(text, j)
            if end is not None:
                j = end
                continue
        if c in ";=" and depth == 0 and not text.startswith("=>", j):
            return None, False   # a statement or a field initialiser, not a method
        if c == "{" and depth == 0:
            so_far = text[start:j].rstrip()
            # A TypeScript `{` opens a type literal, not the body, wherever a type
            # can start: after `|`, `&`, `=>`, `:`, `,`, and in a conditional type
            # after `?` or `extends` (`T extends { a: 1 } ? { b: 2 } : never`).
            literal = (lexer == "go" and re.search(r"\b(?:interface|struct)$", so_far)) or (
                lexer == "javascript" and (
                    not so_far or so_far.endswith(("|", "&", "=>", ":", ",", "?"))
                    or re.search(r"\bextends$", so_far)))
            if not literal:
                ret = (_norm_ts if lexer == "javascript" else _norm)(text[start:j])
                # A Go result tuple is recorded without its parens, `*Run, error`.
                if lexer == "go" and ret.startswith("(") and balanced_slice(ret, 0, "(", ")") == ret[1:-1]:
                    ret = _norm(ret[1:-1])
                return ret, False
            body = balanced_slice(text, j)
            if body is None:
                return None, True
            j += len(body) + 2
            continue
        depth += _bracket(text, j)
        j += 1
    return None, True   # ran off the end: the body was never found


def public_surface(text: str, cfg: dict) -> dict[str, set[tuple[str, str]]]:
    """method name -> {(return type, parameter list)}. A set, because C# and Go
    both allow overloads/variants under one name.

    The parameter list is stored as its split, normalised members joined by
    ", ", so a reformat — prettier or black wrapping a signature and adding a
    trailing comma — is not a change. Python's bare `*` is kept: making a
    parameter keyword-only is one.
    """
    return scan_surface(text, cfg)[0]


def scan_surface(text: str, cfg: dict) -> tuple[dict[str, set[tuple[str, str]]], list[str]]:
    """(`public_surface`, `unreadable_heads`) from one pass over `text`."""
    out: dict[str, set[tuple[str, str]]] = {}
    unreadable: set[str] = set()
    # Comments are not signature, and stripped before slicing so a `)` or an
    # apostrophe in one cannot end the parameter list early.
    text = strip_comments(text, cfg.get("line_comment", "//"), cfg["lexer"])
    for m in re.finditer(cfg["sig_re"], text, re.S):
        status, name, entry = _read_head(text, m, cfg)
        if status == "ok":
            out.setdefault(name, set()).add(entry)
        elif status == "unreadable":
            unreadable.add(name)
    return out, sorted(unreadable)


def unreadable_heads(text: str, cfg: dict) -> list[str]:
    """Names of public method heads `public_surface` matched but could not read.

    Such a method is missing from the surface at that revision, so its removal
    or change would report nothing. `surface` lists these and fails, which turns
    any shape the scanner does not understand into a visible error rather than a
    silent miss. A statement the pattern also matches (`foo(x);`) is not listed.
    """
    return scan_surface(text, cfg)[1]


def _read_head(text: str, m: re.Match, cfg: dict) -> tuple[str, str, tuple[str, str] | None]:
    """("ok", name, (return type, parameters)), ("skip", name, None) for a match
    that is not a method, or ("unreadable", name, None)."""
    order = cfg["sig_groups"]
    name = _norm(dict(zip(order, m.groups()))["name"])
    # Most patterns end on the `(`; JavaScript's stops before it, so a
    # type-parameter list can come first.
    at = m.end() - 1 if text[m.end() - 1] == "(" else m.end()
    tparams = None
    if cfg.get("type_params") and text.startswith("<", at):
        tp = balanced_slice(text, at, "<", ">")
        if tp is None:
            return "unreadable", name, None
        # Split and rejoined like the parameters, so prettier's trailing comma
        # on a wrapped list is not a change.
        tparams = ", ".join(_norm_ts(x) for x in split_args(tp))
        at += len(tp) + 2
        while at < len(text) and text[at] in " \t\n":
            at += 1
    if not text.startswith("(", at):
        return ("unreadable" if tparams is not None else "skip"), name, None
    sliced = _after_params(text, at)
    if sliced is None:
        return "unreadable", name, None
    params, after = sliced
    ret, unreadable = _read_return_tail(text, after, cfg["lexer"])
    if ret is None:
        return ("unreadable" if unreadable else "skip"), name, None
    if tparams is not None:
        ret = _join_type_params(tparams, ret)
    parts = dict(zip(order, m.groups() + (params,)))
    if "ret" not in parts:
        parts["ret"] = ret
    norm = _norm_ts if cfg["lexer"] == "javascript" else _norm
    canon = ", ".join(norm(p) for p in split_args(params, keep_star=True))
    return "ok", _norm(parts["name"]), (_norm(parts["ret"]), canon)



# Kept as an alias: the tests and the older call sites name it `_params`.
_params = split_args


# A TypeScript parameter's head, `name?: `, and an object member's name and `?`.
_TS_PARAM_HEAD_RE = re.compile(r"\s*([\w$]+\s*\??\s*:\s*)")
_TS_MEMBER_NAME = r"(?:readonly\s+)?([\w$]+|\"[^\"]*\"|'[^']*')\s*(\?)?\s*"
# A property member, `name?: type`, with the type captured.
_TS_MEMBER_RE = re.compile(_TS_MEMBER_NAME + r":\s*(.*)$", re.S)
# Any named member: a property, or a method `name(...)`.
_TS_NAMED_MEMBER_RE = re.compile(_TS_MEMBER_NAME + r"[:(]")


def _ts_inline_object(param: str) -> tuple[str, str, str] | None:
    """(prefix, body, suffix) when a TypeScript parameter's type is an inline
    object literal: `opts: { a?: string } = {}` -> ("opts:", "a?: string", "= {}").

    The `{` must follow the annotation colon directly, so a default value such as
    `x = {}` or a generic `Partial<{...}>` never qualifies. None otherwise.
    """
    m = _TS_PARAM_HEAD_RE.match(param)
    if not m or not param.startswith("{", m.end()):
        return None
    start = m.end()
    body = balanced_slice(param, start)
    if body is None:
        return None
    end = start + len(body) + 1
    return (_norm(param[:start]), body, _norm(param[end + 1:]))


def _ts_object_members(body: str) -> dict[str, tuple[bool, str]] | None:
    """member name -> (optional, normalised text) for an object-literal type body,
    split on depth-0 `;` or `,`. None when any member cannot be named (index
    signatures, call signatures, spread-like text) or a name repeats — the caller
    then refuses to call the change compatible. Comments are dropped here even
    though `public_surface` already stripped them: `classify_change` is also
    called directly with raw signature text, and the bodies are a few lines, so
    the second pass costs nothing worth a precondition. Comments are dropped first:
    seclai-javascript documents each member with `/** ... */`, which would
    otherwise hide the member's name, and a `,` or `;` inside one would split it.
    """
    parts = split_args(strip_comments(body, "//", "javascript"), ";,")
    out: dict[str, tuple[bool, str]] = {}
    for raw in parts:
        text = _norm(raw)
        if not text:
            continue
        m = _TS_NAMED_MEMBER_RE.match(text)
        if not m or m.group(1) in out:
            return None
        out[m.group(1)] = (bool(m.group(2)), text)
    return out



def _arrow_at_top(t: str) -> bool:
    """`t` is a function type: it has an `=>` at bracket depth 0."""
    depth, j = 0, 0
    while j < len(t):
        if t[j] in "\"'`":
            end = _quoted_end(t, j)
            if end is not None:
                j = end
                continue
        if depth == 0 and t.startswith("=>", j):
            return True
        depth += _bracket(t, j)
        j += 1
    return False


def _ts_union(t: str) -> list[str]:
    """Top-level `|` members of a type, whitespace-normalised and sorted.

    A function type is one member: in `(e: E) => T | undefined` the `|` belongs
    to the return type, so splitting it there called a callback whose return
    type changed "optional property widened to accept undefined".
    """
    if _arrow_at_top(t):
        return [_norm(t)]
    return sorted(_norm(x) for x in split_args(t, "|"))


def _ts_widened_to_undefined(old: str, new: str) -> bool:
    """`x?: T` -> `x?: T | undefined` (or `undefined | T`). Both optional, same
    name, and the new union is exactly the old one plus `undefined`. Under
    exactOptionalPropertyTypes that admits an explicit `undefined`; without it
    the type is unchanged. Either way every old value and old use still fits."""
    mo, mn = _TS_MEMBER_RE.match(old), _TS_MEMBER_RE.match(new)
    if not mo or not mn or mo.group(1) != mn.group(1) or not (mo.group(2) and mn.group(2)):
        return False
    old_u = _ts_union(mo.group(3))
    return "undefined" not in old_u and _ts_union(mn.group(3)) == sorted(old_u + ["undefined"])


def _ts_options_change(o: str, n: str) -> set[str] | None:
    """Kinds of compatible change between two inline options-object parameters,
    or None when the change is not provably compatible. Same name, optionality
    and default; every old member unchanged, in any order ("same"), or only
    widened to accept `undefined`; every added member optional, and none added to an empty `{}`."""
    po, pn = _ts_inline_object(o), _ts_inline_object(n)
    if po is None or pn is None or po[0] != pn[0] or po[2] != pn[2]:
        return None
    mo, mn = _ts_object_members(po[1]), _ts_object_members(pn[1])
    if mo is None or mn is None:
        return None
    # `{}` accepts any non-null value; a weak type such as `{ a?: T }` does not (TS2559).
    if not mo and mn:
        return None
    kinds: set[str] = set()
    for k, (opt, text) in mo.items():
        if k not in mn:
            return None
        if mn[k] == (opt, text):
            continue
        if not _ts_widened_to_undefined(text, mn[k][1]):
            return None
        kinds.add("undefined")
    added = [v for k, v in mn.items() if k not in mo]
    if not all(opt for opt, _ in added):
        return None
    if added:
        kinds.add("props")
    return kinds or {"same"}


def _ts_wrapped_type(o: str, n: str) -> str | None:
    """"omit" or "partial" when the parameter's type `T` became `Omit<T, "k">`
    (keys a union of string literals) or `Partial<T>`, with `T`, the name, the
    optionality and the default textually unchanged. Every old argument still
    fits both. None otherwise — including `Pick<>` and an `Omit<>` of another
    type."""
    ho, hn = _TS_PARAM_HEAD_RE.match(o), _TS_PARAM_HEAD_RE.match(n)
    if not ho or not hn or _norm(ho.group(1)) != _norm(hn.group(1)):
        return None
    rest_o, rest_n = o[ho.end():], n[hn.end():]
    w = re.match(r"(Omit|Partial)\s*<", rest_n)
    if not w:
        return None
    inner = balanced_slice(rest_n, w.end() - 1, "<", ">")
    if inner is None:
        return None
    after = rest_n[w.end() + len(inner) + 1:]
    args = split_args(inner)
    kind = w.group(1).lower()
    if kind == "omit":
        if len(args) != 2:
            return None
        keys = split_args(args[1], "|")
        if not keys or not all(re.fullmatch(r"\"[^\"]*\"|'[^']*'", k) for k in keys):
            return None
    elif len(args) != 1:
        return None
    return kind if _norm(rest_o) == _norm(args[0] + after) else None


_TS_COMPATIBLE_LABELS = {
    "default": "required parameter made optional in place — source compatible",
    "added": "optional parameters added — source compatible",
    "props": ("optional property added to an options object — usually source compatible, "
              "but breaks a caller passing a non-literal object whose same-named property "
              "has another type"),
    "undefined": "optional property widened to accept undefined — source compatible",
    "omit": ("parameter type wrapped in Omit<…> — usually source compatible: every old value "
             "still fits, but an object literal that sets an omitted key no longer compiles"),
    "partial": "parameter type wrapped in Partial<…> — source compatible: every old value still fits",
    "same": "options object unchanged apart from member order or separators",
}


def _ts_compatible_change(old: list[str], new: list[str]) -> str | None:
    """Label when every changed TypeScript parameter changed in one of the
    provably compatible ways above, with the same count, order and every other
    parameter textually unchanged. None — graded breaking — otherwise."""
    if len(old) != len(new) or old == new:
        return None
    kinds: set[str] = set()
    for o, n in zip(old, new):
        if o == n:
            continue
        k = _ts_options_change(o, n)
        if k is None:
            w = _ts_wrapped_type(o, n)
            if w is None:
                return None
            k = {w}
        kinds |= k
    if len(kinds) > 1:
        kinds.discard("same")
    return "; ".join(_TS_COMPATIBLE_LABELS[k] for k in _TS_COMPATIBLE_LABELS if k in kinds)


# ── Parameters that gained a default ─────────────────────────────────────────
# `f(id: str)` -> `f(id: str | None = None)` and `f(id: string)` -> `f(id?: string)`
# keep every old call working, but a textual diff called both BREAKING. They
# are accepted only parameter by parameter: same name, same position, same
# kind, the type unchanged or only widened to admit None/undefined, and every
# other parameter unchanged or added as optional at the end. Anything else —
# a default lost, a rename, a reorder, a narrowed type, a positional parameter
# made keyword-only — stays BREAKING. C# is deliberately not included: the
# default lands in metadata the caller compiled against, and it has no case
# that needs it.

def _top_level_index(s: str, ch: str) -> int:
    """Index of the first `ch` at bracket depth 0 outside string literals, or -1.
    For `=`, the `=` of `=>`, `==`, `!=`, `<=` and `>=` does not count."""
    depth, j, prev = 0, 0, ""
    while j < len(s):
        c = s[j]
        if c in "\"'`":
            end = _quoted_end(s, j)
            if end is not None:
                j, prev = end, ""
                continue
        b = _bracket(s, j)
        if b:
            depth += b
        elif c == ch and depth == 0:
            nxt = s[j + 1] if j + 1 < len(s) else ""
            if ch != "=" or (nxt not in "=>" and prev not in "=!<>"):
                return j
        prev = c
        j += 1
    return -1


def _union_members(t: str) -> set[str]:
    m = re.fullmatch(r"\s*Optional\[(.*)\]\s*", t, re.S)
    if m:
        return {_norm(x) for x in split_args(m.group(1), "|")} | {"None"}
    return {_norm(x) for x in split_args(t, "|")}


def _py_param(p: str) -> tuple[str, str, str | None] | None:
    """(name, type, default) of one Python parameter, or None if not plain."""
    eq = _top_level_index(p, "=")
    head, default = (p, None) if eq < 0 else (p[:eq], p[eq + 1:].strip())
    m = re.fullmatch(r"\s*([A-Za-z_]\w*)\s*(?::\s*(.+?))?\s*", head, re.S)
    if not m:
        return None
    return m.group(1), _norm(m.group(2)), default


def _ts_param(p: str) -> tuple[str, bool, str, str | None] | None:
    """(name, optional `?`, type, default) of one TypeScript parameter, or None."""
    eq = _top_level_index(p, "=")
    head, default = (p, None) if eq < 0 else (p[:eq], _norm(p[eq + 1:]))
    m = re.fullmatch(r"\s*([A-Za-z_$][\w$]*)\s*(\?)?\s*(?::\s*(.+?))?\s*", head, re.S)
    if not m:
        return None
    return m.group(1), bool(m.group(2)), _norm(m.group(3)), default


def _gained_default(lang: str, o: str, n: str) -> bool:
    """`o` was required and `n` is the same parameter made optional, its type
    unchanged or only widened by None (Python) / undefined (TypeScript)."""
    if lang == "python":
        po, pn = _py_param(o), _py_param(n)
        if not po or not pn or po[0] != pn[0] or po[2] is not None or pn[2] is None:
            return False
        nullable = "None"
    else:
        po, pn = _ts_param(o), _ts_param(n)
        if (not po or not pn or po[0] != pn[0] or po[1] or po[3] is not None
                or not (pn[1] or pn[3] is not None)):
            return False
        nullable = "undefined"
    type_at = 2 if lang == "javascript" else 1
    old_t, new_t = po[type_at], pn[type_at]
    if not old_t:
        return not new_t
    return new_t == old_t or _union_members(new_t) == _union_members(old_t) | {nullable}


def _is_optional(lang: str, p: str) -> bool:
    if lang == "python":
        q = _py_param(p)
        return q is not None and q[2] is not None
    q = _ts_param(p)
    return q is not None and (q[1] or q[3] is not None)


def _py_kinds(raw: str) -> list[tuple[str, str]] | None:
    """[(param, "pos"|"kw")] with the bare `*` consumed, or None when the list
    has `/`, `*args` or `**kwargs` — shapes this rule does not reason about."""
    out, kind = [], "pos"
    for p in split_args(raw, keep_star=True):
        if p == "*":
            kind = "kw"
        elif p == "/" or p.startswith("*"):
            return None
        else:
            out.append((p, kind))
    return out


def _gained_default_change(lang: str, old_par: str, new_par: str,
                           param_change=None) -> set[str] | None:
    """Kinds of compatible change, when every old parameter is unchanged, made
    optional in place, or (via `param_change`) changed in another provably
    compatible way, and every added parameter is optional. None otherwise."""
    if lang == "python":
        ok, nk = _py_kinds(old_par), _py_kinds(new_par)
        if ok is None or nk is None:
            return None
        o_pos = [p for p, k in ok if k == "pos"]
        n_pos = [p for p, k in nk if k == "pos"]
        o_kw = {(_py_param(p) or ("",))[0]: p for p, k in ok if k == "kw"}
        n_kw = {(_py_param(p) or ("",))[0]: p for p, k in nk if k == "kw"}
        if len(n_pos) < len(o_pos) or "" in o_kw or "" in n_kw:
            return None
        pairs = list(zip(o_pos, n_pos))
        for name, o in o_kw.items():
            if name not in n_kw:
                return None
            pairs.append((o, n_kw[name]))
        added = n_pos[len(o_pos):] + [p for name, p in n_kw.items() if name not in o_kw]
    else:
        op, np_ = split_args(old_par), split_args(new_par)
        if len(np_) < len(op):
            return None
        pairs, added = list(zip(op, np_)), np_[len(op):]
    kinds: set[str] = set()
    for o, n in pairs:
        if o == n:
            continue
        if _gained_default(lang, o, n):
            kinds.add("default")
            continue
        k = param_change(o, n) if param_change else None
        if k is None:
            return None
        kinds |= k
    if not all(_is_optional(lang, a) for a in added):
        return None
    if added:
        kinds.add("added")
    return kinds or None


# ── TypeScript named types ───────────────────────────────────────────────────
# An options parameter retyped from one named type to another (`ListOptions` ->
# `ListSolutionsOptions`) was graded BREAKING on its text. It is compatible only
# when both types resolve, from source at their own revision, to members where
# every old member survives unchanged and every added one is optional. A type
# that does not resolve — a generic, an import, an indexed access, a top-level
# union — keeps the change BREAKING.

_TS_BUILTIN = {"string", "number", "boolean", "undefined", "null", "unknown", "any",
               "never", "object", "void", "bigint", "symbol", "true", "false",
               "Record", "Array", "ReadonlyArray", "Promise", "Date", "Readonly",
               "Partial", "Required", "Pick", "Omit", "Map", "Set", "readonly",
               "keyof", "typeof"}


def _ts_expr_end(text: str, i: int) -> int:
    """End of a type-alias expression starting at `i`: the first `;` at depth 0,
    or a blank line / new top-level declaration when the `;` is omitted."""
    depth, j = 0, i
    while j < len(text):
        c = text[j]
        if c in "\"'`":
            end = _quoted_end(text, j)
            if end is not None:
                j = end
                continue
        b = _bracket(text, j)
        if b:
            depth += b
        elif depth == 0 and (c == ";" or (c == "\n" and re.match(
                r"\n\s*(?:\n|(?:export\s+)?(?:interface|type|const|function|class|import)\b)",
                text[j:]))):
            return j
        j += 1
    return len(text)


def ts_declarations(texts: list[str]) -> dict[str, tuple | None]:
    """Type name -> ("interface", [extends...], body, text) | ("alias", expr, text),
    or None when it cannot be resolved (generic, or declared twice differently)."""
    out: dict[str, tuple | None] = {}

    def put(name: str, decl: tuple | None) -> None:
        if name in out and out[name] != decl:
            out[name] = None
        else:
            out[name] = decl

    for raw in texts:
        text = strip_comments(raw, "//", "javascript")
        for m in re.finditer(r"(?m)^[ \t]*(?:export\s+)?(?:declare\s+)?interface\s+([\w$]+)\s*(<)?",
                             text):
            brace = text.find("{", m.end())
            body = balanced_slice(text, m.end()) if brace >= 0 else None
            if m.group(2) or body is None:
                put(m.group(1), None)
                continue
            ext = text[m.end():brace].strip()
            bases = split_args(ext[len("extends"):]) if ext.startswith("extends") else []
            if ext and not bases:
                put(m.group(1), None)
                continue
            whole = _norm(text[m.start():brace + len(body) + 2])
            put(m.group(1), ("interface", bases, body, whole))
        for m in re.finditer(r"(?m)^[ \t]*(?:export\s+)?(?:declare\s+)?type\s+([\w$]+)\s*(<)?", text):
            if m.group(2):
                put(m.group(1), None)
                continue
            eq = re.match(r"\s*=", text[m.end():])
            if not eq:
                continue
            start = m.end() + eq.end()
            end = _ts_expr_end(text, start)
            expr = _norm(text[start:end])
            put(m.group(1), ("alias", expr, _norm(text[m.start():end])))
    return out


def _ts_typed_members(body: str) -> dict[str, tuple[bool, str]] | None:
    """member -> (optional, type) for an object type body; None if any member
    is not a plain `name?: type` property (methods, index signatures, ...)."""
    out: dict[str, tuple[bool, str]] = {}
    for raw in split_args(strip_comments(body, "//", "javascript"), ";,"):
        text = _norm(raw)
        if not text:
            continue
        m = _TS_MEMBER_RE.match(text)
        if not m or m.group(1) in out:
            return None
        out[m.group(1).strip("\"'")] = (bool(m.group(2)), _norm(m.group(3)))
    return out


def _ts_string_keys(k: str) -> set[str] | None:
    keys = split_args(k, "|")
    if not keys or not all(re.fullmatch(r"\"[^\"]*\"|'[^']*'", x) for x in keys):
        return None
    return {x[1:-1] for x in keys}


def ts_resolve(expr: str, decls: dict, seen: tuple[str, ...] = ()
               ) -> dict[str, tuple[bool, str]] | None:
    """Members of a TypeScript object type expression, or None if unresolvable."""
    e = _norm(expr)
    while e.startswith("(") and balanced_slice(e, 0, "(", ")") == e[1:-1]:
        e = e[1:-1].strip()
    if not e:
        return None
    if e.startswith("{") and balanced_slice(e, 0) == e[1:-1]:
        return _ts_typed_members(e[1:-1])
    if len(split_args(e, "|")) > 1:
        return None
    parts = split_args(e, "&")
    if len(parts) > 1:
        merged: dict[str, tuple[bool, str]] = {}
        for part in parts:
            r = ts_resolve(part, decls, seen)
            if r is None:
                return None
            for k, v in r.items():
                if k in merged and merged[k] != v:
                    return None
                merged[k] = v
        return merged
    m = re.fullmatch(r"(Omit|Pick|Partial)\s*<(.*)>", e, re.S)
    if m:
        args = split_args(m.group(2))
        base = ts_resolve(args[0], decls, seen) if args else None
        if base is None:
            return None
        if m.group(1) == "Partial":
            return {k: (True, t) for k, (_, t) in base.items()} if len(args) == 1 else None
        keys = _ts_string_keys(args[1]) if len(args) == 2 else None
        if keys is None:
            return None
        if m.group(1) == "Omit":
            return {k: v for k, v in base.items() if k not in keys}
        return {k: v for k, v in base.items() if k in keys} if keys <= set(base) else None
    if not re.fullmatch(r"[A-Za-z_$][\w$]*", e) or e in seen:
        return None
    d = decls.get(e)
    if d is None:
        return None
    if d[0] == "alias":
        return ts_resolve(d[1], decls, seen + (e,))
    merged = {}
    for b in d[1]:
        r = ts_resolve(b, decls, seen + (e,))
        if r is None:
            return None
        merged.update(r)
    own = _ts_typed_members(d[2])
    if own is None:
        return None
    merged.update(own)
    return merged


def _ts_type_refs_unchanged(t: str, old_decls: dict, new_decls: dict) -> bool:
    """Every identifier a property type names is a builtin, or a local type whose
    declaration is textually identical at both revisions. Same text naming a
    type that changed underneath is not the same type."""
    bare = re.sub(r"\"[^\"]*\"|'[^']*'|`[^`]*`", "", t)
    for ident in set(re.findall(r"[A-Za-z_$][\w$]*", bare)):
        if ident in _TS_BUILTIN:
            continue
        o, n = old_decls.get(ident), new_decls.get(ident)
        if o is None or n is None or o[-1] != n[-1]:
            return False
    return True


def ts_named_options_change(o: str, n: str, old_decls: dict, new_decls: dict) -> set[str] | None:
    """Kinds of compatible change between two options parameters whose types are
    resolved from declarations; None unless provably compatible."""
    po, pn = _ts_param(o), _ts_param(n)
    if not po or not pn or (po[0], po[1], po[3]) != (pn[0], pn[1], pn[3]) or not po[2] or not pn[2]:
        return None
    mo, mn = ts_resolve(po[2], old_decls), ts_resolve(pn[2], new_decls)
    if mo is None or mn is None or (not mo and mn):
        return None
    kinds: set[str] = set()
    for k, (opt, typ) in mo.items():
        if k not in mn or mn[k][0] != opt:
            return None
        if not _ts_type_refs_unchanged(typ, old_decls, new_decls):
            return None
        if mn[k][1] == typ:
            continue
        if not _ts_widened_to_undefined(f"{k}?: {typ}", f"{k}?: {mn[k][1]}"):
            return None
        kinds.add("undefined")
    added = [v for k, v in mn.items() if k not in mo]
    if not all(opt for opt, _ in added):
        return None
    if added:
        kinds.add("props")
    return kinds or {"same"}


def _join_type_params(tparams: str, ret: str) -> str:
    """A generic method's recorded return: `<T extends A>: Promise<T[]>`.

    Kept with the return type rather than the parameters so that the optional-
    parameter rules, which compare parameters by position, are unaffected. No
    TypeScript type starts with `<...>:` (a generic function type is `<T>(`), so
    `_split_type_params` can always take it apart again."""
    return f"<{tparams}>: {ret}".rstrip()


def _unwrap(t: str, wrapper: str) -> str | None:
    """`X` when `t` is exactly `wrapper<X>` (`Promise<X>`, `Array<X>`), else None."""
    if not t.startswith(wrapper + "<"):
        return None
    inner = balanced_slice(t, len(wrapper), "<", ">")
    if inner is None or len(wrapper) + len(inner) + 2 != len(t):
        return None
    return inner.strip()


def _array_element(t: str) -> str | None:
    """`X` when `t` is an array of `X`: `X[]` for a name or a parenthesised
    type, or `Array<X>`. A union like `A | B[]` is not an array, so the element
    must be a single name (with type arguments) or wrapped in parens."""
    inner = _unwrap(t, "Array")
    if inner is not None:
        return inner
    if not t.endswith("[]"):
        return None
    el = t[:-2].strip()
    if el.startswith("(") and balanced_slice(el, 0, "(", ")") == el[1:-1]:
        return el[1:-1].strip()
    m = re.fullmatch(r"[\w$.]+", el) or re.fullmatch(r"[\w$.]+(<.*>)", el)
    if m and (m.lastindex is None or _unwrap(el, el[:el.index("<")]) is not None):
        return el
    return None


def _narrowed_from_unknown(old: str, new: str) -> bool:
    """True when `new` only replaces the `unknown` in `old` with a concrete type,
    keeping everything around it — the one return change a caller cannot
    notice except as better types.

    `unknown` is the value the caller reads only when it is the whole return,
    its array, or either inside `Promise<>`. The wrapper must survive: callers of
    `unknown[]` call `.map`, and callers of `Promise<unknown>` `await` or
    `.then`, so `unknown[]` -> `Promise<Thing>` breaks them. An `unknown`
    anywhere else (a callback parameter, a constraint, `Record<string, unknown>`)
    stays BREAKING.
    """
    if "unknown" in new:
        return False
    def concrete(x: str | None) -> bool:
        return bool(x) and "unknown" not in x
    if old == "unknown":
        return True
    if old == "unknown[]":
        return concrete(_array_element(new))
    if old == "Promise<unknown>":
        return concrete(_unwrap(new, "Promise"))
    if old == "Promise<unknown[]>":
        inner = _unwrap(new, "Promise")
        return inner is not None and concrete(_array_element(inner))
    return False


def _split_type_params(ret: str) -> tuple[str | None, str]:
    """(type parameters, return type) of a recorded return; None when the method
    is not generic."""
    if ret.startswith("<"):
        inner = balanced_slice(ret, 0, "<", ">")
        if inner is not None and ret.startswith(":", len(inner) + 2):
            return inner, ret[len(inner) + 3:].strip()
    return None, ret


def classify_change(lang: str, old: tuple[str, str], new: tuple[str, str],
                    ts_decls: tuple[dict, dict] | None = None) -> tuple[str, str]:
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
    if lang == "javascript":
        # A type parameter or its constraint changing cannot be proven safe from
        # text: `<T extends A>` -> `<T extends B>` can reject every old call.
        old_tp, old_ret = _split_type_params(old_ret)
        new_tp, new_ret = _split_type_params(new_ret)
        if old_tp != new_tp:
            return "break", "type parameters changed"
    if old_par != new_par:
        op, np_ = split_args(old_par), split_args(new_par)
        if lang == "javascript" and old_ret == new_ret:
            label = _ts_compatible_change(op, np_)
            if label:
                return "warn", label
        if lang in ("python", "javascript") and old_ret == new_ret:
            def ts_param_change(o: str, n: str) -> set[str] | None:
                k = _ts_options_change(o, n)
                if k is None:
                    w = _ts_wrapped_type(o, n)
                    k = {w} if w else None
                if k is None and ts_decls is not None:
                    k = ts_named_options_change(o, n, *ts_decls)
                return k
            kinds = _gained_default_change(
                lang, old_par, new_par, ts_param_change if lang == "javascript" else None)
            if kinds:
                if len(kinds) > 1:
                    kinds.discard("same")
                return "warn", "; ".join(_TS_COMPATIBLE_LABELS[k]
                                         for k in _TS_COMPATIBLE_LABELS if k in kinds)
        # `len(np_) > len(op)` matters: `split_args` drops Python's bare `*`, so
        # `f(self, a, b)` -> `f(self, a, *, b)` yields two IDENTICAL lists, the
        # empty tail passes `all()` vacuously, and making a parameter
        # keyword-only — which breaks every positional caller — was graded
        # "optional parameters added — source compatible".
        added_optional = (len(np_) > len(op)
                          and np_[:len(op)] == op
                          and all("=" in a for a in np_[len(op):]))
        if added_optional:
            if lang == "csharp":
                return "warn", "optional parameters added — source compatible, but binary breaking for consumers that do not recompile"
            return "warn", "optional parameters added — source compatible"
        return "break", "parameter list changed"
    if old_ret != new_ret:
        # Only where `unknown` is the value the caller reads: the whole return,
        # its array, or either inside `Promise<>`. Anywhere else it may be a
        # parameter of a callback (`(x: unknown) => void`) or a constraint
        # (`<T extends unknown>`), where narrowing rejects calls that worked, so
        # it stays BREAKING; a covariant case like `() => unknown` is a false
        # alarm this accepts.
        if lang == "javascript" and _narrowed_from_unknown(old_ret, new_ret):
            return "warn", "return narrowed from unknown — safe for consumers"
        if lang == "python":
            return "warn", "return annotation changed — not enforced at runtime, but check the value did not change"
        return "break", "return type changed"
    return "warn", "signature reordered"


def _texts_at(repo: Path, rev: str | None, sub: str, suffix: str) -> list[str]:
    """Every `*suffix` file under `sub`, at `rev` or in the working tree. A file
    that cannot be read is skipped: its types then do not resolve, which keeps
    any change that needs them BREAKING."""
    if rev is None:
        return [f.read_text(encoding="utf-8", errors="replace")
                for f in sorted((repo / sub).rglob(f"*{suffix}"))]
    ls = subprocess.run(["git", "-C", str(repo), "ls-tree", "-r", "--name-only", rev, "--", sub],
                        capture_output=True, text=True)
    out = []
    for name in ls.stdout.split():
        if name.endswith(suffix):
            p = subprocess.run(["git", "-C", str(repo), "show", f"{rev}:{name}"],
                               capture_output=True, text=True)
            if p.returncode == 0:
                out.append(p.stdout)
    return out


def _go_package_texts(repo: Path, rev: str | None, sub: str) -> list[str]:
    """The non-test `.go` files directly in `sub` (one Go package), at `rev` or
    in the working tree. Listed at the rev itself, so a file deleted since the
    tag still contributes its types to the baseline."""
    if rev is None:
        d = repo / sub
        return [f.read_text(encoding="utf-8", errors="replace")
                for f in sorted(d.glob("*.go")) if not f.name.endswith("_test.go")]
    prefix = "" if sub in ("", ".") else sub.rstrip("/") + "/"
    ls = subprocess.run(["git", "-C", str(repo), "ls-tree", "--name-only", rev, "--", prefix or "."],
                        capture_output=True, text=True)
    out = []
    for name in ls.stdout.split():
        if name.endswith(".go") and not name.endswith("_test.go") and "/" not in name[len(prefix):]:
            p = subprocess.run(["git", "-C", str(repo), "show", f"{rev}:{name}"],
                               capture_output=True, text=True)
            if p.returncode == 0:
                out.append(p.stdout)
    return out


_GO_TYPE_RE = re.compile(r"^type[ \t]+([A-Za-z_]\w*)(?:\[[^\]\n]*\])?[ \t]+", re.M)
_GO_FIELD_RE = re.compile(r"^([A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\s+(\S.*)$", re.S)
_GO_EMBED_RE = re.compile(r"^\*?([A-Za-z_]\w*\.)?([A-Za-z_]\w*)(?:\[.*\])?$", re.S)


def _go_struct_members(body: str) -> list[str] | None:
    """A struct body (or `const`/`var` block) split into its declarations at
    depth 0: a nested `struct { ... }` or `map[K]V` stays one member. A line
    break splits only where Go would end the statement (after an identifier,
    literal, `)`, `]` or `}`), so `"a" +` continues onto the next line. None
    when it does not balance."""
    out, cur, depth, j = [], [], 0, 0
    while j < len(body):
        ch = body[j]
        if ch in "\"'`":
            end = (body.find("`", j + 1) + 1 or None) if ch == "`" else _quoted_end(body, j)
            if end is None:
                return None
            cur.append(body[j:end])
            j = end
            continue
        if ch in "([{":
            depth += 1
        elif ch in ")]}":
            depth -= 1
            if depth < 0:
                return None
        if depth == 0 and ch in "\n;":
            member = "".join(cur)
            tail = member.rstrip()
            if (ch == ";" or not tail or tail[-1].isalnum() or tail[-1] in "_\"'`)]}"
                    or tail.endswith(("++", "--")) or re.search(r"(?<![\w.])\d[\d_]*\.$", tail)):
                out.append(member.strip())
                cur = []
                j += 1
                continue
        cur.append(ch)
        j += 1
    out.append("".join(cur).strip())
    return None if depth else [m for m in out if m]


def go_struct_fields(text: str) -> tuple[dict[str, dict[str, str] | str], list[str]]:
    """(declarations, unreadable) for one Go package's source.

    A declaration maps an EXPORTED name to {exported field: type} for a struct,
    to the alias target string (`generated.X`) for `type A = B`, to "type T"
    for any other type (`type Scope string` -> "type string"), and to
    "const T" / "var T" for a package constant or variable, T its declared type
    if any. A struct tag is dropped, and a nested struct's backtick tags too:
    retagging changes the wire, not what compiles. An embedded field is named by its type,
    as Go names it. A struct whose body does not balance, holds a member read as
    neither a field nor an embedding, or embeds an unexported struct (whose
    promoted fields this scan cannot list) is unreadable.
    """
    text = strip_comments(text, "//", "go")
    decls: dict[str, dict[str, str] | str] = {}
    # A grouped `type ( ... )` block is not read; say so rather than drop it.
    unreadable: list[str] = ["type ( ... ) block"
                             for _ in re.finditer(r"^type[ \t]*\(", text, re.M)]
    for m in re.finditer(r"^(const|var)\b[ \t]*(\()?", text, re.M):
        kind = m.group(1)
        if m.group(2):
            body = balanced_slice(text, m.end() - 1, "(", ")")
            members = _go_struct_members(body) if body is not None else None
            if members is None:
                unreadable.append(f"{kind} ( ... ) block")
                continue
        else:
            members = [text[m.end():].split("\n", 1)[0]]
        last = ""
        for mem in members:
            left, eq, _ = mem.partition("=")
            head = re.match(r"\s*([\w\s,]*?\w)(?:\s+([^,\s].*?))?\s*$", left)
            if head is None:    # no name before the type or `=`
                unreadable.append(f"{kind} ( ... ) block" if m.group(2) else f"{kind} {left.strip()}")
                break
            names, typ = head.groups()
            if kind == "const" and not eq and not typ:
                typ = last          # Go repeats the previous spec's type and value
            last = typ or ""
            for n in re.split(r"\s*,\s*", names):
                if n[0].isupper():
                    decls[n] = _norm(f"{kind} {typ or ''}")
    for m in _GO_TYPE_RE.finditer(text):
        name, rest = m.group(1), text[m.end():]
        if not name[0].isupper():
            continue
        if rest.startswith("="):
            decls[name] = _norm(rest[1:].split("\n", 1)[0])
            continue
        if not re.match(r"struct\s*\{", rest):
            # `type Scope string`, interfaces, func types: the first line of the
            # underlying type, so `string` -> `int` is a change.
            decls[name] = _norm("type " + rest.split("\n", 1)[0])
            continue
        body = balanced_slice(text, m.end())
        members = _go_struct_members(body) if body is not None else None
        if members is None:
            unreadable.append(f"type {name} struct")
            continue
        fields: dict[str, str] = {}
        ok = True
        for mem in members:
            mem = re.sub(r"\s*`[^`]*`\s*$|\s*\"(?:[^\"\\]|\\.)*\"\s*$", "", mem)
            e = _GO_EMBED_RE.match(mem)
            f = _GO_FIELD_RE.match(mem)
            if e and not f:
                if not e.group(1) and e.group(2)[0].islower():
                    ok = False
                    break
                fields[e.group(2)] = "embedded " + _norm(mem)
            elif f:
                for fname in re.split(r"\s*,\s*", f.group(1)):
                    fields[fname] = _norm(re.sub(r"\s*`[^`]*`", "", f.group(2)))
            else:
                ok = False
                break
        if not ok:
            unreadable.append(f"type {name} struct")
            continue
        decls[name] = {k: v for k, v in fields.items() if k[0].isupper()}
    return decls, unreadable


def _go_kind(entry: str) -> str:
    """What a `go_field_surface` entry declares: "type", "const", "var" or "field".
    A field's type text cannot start with these keywords; "struct" alone is a type."""
    word = entry.split(" ", 1)[0]
    return "type" if entry == "struct" or word == "type" else word if word in ("const", "var") else "field"


def go_field_surface(seclai_texts: list[str], generated_texts: list[str]
                     ) -> tuple[dict[str, str], list[str]]:
    """(surface, unreadable) for package seclai (as `X`) and `generated/` (as
    `generated.X`): `Type` -> "struct" and `Type.Field` -> field type for every
    exported struct, `Name` -> its `go_struct_fields` value for every other
    exported declaration, and "type = target" for any other alias. An alias to
    an exported struct in either package (`type Req = generated.ReqApi`) is
    listed with the fields it resolves to, since that is what its callers set.
    """
    pk: dict[str, dict] = {}
    unreadable: list[str] = []
    for prefix, texts in (("", seclai_texts), ("generated.", generated_texts)):
        decls = {}
        for t in texts:
            d, u = go_struct_fields(t)
            decls.update(d)
            unreadable += [f"{n} in package {prefix.rstrip('.') or 'seclai'}" for n in u]
        pk[prefix] = decls

    def resolve(prefix: str, name: str, seen: tuple = ()) -> dict | None:
        d = pk[prefix].get(name)
        if isinstance(d, dict) or d is None or name in seen:
            return d if isinstance(d, dict) else None
        if d.startswith("generated."):
            return resolve("generated.", d[len("generated."):], seen + (name,))
        if "." in d or not re.fullmatch(r"\w+", d):
            return None   # another package, or not a plain name: not ours to read
        return resolve(prefix, d, seen + (name,))

    out: dict[str, str] = {}
    for prefix, decls in pk.items():
        for name, d in decls.items():
            fields = resolve(prefix, name)
            if fields is None:
                out[prefix + name] = d if _go_kind(d) != "field" else f"type = {d}"
                continue
            out[prefix + name] = "struct"
            for f, t in fields.items():
                out[f"{prefix}{name}.{f}"] = t
    return out, unreadable


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
    if not files:
        die(f"no client sources found for {lang} in {repo}")
    rel = [str(f.relative_to(repo)) for f in files]
    # `verify_rev` proves the rev resolves; it does not prove the sources existed
    # under these paths back then. A rename since the tag makes every `git show`
    # fail, and swallowing that yields an empty baseline — zero removed, zero
    # changed, "no breaking change" — the same confident wrong answer one level
    # down. Joined with newlines so the last line of one file cannot merge with
    # the first line of the next.
    old_parts = []
    for r in rel:
        p = subprocess.run(["git", "-C", str(repo), "show", f"{args.rev}:{r}"],
                           capture_output=True, text=True)
        if p.returncode == 0:
            old_parts.append(p.stdout)
    if not old_parts:
        die(f"none of the client sources exist at {args.rev} in {repo.name} "
            f"({', '.join(rel)}) — were they renamed since the tag?")
    old_text = "\n".join(old_parts)
    new_text = "\n".join(f.read_text(encoding="utf-8", errors="replace") for f in files)

    (old, old_unread), (new, new_unread) = scan_surface(old_text, cfg), scan_surface(new_text, cfg)
    unreadable = [(args.rev, n) for n in old_unread] + [("now", n) for n in new_unread]
    ts_decls = None
    if lang == "javascript":
        ts_decls = (ts_declarations(_texts_at(repo, args.rev, "src", ".ts")),
                    ts_declarations(_texts_at(repo, None, "src", ".ts")))
    removed = sorted(set(old) - set(new))
    added = sorted(set(new) - set(old))
    changed = [k for k in sorted(set(old) & set(new)) if old[k] - new[k]]

    # Go: exported struct fields too. The request/response types are aliases of
    # oapi-codegen structs in the public `generated` package, so removing or
    # retyping a field breaks callers while every method signature is unchanged.
    old_f, new_f = {}, {}
    if lang == "go":
        old_f, old_fu = go_field_surface(_go_package_texts(repo, args.rev, "."),
                                         _go_package_texts(repo, args.rev, "generated"))
        new_f, new_fu = go_field_surface(_go_package_texts(repo, None, "."),
                                         _go_package_texts(repo, None, "generated"))
        unreadable += [(args.rev, n) for n in old_fu] + [("now", n) for n in new_fu]
        gone_types = {k for k in old_f if old_f[k] == "struct" and k not in new_f}
        for k in sorted(set(old_f) - set(new_f)):
            if k in gone_types or k.rsplit(".", 1)[0] not in gone_types:
                removed.append(f"{k} ({_go_kind(old_f[k])})")
        added += [f"{k} ({_go_kind(new_f[k])})" for k in sorted(set(new_f) - set(old_f))
                  if _go_kind(new_f[k]) != "field" or k.rsplit(".", 1)[0] in old_f]

    fields_note = (f"; {sum(v == 'struct' for v in old_f.values())} exported struct type(s) "
                   f"at {args.rev}, {sum(v == 'struct' for v in new_f.values())} now") if lang == "go" else ""
    print(f"{repo.name} [{lang}] — {len(old)} public method(s) at {args.rev}, {len(new)} now{fields_note}")

    if unreadable:
        print(f"\nUNREADABLE ({len(unreadable)}) — public method heads, or Go declarations, "
              "this scan could not read; they are missing from the surface, so a change "
              "to them would go "
              "unreported. Fix the scanner (and add a test) before trusting this result:")
        for rev, n in unreadable:
            print(f"   {n}  (at {rev})")
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
            sev, label = classify_change(lang, gone[0], now[0], ts_decls)
        (breaks if sev == "break" else warns).append((k, label, gone, now))
    for k in sorted(set(old_f) & set(new_f)):
        if old_f[k] != new_f[k]:
            what = ("field type changed" if _go_kind(old_f[k]) == _go_kind(new_f[k]) == "field"
                    else "declaration changed")
            breaks.append((k, what, [(old_f[k], None)], [(new_f[k], None)]))

    def show(rows: list) -> None:
        for k, label, gone, now in rows:
            print(f"   {k} — {label}")
            for ret, params in gone:
                print(f"       was: {ret}" + ("" if params is None else f" ({params[:100]})"))
            for ret, params in now:
                print(f"       now: {ret}" + ("" if params is None else f" ({params[:100]})"))

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
    if unreadable:
        print(f"\n{len(unreadable)} unreadable declaration(s) — the result above is incomplete.")
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

    A RELATIVE path is resolved against `fallback` first, exactly as `load_spec`
    resolves it. `--spec ../seclai-python/openapi/seclai.openapi.json` is the
    form `load_spec`'s own error message recommends, and treating it as
    repo-root-relative sent `git show` looking for `../seclai-python/...` inside
    seclai-csharp, so `models --since` died on the one workflow it exists for.
    """
    full = spec if spec.is_absolute() else (fallback / spec)
    if not full.exists():
        # Nothing on disk to locate; assume repo-root-relative, as before.
        return fallback, str(spec)
    try:
        top = subprocess.check_output(
            ["git", "-C", str(full.parent), "rev-parse", "--show-toplevel"],
            stderr=subprocess.DEVNULL, text=True).strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        die(f"{full} is not inside a git work tree; --since needs one")
    root = Path(top)
    try:
        return root, str(full.resolve().relative_to(root.resolve()))
    except ValueError:
        die(f"{full} resolves outside its own work tree {root}; --since cannot read it")


def affix_free(name: str) -> str:
    """A schema name with its module qualification and `Api`/`Model` affixes
    dropped, as seclai-csharp names its hand-written models:
    `routers__api__agents__UpdateAgentRequest` -> `UpdateAgentRequest`,
    `SlackTriggerConfigApiResponse` -> `SlackTriggerConfigResponse`,
    `CloudDriveResponseModel` -> `CloudDriveResponse`. Only a trailing `Model`
    and an `Api` before the last word go, so `EmbeddingModelListResponse`,
    `ApiVersionResponse` and `UpdateApiVersionRequest` keep theirs. `models`
    matches classes by it.
    """
    name = short_label(name)
    name = re.sub(r"(?<=[a-z0-9])Model$", "", name)
    return re.sub(r"(?<=[a-z0-9])Api(?=[A-Z][a-z0-9]*$)", "", name)


def spec_schema_index(spec: dict) -> dict[str, list[tuple[str, set[str]]]]:
    """Short schema name -> [(full name, property names)].

    The spec qualifies some schemas by module (`routers__api__agents__X`), and
    two modules can export the same trailing name, so a short name may resolve
    to more than one schema.
    """
    index: dict[str, list[tuple[str, set[str]]]] = {}
    for full, sch in (spec.get("components", {}).get("schemas") or {}).items():
        index.setdefault(short_label(full), []).append((full, set(sch.get("properties") or {})))
    return index


def _element(label: str) -> tuple[bool, str]:
    m = re.match(r"^array<(.+)>$", label)
    return (True, m.group(1)) if m else (False, label)


def operation_models(repo: Path, lang: str, spec: dict, classes: set[str]) -> dict[str, dict[str, set[str]]]:
    """Model class -> {full schema name -> client methods} from the client itself.

    A method that returns class X for an operation whose response is schema S,
    or takes X as a parameter where the operation's request body is S, says X
    models S. That is the client's own declaration, so it settles which of two
    same-named schemas (`routers__api__sources__FileUploadResponse` and
    `routers__api__contents__FileUploadResponse`) a class is, and links a class
    named nothing like its schema (`ContentFileUploadResponse`). A list on one
    side and an object on the other links nothing: that is a `returns` finding.
    """
    cfg = LANGS.get(lang)
    if not cfg or "return_head_re" not in cfg:
        return {}
    ops = {}
    for raw, item in spec.get("paths", {}).items():
        for verb, op in item.items():
            if verb in ("get", "post", "put", "patch", "delete"):
                ops[(verb.upper(), normalise(raw))] = op
    links: dict[str, dict[str, set[str]]] = {}

    def link(cls: str, label: str, mname: str) -> None:
        if cls in classes and "<" not in label and label not in ("object", "none", "unknown"):
            links.setdefault(cls, {}).setdefault(label, set()).add(mname)

    texts = [f.read_text(encoding="utf-8", errors="replace") for f in source_files(repo, cfg)]
    for mname, body, call, _request_body, _helper in request_blocks(texts, cfg):
        op = ops.get(call)
        if op is None:
            continue
        declared, unread = read_block_return(body, cfg)
        if declared and not unread:
            got_arr, got = _element(normalise_type(declared, lang))
            for code in ("200", "201", "202"):
                if code in op.get("responses", {}):
                    want_arr, want = _element(_schema_label(_json_schema(op, "response", code)))
                    if got_arr == want_arr:
                        link(got, want, mname)
                    break
        req = _schema_label(_json_schema(op, "request"))
        anchor = re.search(cfg["method_re"], body)
        sliced = _after_params(body, body.find("(", anchor.end() - 1)) if anchor else None
        if sliced and req not in ("none", "object"):
            types = {a.split()[0] for a in split_args(sliced[0]) if a.split()}
            for t in types & classes:
                link(t, req, mname)
    return links


def match_models(classes: set[str], spec: dict,
                 links: dict[str, dict[str, set[str]]]) -> dict[str, list[str]]:
    """Model class -> the full schema names it may model ([] when none).

    By name first: the exact short name, else the `affix_free` spelling,
    ignoring case (`HttpValidationError` is `HTTPValidationError`). A
    class the client links to operations (`operation_models`) is narrowed to
    the named candidates it is used for; when it is used for none of them, its
    links replace them. More than one result is ambiguous and is reported so;
    picking one would check a model against a schema it may not be.
    """
    fulls = list((spec.get("components", {}).get("schemas") or {}))
    exact: dict[str, list[str]] = {}
    loose: dict[str, list[str]] = {}
    for full in fulls:
        exact.setdefault(short_label(full), []).append(full)
        loose.setdefault(affix_free(full).lower(), []).append(full)
    out = {}
    for cls in classes:
        named = exact.get(cls) or loose.get(affix_free(cls).lower()) or []
        used = set(links.get(cls, {}))
        if used:
            named = sorted(used & set(named)) or sorted(used)
        out[cls] = named
    return out


def cmd_models(args) -> int:
    repo = Path(args.repo).resolve()
    if repo.name in NOT_APPLICABLE:
        print(f"{repo.name}: not applicable — {NOT_APPLICABLE[repo.name]}")
        return 0
    lang = args.lang or detect_lang(repo)
    if not lang:
        # `MODEL_LANGS.get(None)` is None, so without this an undetectable repo
        # printed "models are generated; nothing to check" and exited 0.
        die(f"cannot detect SDK language in {repo}")
    cfg = MODEL_LANGS.get(lang)
    if not cfg:
        print(f"{repo.name} [{lang}] — models are generated; nothing to check by hand.")
        return 0

    spec = load_spec(args.rev, args.spec, repo)
    props = {full: set(sch.get("properties") or {})
             for full, sch in (spec.get("components", {}).get("schemas") or {}).items()}

    # `--since` narrows the report to properties the schema GAINED since a rev.
    # Without it the C# models show 27 findings, every one a coverage gap that
    # predates this sync — a signal nobody can act on, and a gate nobody keeps.
    # With it the question becomes the one that actually bit: did this sync
    # leave a model behind? Six were, in 2026-07, and only review caught them.
    gained: dict[str, set[str]] | None = None
    if args.since:
        spec_repo, rel = git_location(Path(args.spec), repo)
        old = load_spec(args.since, rel, spec_repo)
        was_full = {f: set(s.get("properties") or {})
                    for f, s in (old.get("components", {}).get("schemas") or {}).items()}
        was_short = spec_schema_index(old)
        gained = {}
        for full, now in props.items():
            # A schema that only gained or lost its module qualification since
            # the rev is the same schema: compare it with its old short name.
            was = was_full.get(full)
            if was is None:
                was = set().union(*(p for _, p in was_short.get(short_label(full), [])))
            if now - was:
                gained[full] = now - was

    files = sorted(repo.glob(cfg["glob"]))
    scope = f", {len(gained)} schema(s) changed since {args.since}" if gained is not None else ""
    print(f"{repo.name} [{lang}] — {len(files)} model file(s), {len(props)} spec schemas{scope}")

    models: dict[str, set[str]] = {}
    for f in files:
        text = f.read_text(encoding="utf-8", errors="replace")
        m = re.search(cfg["class_re"], text)
        if m:
            models[m.group(1)] = set(re.findall(cfg["prop_re"], text))
    matches = match_models(set(models), spec, operation_models(repo, lang, spec, set(models)))

    missing, extra, unmatched, ambiguous = [], [], [], []
    for cls in sorted(models):
        have, cands = models[cls], matches[cls]
        if not cands:
            unmatched.append(cls)
            continue
        if len(cands) > 1:
            ambiguous.append((cls, cands))
        # With more than one candidate, only properties common to all of them are
        # unambiguously missing. A gate that guesses is a gate that gets muted.
        want = set.intersection(*(props[c] for c in cands))
        if gained is not None:
            want &= set().union(*(gained.get(c, set()) for c in cands))
        gone = want - have
        if gone:
            missing.append((cls, sorted(gone), cands))
        if len(cands) == 1 and gained is None:
            surplus = have - props[cands[0]]
            if surplus:
                extra.append((cls, sorted(surplus)))

    if missing:
        print(f"\nMISSING PROPERTIES ({len(missing)}) — the spec declares these and the model does not:")
        for cls, gone, full in missing:
            where = f"  [{', '.join(full)}]" if full != [cls] else ""
            print(f"   {cls}{where}\n       {', '.join(gone)}")
    if extra:
        print(f"\nWARN — EXTRA PROPERTIES ({len(extra)}) — in the model, not in the schema:")
        for cls, surplus in extra:
            print(f"   {cls}: {', '.join(surplus)}")
    if ambiguous:
        print(f"\nWARN — AMBIGUOUS ({len(ambiguous)}) — the class matches several schemas and "
              "nothing narrows it to one; only properties all of them declare are checked:")
        for cls, cands in ambiguous:
            print(f"   {cls}: {', '.join(cands)}")

    if unmatched:
        # Not a finding: the spec declares some request bodies inline rather than
        # as a named component, so CreateSourceRequest and UpdateSourceRequest —
        # both of which WERE left stale in 2026-07 — have nothing to match
        # against. Say so, and name them, so the count is never read as full
        # coverage.
        print(f"\n   note: {len(unmatched)} model class(es) have no named schema "
              f"and are unchecked (inline request bodies land here)"
              + ("." if args.quiet_unmatched else ":\n   " + ", ".join(unmatched)))

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
                   help="count the model classes with no schema without naming them")
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
