# seclai-go

## Changing and reviewing code

The rules for writing a change (Gate 3, a cold review of your own diff before you
report) and for producing review findings are shared by every SDK repo. They are
vendored from `sdk-tools` with the review-triage skill:

@.claude/skills/seclai-sdk-review-triage/rules.md

## Git and PRs

- **Never commit or push.** No `git commit`, `git push`, `gh pr create`, no merging, not after finishing a task, not after addressing review comments, not after checks pass. The user commits all work themselves. Leave the changes in the working tree and say the work is ready. Do not offer to commit either.
- **Never conclude that a branch has no PR. Run `gh pr view`.** `gh pr view --json number,url,state` answers it in one call. A diff target you were handed (`HEAD~1..HEAD`, a commit range, a path) says what to read, never whether a PR exists. With `/code-review --comment`, post the findings to that PR. If the command comes back empty, report that it found none, not that there is no PR.
