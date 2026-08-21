# Typist API - Bruno collection

A hand-curated [Bruno](https://www.usebruno.com/) collection for exploring the
Typist API by hand. Requests are stored as OpenCollection YAML (`.yml`) files,
so this whole folder is committed to the repo and diffs cleanly in review.

## Open it

Bruno -> _Open Collection_ -> select this `api/bruno` folder. Pick the **Local**
environment (top-right) before sending anything.

## The auth flow (the point of this collection)

The public endpoints (`healthz`, `readyz`, `auth/*`) set `auth: none`.
Everything under **Progress**, **Lessons** and **Sessions** is protected and
sends a bearer token:

1. Send **Auth / Register** (first time) or **Auth / Login** (thereafter).
2. Its `after-response` script writes the returned JWT into the `token`
   environment variable via `bru.setEnvVar("token", res.body.token)`.
3. Every protected request sends `Authorization: Bearer {{token}}`
   automatically - no copy/paste.

`token` is flagged secret, so its value is not committed. `userEmail` /
`userPassword` are throwaway dev credentials, kept in one place.

## The practice loop

The four requests added for phase 4 are meant to be sent in order, and they are
the same loop the roadmap's definition of done automates:

1. **Lessons / Next Lesson** - words to type, generated from your competency.
   Pure read; send it as often as you like.
2. **Sessions / Submit Session** - the observations. The server derives WPM and
   accuracy from them and folds them into competency inside one transaction.
3. **Progress / Get Progress** - watch `samples` climb and `score` move.
4. **Sessions / List Sessions** -> **List Sessions (Next Page)** - page back
   through the history. The first stashes `sessionCursor`; the second consumes
   and refreshes it, so sending it repeatedly walks forward a page at a time.

`sessionCursor` is runtime-only like `token`, but not secret: it is an opaque
`(completed_at, id)` pair, unsigned, and every query is scoped by the
authenticated user, so it authorises nothing by itself.

## Phase 4 is still being built

**Lessons** and **Sessions** assert the behaviour those endpoints are being
built towards, not the behaviour they have today. While a route is still
stubbed it answers `501 problem+json` and its assertions fail - that is the
intended reading, and each request's `docs` block names the step in
[`../../docs/plans/phase-4-sessions.md`](../../docs/plans/phase-4-sessions.md)
that turns it green. Delete the assertions if you would rather have a quiet
collection than a checklist.

## Scope: this is an exploration tool, not a spec

This collection deliberately covers only the flows you poke by hand. It is
**not** a mirror of `api/openapi.yaml`, and it is not the thing that keeps the
server honest against the spec - that job belongs to spec-driven checks that
read `openapi.yaml` directly (the `codegen` CI job already pins the generated
server types to the spec). Keeping this collection small means drift here is
low-stakes and obvious: a stale request simply 404s while you are clicking.

When you add an endpoint to `openapi.yaml`, add a matching `.yml` request here
only if it is a flow you actually want to exercise by hand.

## Format notes

- Request files follow the OpenCollection YAML structure reference. Auth lives
  under the `http` block; scripts use `type: after-response`; declarative
  checks use the `runtime.assertions` block.
- The environment file shape (`environments/*.yml`) is a best-effort guess -
  the OpenCollection env format is not documented publicly yet. If Bruno does
  not load them, recreate the environment once via the Bruno UI
  (Configure -> Environments) and it will rewrite the file correctly.
