# Rules for changing and reviewing an SDK repo

> **Vendored file — do not edit in place.** The canonical copy lives in
> `seclai/sdk-tools` at `skills/seclai-sdk-review-triage/rules.md`. Each SDK
> repo's `CLAUDE.md` imports its own vendored copy.

Adapted from the `seclai` repo's `CLAUDE.md`. These rules apply to every task in an
SDK repo, whether you are writing the change or reviewing one. In sdk-tools they
arrive through the triage skill, and its own `CLAUDE.md` rules for audit findings
take precedence. The gates before this one are each repo's lint and test targets.

## The cold-review gate: review your own diff before you report

**Run it before you report a change as done, and before you draft replies to a
review round.** A reply is a published claim, and the next cold review finds what
this gate skips. Skip it for a diff under ~10 changed lines, and say so. Skip it when
you are a subagent: the agent that reports to the user runs it on the whole diff it
reports, including what subagents wrote. Do not report completion while a subagent
you started is still running.

Delegate it, scoped to the diff being reported. Subagents get this file but not your
reasoning, and not inheriting your belief is the point. Run four prompts in parallel:

- **Falsify** each claim your report, replies, CHANGELOG and docs are about to
  make. Hand over the claim and the artifact that settles it: the code, the spec,
  the last release tag. Never hand over your reasoning.
- **Break** each function you added or changed: "produce an input where these tests
  pass and the code is wrong." Point the prompt at what has bitten SDK changes:
  - a caller-supplied or replaced `HTTPClient`/transport;
  - cancellation versus timeout;
  - a cut or partial stream;
  - an error from a 4xx (auth) versus a 5xx;
  - the released behaviour at the last tag;
  - the sync/async twin.
- **Read** the prose and test names you added: "which assert a fact, and is each
  one true? Which prevent no wrong action a reader would otherwise take? Those are
  deleted. For each that ranges over a set (every, each, never, a count), list the
  set from the code and name a member it misses."
- **Trace** what the change does to code it does not touch: "list each caller of
  every changed function, and each variant it runs under (overloads, the async
  twin, the same helper in the sibling SDKs). Name one whose behaviour, comment,
  test or doc sentence the change makes wrong."

**Passing is reporting what came back, including nothing.** Triage its findings with
`seclai-sdk-review-triage`, the same as a reviewer's. A fix arising here runs the
gate again on that fix.

**Never predict what the next review round will find.** Report what this gate
found on the diff: "the cold-review gate found no finding that changes behaviour",
or the list it did find. That is a claim about this diff, not about the next round.

## Producing review findings

For the agent writing findings, not the author triaging them.

- **State the evidence each finding carries.**
  - *Proven*: you can write a failing test today from input that occurs.
  - *Contract*: a published statement is false (README, CHANGELOG, docstring,
    comment).
  - *Latent*: provably wrong when reached, and you can name the absent condition.
  - *Speculative*: hypothesised from code shape.
  - *Consequence*: a true statement about an effect elsewhere.
- **Say whether the behaviour is already in the last release.** Check the last tag.
  A defect the PR did not introduce is reported as pre-existing; in a spec-sync PR
  the author files it as an issue rather than fixing it there.
- **Do not report speculative hardening**, such as "a server could send X", "a
  caller could reuse Y", unbounded inputs, races or timing margins. Report a latent
  finding only when it would ship a break or a wrong request to consumers.
- **Every finding names a concrete failure**: the input or state, and the wrong
  result. If it cannot be stated that way, it is an opinion; say so.
- **Contract means false, not incomplete.** A sentence that leaves something out is
  an opinion, unless the omission makes what it says false.
- **Read the PR description and the earlier threads before reporting on code they
  cover.** Do not report a finding that reverses a decision recorded there; quote
  the decision and argue it, if you must.
- **Terminate on severity, not emptiness.** "No finding that changes behaviour or
  the published surface" is decidable, and it is the stopping point.
