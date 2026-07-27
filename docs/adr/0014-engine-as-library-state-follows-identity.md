# ADR 0014: Engine as a library; state location follows identity

- **Status:** Proposed
- **Date:** 2026-06-22
- **Related Artefacts**
  - Supplements: [ADR 0010](/docs/adr/0010-unified-identity-and-jwt.md)

<!-- TODO: I'm not convinced this is the best approach. Consider it. -->

## Context

The product requirement is that the trainer is usable the instant it launches or is connected to - no sign-in wall. An unidentified user gets an ephemeral session and a note explaining that signing in (or connecting with an SSH key) will persist their progress. This is the typist equivalent of a web app that works anonymously and offers an account to save state.

[ADR 0010](/docs/adr/0010-unified-identity-and-jwt.md) fully address how this requirement would be achieved. An anonymous user has no `user_progress` row to load or save. And the REST API is stateless, so "anonymous, in-memory" state cannot survive between two requests unless it lives somewhere: a server-side ephemeral store, or the client. The browser equivalent (a cookie / `localStorage`) has a natural analogue for a TUI - a local state file.

## Decision

Adopt one rule: **`internal/engine` is a pure library; the API is that library plus persistence and auth; state lives wherever the user's identity says it should.**

| Surface                   | Identity         | State lives                                          | Engine runs           |
| ------------------------- | ---------------- | ---------------------------------------------------- | --------------------- |
| Standalone TUI, signed in | password -> JWT  | Postgres, via the API                                | server                |
| Standalone TUI, anonymous | none             | local state file (`$XDG_STATE_HOME/typist/`) | client, in-process    |
| SSH TUI, public key       | `ssh_key` -> JWT | Postgres, via the API                                | server                |
| SSH TUI, no key           | anonymous        | in-memory, discarded on disconnect                   | sshd host, in-process |

Identified users (password or SSH key) go through the API exactly as [ADR 0010](/docs/adr/0010-unified-identity-and-jwt.md) describes; their state is persisted centrally and the SSH demo still proves the real API works end to end. Anonymous and offline users run the same engine in-process against ephemeral or locally-filed state and never touch the database - which is also what keeps the public SSH surface safe from table-flooding ([ADR 0010](/docs/adr/0010-unified-identity-and-jwt.md)), now a property that falls out of the model rather than a special case.

This supplements [ADR 0010](/docs/adr/0010-unified-identity-and-jwt.md). The "one client codebase, one API auth model" story holds for identified users. The single addition is that the engine, being a pure library with no I/O, also runs in-process for the anonymous / offline path - where there is by definition nothing to authenticate and nothing to persist.

## Consequences

**Positive**

- The tool is usable immediately on every surface, with no sign-in wall - the core product requirement.
- The anonymous-never-persists rule falls out naturally: anonymous state has nowhere to be written, so the database is untouched without special-casing the API.
- `internal/engine` having two consumers (in-process and behind the API) is a clean property of a pure library, and one worth showing: the same domain code runs in two deployment modes.
- The standalone TUI gains useful offline behaviour for free - it works with no server at all, and signing in is what opts a user into sync.

**Negative**

- The client has two code paths (local-engine vs API), branching on identity. Mitigated by the branch being narrow and along a clean line: anonymous -> local; identified -> API. The UI above and the engine below are shared.
- Anonymous progress is not portable across machines or connections, and SSH-anonymous progress is lost on disconnect. This is intended: persistence is what you sign in to get, and the UI says so.
- Importing an anonymous local-file user's progress into a real account on first sign-in is a possible future feature; v1 starts the account fresh. Noted, not built.

## Alternatives considered

- **Server-side ephemeral store for anonymous users** (in-memory map keyed by session, TTL-evicted). Rejected for v1: it makes the API stateful for the anonymous path, loses progress on restart, and does nothing for the standalone client's offline case. The local-file approach gives the standalone client a better property for less server complexity.
- **Run the engine in-process for everyone; make the API a stateless "compute-from-supplied-state" service.** Rejected: it pushes persistence to every client and discards the central, synced account model the signed-in experience depends on.
- **Keep ADR 0010 literally (no in-process engine) and force anonymous users through the API with a magic ephemeral identity.** Rejected: it adds an anonymous-token code path and a non-persisting branch inside every persistence call, for a worse outcome than simply running the pure library locally.
