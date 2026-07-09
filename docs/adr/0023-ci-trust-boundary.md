# ADR 0023: Untrusted code and the CI trust boundary in a public repo

- **Status:** Proposed
- **Date:** 2026-07-07
- **Related Artefacts:**
  - Supplements: [`ADR 0022`](/docs/adr/0022-protected-main-with-auto-merge.md) (auto-merge relies on this boundary)
  - Implemented by: repo workflow-permission and fork-approval settings, SHA-pinned actions across `.github/workflows/`, `.github/workflows/dependabot-auto-merge.yml`

## Context

This is a public repository, so anyone can open a pull request from a fork. Two questions follow that a private repo never has to answer: whose code is allowed to _run_ in CI, and with what privileges? [ADR 0022](/docs/adr/0022-protected-main-with-auto-merge.md) makes CI the gate that auto-merges trusted changes - that gate is only sound if untrusted code cannot execute with secrets or write access, and cannot masquerade as trusted.

## Decision

Draw the boundary at **trusted authors (the maintainer and this repo's own bots) vs. everyone else.** Green CI is a _necessary_ condition for a merge, never a _sufficient_ one for an untrusted PR.

- **Auto-merge is opt-in per pull request and gated on `github.actor`.** Branch protection only _blocks_ a merge until checks pass; it never merges anything. A merge is scheduled only when a bot path calls `gh pr merge --auto` on its own PR, each gated on the actor. A human or fork PR never has auto-merge enabled - it stays green until the maintainer merges it by hand. External contributors cannot merge regardless of CI: merging needs write access forks do not have.
- **Untrusted code does not run in CI unattended.** The fork-PR workflow-approval policy is tightened from GitHub's default (`first_time_contributors`) to **all external contributors**, so _every_ outside PR's workflows are held pending a maintainer's approval before they execute - on each push, not just first-time contributors. A stranger's PR cannot even reach "passes tests" without a deliberate click.
- **No untrusted-code-with-privileges footguns.** Workflows trigger on `pull_request` (fork PRs run in the fork's context with a read-only token), never `pull_request_target`. The repo-wide default `GITHUB_TOKEN` is `read`, and the token cannot approve pull requests. Elevated write permissions (e.g. in `dependabot-auto-merge.yml`) are withheld by GitHub on fork PRs and are gated on the actor regardless.
- **Third-party actions are pinned to full commit SHAs, not tags.** A tag is mutable; a moved or compromised tag would let a third-party action run arbitrary code in CI with whatever permissions the job grants - the classic CI supply-chain vector. SHA pins make each action reference immutable. Dependabot (the `github-actions` ecosystem, ADR 0017) advances the pins and keeps the human-readable version in a trailing comment, so pinning does not mean going stale.

The net: the only changes that auto-merge are the maintainer's own and the two internal bots' already-validated bumps, and no code the maintainer has not vetted executes with privileges. An arbitrary green PR does not merge itself.

## Consequences

**Positive**

- The auto-merge convenience of ADR 0022 rests on an explicit, enforced trust boundary rather than on "CI passed."
- No path for a fork PR to exfiltrate secrets or gain write access; the third-party-action supply chain is immutable at the SHA level.

**Negative**

- The all-external-contributors policy means every outside PR needs a maintainer click before its CI runs, on each new push. Negligible on a low-traffic portfolio repo, and the direct price of not executing untrusted code unattended; it would want revisiting if the repo ever took regular external contributions.
- SHA pins are less readable than tags and rely on Dependabot to advance them; a stalled updater silently freezes action versions until noticed.

## Alternatives considered

- **Scope auto-merge to green CI rather than to trusted authors.** Simpler to state, but on a public repo it would let any green fork PR merge itself - exactly the footgun the boundary exists to prevent. Rejected.
- **`pull_request_target` for richer fork-PR automation.** Grants a read-write token and repo secrets to workflows triggered by fork PRs - the canonical CI supply-chain vulnerability, since the fork's untrusted code then runs with privileges. Rejected; the automation here does not need it.
- **Tag-pinned actions (`@v7`).** Readable and auto-updating, but mutable: the pin's security rests on the upstream owner never moving the tag and never being compromised. Rejected for the privileged surface in favour of SHA pins with a version comment.
