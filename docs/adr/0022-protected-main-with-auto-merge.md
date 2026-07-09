# ADR 0022: Trunk-based development on a protected `main` with auto-merge

- **Status:** Proposed
- **Date:** 2026-07-07
- **Related Artefacts:**
  - Supplements: [`ADR 0017`](/docs/adr/0017-hybrid-dependency-management.md) (the forcing function)
  - Related: [`ADR 0023`](/docs/adr/0023-ci-trust-boundary.md) (the trust boundary auto-merge relies on)
  - Implemented by: branch protection on `main` (repo setting) and the auto-merge steps in the CI/updater workflows. Operational detail - exact checks, the `gh` flow, recovery - lives in [`HACKING.md`](/HACKING.md).

## Context

Development pushed straight to `main`. That is frictionless solo but leaves two gaps: CI runs on the `push` event _after_ a commit lands, so a failing change leaves `main` red until it is fixed; and there is no per-change unit - description, linked CI, rationale - of the kind the rest of the repo leans on (ADRs, closure deltas in commit bodies).

The forcing function is [ADR 0017](/docs/adr/0017-hybrid-dependency-management.md): the dependency updaters move to an **auto-merge-on-green** posture. GitHub only offers auto-merge when a pull request has an unmet merge requirement - in practice, a branch-protection rule requiring status checks. There is no "merge when CI passes" without a required-checks gate on `main`. So enabling auto-merge at all requires protecting `main`; the only open question is whether the author's own changes ride the same rule.

This is a public repository whose purpose is to demonstrate engineering practice. That reframes the usual "PR workflow is team ceremony" objection: an always-green `main` and a history of self-contained, CI-validated, narrated PRs are themselves the artefact here, not overhead adopted to look like a team.

## Decision

Adopt a **uniform** PR-based, trunk-based flow: every change - the author's and the bots' - reaches `main` through a branch, a pull request, and auto-merge on green CI. No direct pushes; no self-bypass.

- **`main` is protected** and requires a pull request whose CI checks pass before it can merge. The required checks are exactly the CI jobs that run on a pull request; the specific job list is CI config, so it lives with the workflow and `HACKING.md`, not here - renaming a job is a configuration change, not a decision.
- **The rule applies to the author too (`enforce_admins`).** The bots' PRs are already the _most_-validated changes in the repo (the flake updater builds the closure before opening a PR; Dependabot bumps are mechanical), so the likeliest source of a red `main` is hand-written work. A rule that gated the bots but exempted the author would protect the surface that needs it least.
- **Linear history; no merge commits.** `main` reads as a clean sequence of changes. Squash is the default; rebase-merge is reserved for the occasional branch whose commits are individually meaningful. The per-PR choice is a guideline, not a decision - see [`HACKING.md`](/HACKING.md).
- **Auto-merge is opt-in per pull request and scoped to trusted authors**, never a property of the branch. Green CI is a _necessary_ condition for any merge but never a _sufficient_ one for an untrusted PR. That trust boundary and its enforcement are [ADR 0023](/docs/adr/0023-ci-trust-boundary.md).

Merge strategy, exact required-check names, the everyday `gh` flow, and branch-protection recovery are operational detail and live in [`HACKING.md`](/HACKING.md), which is expected to change without an ADR.

## Consequences

**Positive**

- `main` is always green: every commit on it passed the required checks _before_ landing, not after.
- Every change is a self-contained, CI-validated, reviewable unit with a description and linked CI - a portfolio artefact, and the same mechanism that unlocks bot auto-merge (ADR 0017).
- One posture for author and bots: no special-casing, and the protection covers the surface (hand-written changes) that actually threatens `main`.

**Negative**

- **CI is the sole gate**, so the scheme is only as strong as test coverage: a regression CI does not exercise can auto-merge to `main`, handled by revert-when-discovered rather than pre-merge inspection. Accepted deliberately (see ADR 0017); it raises the stakes on keeping CI meaningful.
- Every change needs a PR, including trivial ones. The `gh` flow keeps this to a few seconds, but it is not zero.
- `enforce_admins` means a misconfigured required check (e.g. a renamed job whose context no longer matches) can block _all_ merges, including the author's, with no bypass. Recovery is an admin action; the procedure is in [`HACKING.md`](/HACKING.md). Renaming a required job means updating the protection config in lockstep.

## Alternatives considered

- **Protect `main` for the bots, let the author bypass (`enforce_admins` off).** Lower friction, but it guards the wrong surface: bot PRs are the most-validated changes, hand-written pushes the least, so exempting the author forfeits the green-`main` guarantee exactly where breakage originates - while still paying for the protection machinery. Rejected.
- **Keep direct-push to `main`, no protection.** Cannot deliver auto-merge at all (GitHub needs a required-checks rule to offer it), and leaves CI as an after-the-fact signal that can leave `main` red. Rejected.
- **Required approvals ≥ 1, CODEOWNERS, or a merge queue.** Solve team- and scale-shaped problems (distributing review, ordering concurrent merges) this repo does not have; on a solo repo they are ceremony that would deadlock (self-approval) or add operational surface for no benefit. Rejected as premature.
