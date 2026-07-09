# Hacking on typist

Operational guide for working in this repo: the day-to-day change flow, the CI
and branch-protection setup behind it, and recovery procedures.

The _why_ behind this setup is in the ADRs - [0017](/docs/adr/0017-hybrid-dependency-management.md)
(dependency management), [0022](/docs/adr/0022-protected-main-with-auto-merge.md)
(trunk-based dev), [0023](/docs/adr/0023-ci-trust-boundary.md) (CI trust
boundary). This file is the _how_, and is expected to change without an ADR as
jobs and settings evolve. Build, test, and codegen commands live in
[`AGENTS.md`](/AGENTS.md#commands).

## Everyday change flow

Every change - however trivial - lands on `main` through a branch and a pull
request that merges itself once CI is green (ADR 0022). Direct pushes to `main`
are blocked.

```bash
git switch -c short-topic-branch
# ... commit your work ...
git push -u origin HEAD
gh pr create --fill
gh pr merge --auto --squash   # merges itself when the required checks pass
```

Three commands after the work is done. The PR waits until CI is green, then
GitHub squash-merges it and deletes the branch.

### Squash vs. rebase merge

`main` keeps a linear history (merge commits are disabled repo-wide). Two
strategies remain; choose per PR:

- **Squash (default)** - `gh pr merge --auto --squash`. Use for one logical
  change, or a branch with WIP-y commits (`fix`, `wip`, `actually fix`) that
  would be noise on `main`. This is the common case.
- **Rebase** - `gh pr merge --auto --rebase`. Use when the branch's commits are
  _individually_ meaningful: each one builds, passes tests, and is one logical
  step. Preserves that narrative on `main`. Reserve it for a feature branch you
  deliberately authored as atomic commits - don't use it to dump messy WIP.

Rule of thumb: if you'd be happy to see every commit on the branch in
`git log main`, rebase; otherwise squash. When a PR feels too big to squash
cleanly, that's often a signal it should have been several smaller PRs.

## Branch protection on `main` (current settings)

These are repo settings, not files. Rationale is in ADR 0022/0023; the current
values:

| Setting                                   | Value                                   | Note                                                                                                                                                                                                                                                      |
| ----------------------------------------- | --------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Require a pull request                    | yes, **0 required approvals**           | a solo author can't approve their own PR; a non-zero count would deadlock. 0 keeps the PR + green-CI gate while allowing self-merge                                                                                                                       |
| Required status checks                    | `lint-and-test`, `dev-shell`, `codegen` | exactly the `ci.yml` jobs that run on a PR. `build-image` is excluded: it's `if: main`-gated and never reports on a PR, so requiring it would block every PR on a check that never arrives                                                                |
| Strict (require branches up to date)      | **off**                                 | with a daily bot cadence, strict would force a rebase of every open PR on each merge, for a serialization guarantee this volume doesn't need. The residual risk - a PR that passed against a slightly stale `main` - is accepted (revert-when-discovered) |
| Include administrators (`enforce_admins`) | **on**                                  | the rule applies to the author too (ADR 0022)                                                                                                                                                                                                             |
| Require linear history                    | yes                                     | merge-commit merges disabled repo-wide; squash + rebase only                                                                                                                                                                                              |
| Require conversation resolution           | yes                                     | cheap hygiene; bot PRs carry no threads, so it never blocks them                                                                                                                                                                                          |

Repo-level flags: `allow_auto_merge` and `delete_branch_on_merge` on;
merge-commit merges off.

## How the bots ride the same rail

Both bots are subject to the same required checks as any PR; the workflows only
_schedule_ the merge, never bypass the gate (ADR 0023).

- **flake updater** (`.github/workflows/flake-update.yml`) - flips on auto-merge
  for its own PR inline (`gh pr merge --auto --squash`) using `FLAKE_UPDATE_TOKEN`.
- **Dependabot** (`.github/workflows/dependabot-auto-merge.yml`) - triggered on
  `pull_request`, gated `if: github.actor == 'dependabot[bot]'`, flips on
  auto-merge with an elevated `GITHUB_TOKEN`.

## Security scanning

- **CodeQL** (GitHub default setup, a repo setting) - SAST over the Go code on
  pull requests and a schedule. Deliberately a **non-required** check: it
  annotates PRs but does not block a merge.
- **govulncheck** (`ci.yml`) - flags known vulnerabilities in dependencies that
  the code actually _reaches_. Runs inside the Nix dev shell so it uses the
  flake-pinned toolchain.
- **Dependabot** + **flake updater** - keep dependencies and their security
  fixes current (ADR 0017).

## Recovery: protection is blocking my own merges

`enforce_admins` is on with no bypass, so a misconfigured required check (e.g. a
renamed `ci.yml` job whose status context no longer matches the protection rule)
can block _all_ merges, including yours. To recover, an admin drops protection,
fixes the mismatch, and re-applies it:

```bash
# Drop protection on main (admin only), then fix and re-apply.
gh api -X DELETE repos/corygyarmathy/typist/branches/main/protection
# ... correct the required-check name / job, then re-apply protection ...
```

If you only need to correct the required-check list, prefer a targeted `PATCH`
to the `required_status_checks` contexts over deleting the whole rule.

**When you rename a required `ci.yml` job, update the branch-protection contexts
in the same change** - otherwise the next merge hangs on a check that never
reports.
