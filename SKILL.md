---
name: seclai-sdk-review-triage
description: Triage PR review comments in a Seclai SDK repo (seclai-python, seclai-javascript, seclai-go, seclai-csharp, seclai-cli, seclai-mcp) or in sdk-tools, by evidence class, by how many SDKs share the site, and by whether it touches published surface, into fix here / fix in every SDK / ask / nothing / upstream / push back. Use when addressing review comments or unresolved PR threads, when a /code-review result is pasted in, before replying to or resolving threads, before reporting a review round, and when review rounds keep finding new problems.
---

# Triaging SDK review comments

> **Vendored file — do not edit in place.** The canonical copy lives in the
> `seclai/sdk-tools` repository at `skills/seclai-sdk-review-triage/`, and is
> mirrored into each SDK repo with `git subtree`. Edits made here are reported as
> drift by `sdk-tools/sync.sh --check` and are overwritten on the next pull.

Adapted from the `seclai` repo's `pr-review-triage` for SDK work, where the stakes
are published surface and wire behaviour, and one finding usually exists in four
SDKs at once.

**Fetch the PR's own threads first.** A pasted `/code-review` result is a second
input, not a substitute:

```
gh api graphql -f query='{ repository(owner:"seclai",name:"<repo>") {
  pullRequest(number:<n>) { reviewThreads(first:100) { nodes {
    id isResolved comments(first:1){ nodes { path line databaseId body } } } } } }'
```

**State what the PR is for before reading a finding**, a sentence each: its goal
(sync to which spec, plus any backlog the user agreed to), the design it chose, and
what it ruled out. Weigh every finding against that. A finding that re-opens a
decision already made in this PR or an earlier round is answered by citing the
decision, not by changing the code back.

**A spec-sync PR changes only what the spec diff changed**, plus backlog the user
agreed to before the review. A Proven finding about behaviour that is already in the
last release goes to an issue, however cheap the fix looks, and the thread links it;
other classes keep their §1 disposition. A Contract finding on prose this PR wrote is
still fixed here.

**Read `rules.md` next to this file** (each SDK `CLAUDE.md` imports it). Its
cold-review gate runs on your response diff before you reply (§7).

## 1. Classify each finding

| Class | Test | Disposition |
|---|---|---|
| **Proven** | You can write a failing test today from input that occurs | Fix (§3) |
| **Contract** | A published statement is false: README, CHANGELOG, docstring, SKILL.md, a comment | Make it true once, by the smallest change; deleting the claim counts |
| **Latent** | Provably wrong when reached; you can name the absent condition | **Nothing**, unless it would ship a break or a wrong request to consumers (§4) |
| **Speculative** | Hypothesised from code shape | Nothing |
| **Opinion** | Naming, reuse, dedupe, where a rule lives, a narrower type | Internals: **nothing**. New public surface: **ask** (§2) |
| **Consequence** | A true statement about an effect elsewhere | Record it in the reply |

The class is yours to assign; a label arriving with the finding is an input.

**Check that the finding is right before acting.** Name the command whose output
would prove your answer wrong, and run it: build the consumer, run the audit, read
the backend handler. Confident it is wrong: push back and change nothing. Unsure:
say which part you doubt, then act. Right diagnosis with a wrong suggested fix: take
the diagnosis, replace the repair, say both.

## 2. What it touches decides the process

| It touches | Then |
|---|---|
| **Published surface**: a public name or signature, what goes on the wire (query keys, null vs omitted, body shape), what a response deserializes into, version constants, a CHANGELOG heading or **Breaking:** label | **Decide, don't patch.** Check `surface` against the last tag; ask the user when the fix trades one consumer outcome for another |
| **Internal code** in this PR | Fix if proven or contract; otherwise nothing |
| **Trivial**: an internal comment, a test name, helper dedupe, a log line | One pass (delete, or the smallest true change) or nothing. Never a second pass |

**Unreleased public surface can still change freely; released surface cannot.**
An opinion on a public name added in this PR is a decision to make now, with the
user, because a rename after release is breaking. The same opinion on a released
name is a major-version question.

**A finding on generated code** (`seclai/_generated/`, `src/openapi.ts`,
`generated/seclai.gen.go`) is fixed in the generator's flags or upstream in the
spec, never in the output.

**A spec defect goes upstream.** An undocumented null, a response schema that inlines
a string instead of `$ref`-ing the enum, untyped list items: reply on the thread,
and suggest the spec fix in `seclai`. Don't work around it in four SDKs unless the
user asks.

## 3. One SDK, or all of them?

**Count the sites across the sibling SDKs before fixing.** Most findings are about a
behaviour the four SDKs share: an optional `agent_id`, how `days` and the dates
combine, how a null clears a field. Fixing it in one SDK leaves three wrong and
invites the next round to find them. Check the siblings, fix them all the same way
in this round, or say the count in the reply ("one of four SDKs").

Two or more sites, or a fix that needed another fix where both changed behaviour,
means a design question first: give the options and a recommendation, and let the
user choose before any code.

## 4. Latent findings and the audit tool

A latent finding gets **nothing** unless learning it the breaking way would ship a
break or a wrong request to consumers, and nothing else would catch it first. Say
the assessment on the thread, so the reviewer knows it was weighed, not missed.

For `sdksync.py` in sdk-tools, sdk-tools' CLAUDE.md governs. In short: `surface` and
`returns` report what they cannot read as UNREADABLE, so a parsing limit there is a
false alarm, never a silent miss. A latent parsing finding is not reportable unless
it gets past that guard. The stop condition is **no finding that changes an audit
result on a real SDK file**. Check it by running the audits on all four checkouts
before and after.

## 5. Measure the round before fixing it

Signals that the rounds are churning instead of converging:

- the finding count stays flat or rises across three rounds;
- a fix needed another fix, and both changed behaviour;
- the response diff approaches the size of the change under review;
- more than half the findings land on the previous round's fixes (on a full-diff
  review; a delta review lands there by construction).

**Two firing, or the fix-needs-a-fix one alone: stop fixing finding by finding.**
Name the signal, name the requirement or design that is unclear, propose the way
out (often: make the failure mode loud, so latent cases become visible false
alarms; or split the adjacent work), and let the user decide.

**The default way out is to freeze the mechanism.** When a fix to a mechanism (a
helper's timeout, fallback, retry or error rules) needs a fix, and both changed
behaviour, propose that this PR stop changing it: later findings on it go to one
issue listing every affected repo, and a fix that created a regression is reverted
rather than fixed forward.

After the first round, review only the delta since the last reviewed commit, not
`HEAD~1..HEAD` counted by hand.

## 6. Your own fixes

**Delete, or add a guard. Do not add a named concept** (a new function, type,
option, or config key) to the code under review to answer a finding. Each new name is
surface for the next round. If a fix seems to need one, stop and say so.

**Do not widen scope to make a test easy.** Supporting a shape no SDK has, because a
test needed it, is new work with its own review rounds.

**A fix that adds or changes a mechanism is a new change, not a repair**: a new
option, timeout, fallback, retry, client or error type. Do not write it in answer to
a finding. Write down what it must do for each case (each way a stream or poll ends,
and its outcome, per SDK), agree it with the user, then implement it in every SDK
at once, in its own PR unless the user puts it in this one. Without that, each review round re-derives the design and finds the next
edge case.

**A test added for a finding must fail without the fix.** Undo the fix, watch the
test fail, restore it. Say that you did. That proves the fix does what it says, not
that it is right; when the cold-review gate runs, its Break prompt checks the second.

## 7. Replies and threads

- One reply per thread, posted after lint, tests and the cold-review gate on the
  response diff (`rules.md`) pass. The user commits and pushes, so say the change is in
  the working tree.
- **Resolve only after the user reports the push** and
  `gh pr view --json headRefOid` shows the new head, and only threads you changed.
  A pushback, a doubt, or a question for the user stays open unless the user says to
  resolve it.
- **Lead with the disagreement** when there is one. Dispute the claim, not the
  reviewer.
- A review written against unpushed commits has no threads. Reply with one PR
  comment once the commits are pushed.

## 8. Reporting the round

Lead with the class counts and how many findings changed behaviour. For example:
"9 findings: 2 proven, 4 latent, 1 contract, 2 opinion; 2 changed behaviour". When
none changed behaviour, say so in the first line: that is the stop condition. Then
list the pushbacks and doubts before the fixes. Report the classes you assigned,
not the ones the review claimed. Say what the cold-review gate found on the
response diff. End with:

```
Findings that change behaviour: N
Sibling SDKs checked: N of 4 — fixed in N
Pushed back / asked: N
Below the bar: N — said on the thread
Fixes verified by undoing them: N of N
Cold-review gate on the response diff: N findings, N changed behaviour | skipped (<10 lines)
```
