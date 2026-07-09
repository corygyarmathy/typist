# ADR 0017: Hybrid dependency management (Dependabot + dedicated nix flake updater)

- **Status:** Proposed
- **Date:** 2026-06-29
- **Related Artefacts:**
  - Supplements: [`ADR 0006`](/docs/adr/0006-docker-and-nix.md)
  - Implemented by: `.github/dependabot.yml`, `.github/workflows/flake-update.yml`, `.github/workflows/ci.yml` (`dev-shell` job)

## Context

Dependency updates span four surfaces: the Nix `flake.lock` (dev-shell tooling now, build later), Go modules and the Docker base image (what actually ships), and pinned GitHub Actions (the CI supply chain).

A single tool covering all four is attractive, but difficult to find one that effectively manages each one - especially nix. A nixpkgs bump implies closure-level package deltas (`go 1.26.3 -> 1.26.4`, ...) that ought to be recorded in **git history**, not only in an external tool (i.e. GitHub PR comments). The reasons are concrete:

- A bisect or rollback needs the package deltas in `git log`; recomputing them per-commit during an investigation is complex, slow, and annoying.
- A reviewer approving a flake bump should see _what packages changed_ in the PR without running commands by hand.

Dependabot offers no hook to attach computed content (the closure diff) to its PR.

Nix has its own complexities and the native tools are best at handling them. The other three surfaces have no such need and Dependabot handles them turnkey, including security updates.

## Decision

Split dependency management by where each tool is strong:

- **Dependabot** (`.github/dependabot.yml`) owns `gomod`, `docker`, and `github-actions` - periodic version updates, plus Dependabot security updates on the shipped surface (`gomod`, `docker`). The `nix` ecosystem block is removed.
- **A dedicated `flake-update.yml` workflow** owns the flake. On a periodic schedule (and on demand), it builds the dev-shell closure before and after `nix flake update`, computes `nix store diff-closures`, and opens/updates a single PR via `peter-evans/create-pull-request` with the closure delta written **into the commit message body**. The same build that produces the "after" closure validates that the updated shell builds, so a broken bump fails the workflow and no PR is opened. An empty closure diff means `nix flake update` only refreshed `flake.lock` metadata (`rev`/`narHash`) without changing any package version; in that case the workflow opens no PR, since there is nothing to review or record in git history.

The closure delta is therefore in the commit from the moment the PR exists - durable in `git log` independent of the merge strategy or any repo setting.

The workflow checks out and creates the PR using a stored token (`secrets.FLAKE_UPDATE_TOKEN`, a fine-grained PAT or GitHub App token with `contents: write` + `pull-requests: write`) rather than the default `GITHUB_TOKEN`, so the resulting PR triggers `ci.yml` like any normal PR. This preserves the property that flake bumps run full CI (lint/test/build _and_ the `dev-shell` job), not just the in-workflow shell build.

## Consequences

**Positive**

- The nixpkgs package delta is in the commit body from PR creation - no `workflow_run` indirection, no dependency on a squash-message repo setting, no force-pushing or amending.
- The updater both updates and validates: a flake bump that breaks the dev shell fails before any PR is opened. PRs exist only when there is a valid update to merge.
- Each tool is used where it is strong: Dependabot's turnkey security updates on the shipped surface; a Nix-aware workflow on the flake.
- An empty diff is a pure metadata bump (identical closure, nothing to review) and opens no PR; only a non-empty diff - real package changes worth a look - produces one. No-op `flake.lock` churn never reaches review.

**Negative**

- Two tools instead of one, additional complexity.
- Requires one stored credential (`FLAKE_UPDATE_TOKEN`) so the flake PR triggers CI. A PAT expires and must be rotated; a GitHub App token avoids expiry at the cost of more initial setup.
- The flake updater opens one PR tracking `nixos-unstable`'s head, doesn't create separate PRs for each package.
- Per-run cost: the dev-shell closure is built twice (before + after).
- `flake.lock` metadata-only refreshes (`rev`/`narHash` churn with an identical closure) are never committed on their own; the lock advances only on the run where a package version actually changes, which also carries any pending metadata. This is intentional - the lock metadata has no value absent a closure change - but it means `flake.lock` is not kept maximally fresh between meaningful bumps.

## Alternatives considered

- **Dependabot for everything.** Rejected: no hook to attach the closure diff to a PR, forcing a two-stage `workflow_run` pipeline and the squash-message coupling. The diff stays outside the commit until merge and only if a repo setting is right.
- **`DeterminateSystems/update-flake-lock` as the flake updater.** The community-standard wrapper, and the first choice for the flake updater. Rejected because its commit message is a static input set _before_ `nix flake update` runs, so it cannot carry the post-update `diff-closures` output in the commit body - only in the PR body, which reintroduces the "delta not in git history" problem. It wraps `peter-evans/create-pull-request`; using that directly is one layer fewer and lets the commit body carry computed content.
- **Renovate for everything.** Viable and powerful: its `postUpgradeTasks` can run `nix store diff-closures` and fold the result into the same commit, and it adds regex managers and a dependency dashboard. Rejected for now because the capability that matters here (`postUpgradeTasks`) requires self-hosting the Renovate runner, which is more operational surface than this project warrants at this stage.
- **Treat the diff as recomputable and skip git history.** Rejected: a non-empty closure change is the reviewer's primary approval signal and a bisect's primary breadcrumb; both are degraded if the delta is not in the commit.
