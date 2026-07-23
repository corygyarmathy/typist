# ADR 0024: Password Hashing with argon2id

- **Status:** Accepted
- **Date:** 2026-07-23

## Context

The auth path (ADR 0010) stores a credential the server must be able to verify but must never be able to recover, and which an attacker who exfiltrates the datastore must not be able to invert cheaply. A password is low-entropy human input, so the defence cannot rely on the secret being strong; it has to rely on making each guess expensive. That rules out plain cryptographic hashes (SHA-256 and similar), which are designed to be fast and are therefore trivially brute-forced offline with commodity GPUs.

Two further forces bear on the choice. First, the hashes will outlive the parameters chosen today: hardware gets cheaper, so the work factor will need to rise, and old hashes must remain verifiable while new ones use stronger settings. Second, the hashing primitive is deliberately expensive, which makes it a resource-exhaustion lever and a timing side-channel unless those are explicitly defended. This ADR records the decision to hash passwords with a memory-hard key derivation function (KDF) and the shape of the surrounding defences; it does not record the concrete cost parameters, which are a tuning matter expected to change.

## Decision

Passwords are hashed with **argon2id**, and the auth path is built around four commitments.

1. **A memory-hard KDF, and specifically argon2id.** Guessing is made expensive in _memory_ as well as time, which is what neutralises the attacker's asymmetric advantage: GPUs and ASICs parallelise cheaply on compute but not on large per-guess memory. argon2id is chosen over the other memory-hard candidates because it is the PHC competition winner and the modern default recommendation, and because the `id` variant is a hybrid - it takes argon2i's resistance to side-channel (memory-access) attacks together with argon2d's resistance to time-memory trade-off attacks, rather than committing to one threat model at the expense of the other.

2. **PHC-string storage.** Each stored credential is the standard PHC string, so the algorithm, version, and cost parameters travel _with_ the hash rather than living in code or config. Verification reads the parameters out of the stored value, which means old hashes stay verifiable after we raise the work factor, and the system can migrate hashes forward (rehash on next successful login) without a flag day or a schema that pins one parameter set. The self-describing format also keeps us honestly interoperable with the wider ecosystem instead of inventing a private encoding.

3. **Constant-time verification.** The comparison of the derived key against the stored key is constant-time. A byte-by-byte comparison that returns early leaks, through timing, how much of a candidate hash was correct; a constant-time compare removes that oracle so verification reveals only match/no-match.

4. **Two defences around the primitive.** Because a memory-hard hash is expensive on purpose, it is defended on two axes:
   - **A timing equaliser on login.** Every login runs a real verification on every path - including when the email is unknown - by verifying against a precomputed dummy hash. Without this, "no such user" returns fast while "wrong password" pays the full hashing cost, and that timing gap is a user-enumeration oracle. Equalising the work makes a registered email indistinguishable from an unregistered one by timing.
   - **A memory bound on concurrency.** Each derivation costs a fixed, deliberately large slice of RAM, so an unbounded burst of register/login requests is a cheap denial-of-service lever. The number of concurrent argon2 operations is capped, and excess requests wait (backpressure) rather than all allocating at once and exhausting the host. This bounds _resource use_; it is complementary to the request-_rate_ limiting deferred to ADR 0008. Relatedly, because the PHC parameters are read back from stored data, verification rejects parameter values above sane upper bounds so a malformed or hostile hash cannot coerce an arbitrarily expensive derivation.

## Consequences

**Positive**

- A datastore breach does not hand the attacker cheap offline guessing: each candidate costs real time _and_ real memory, which is the property SHA-family hashes lack.
- Cost parameters can be strengthened over time and hashes migrated forward without breaking existing credentials, because every hash carries the parameters it was made with.
- The auth path does not leak, through timing, whether an email is registered, and does not leak, through comparison timing, how close a guess was.
- The expensive primitive cannot be turned into a memory-exhaustion DoS by a request burst, nor into an amplification vector by a hostile stored parameter set.
- The storage format is a published standard, keeping the credential store interoperable and legible rather than bespoke.

**Negative**

- Every login and registration pays a deliberate CPU-and-memory cost; this is the point, but it makes the auth path the heaviest per-request work in the system and a capacity-planning input.
- The concurrency cap converts a memory-exhaustion failure mode into a latency/backpressure one under load - safer, but it means auth can slow down rather than simply scaling out, and the cap is another parameter to size.
- The timing equaliser means the login path always does hashing work even for garbage input, spending CPU to deny an oracle.
- Parameters that live in the hash can drift: without a rehash-on-login policy, a store can accumulate credentials at stale, weaker work factors, so raising the factor is necessary but not by itself sufficient.

## Alternatives considered

- **bcrypt.** Battle-tested and constant-factor-expensive, but it is only mildly memory-hard (a small fixed working set) and so is far more amenable to GPU/ASIC parallel attack than a memory-hard KDF. It also carries legacy sharp edges (a 72-byte input truncation). Adequate, but not the strongest available choice for a new system.
- **scrypt.** Genuinely memory-hard and a reasonable option, but its single combined cost knob couples memory and CPU awkwardly, and it predates and lost to argon2 in the PHC competition. argon2id gives independent, better-understood parameters and side-channel-plus-trade-off resistance in one primitive.
- **Plain or salted fast hashes (SHA-256, PBKDF2 with low iterations).** Designed for speed, which is exactly wrong for a low-entropy secret; a salt defeats precomputed rainbow tables but does nothing to slow per-guess brute force. Rejected outright.
- **A separate parameters column instead of PHC strings.** Keeps parameters with the hash, but as a private schema rather than a standard encoding - more migration and interop friction for no gain over the self-describing format.
