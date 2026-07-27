# ADR 0012: Targets set by tool, not by user

- **Status:** Accepted
- **Date:** 2026-06-18

## Context

Improving at typing is a process of building familiarity, from familiarity accuracy, and from accuracy speed. Many tools have mechanisms for managing the accuracy or words-per-minute (WPM) targets, and often suggest for the user to raise these targets over time. But, in my opinion, this manual intervention should be unnecessary. It is a distraction from the core activity, improving at typing, and the suggested procedures for improving should instead be built into the tool itself.

## Decision

The tool will automatically set any given thresholds, be it accuracy, WPM, or anything else. The targets will be based on the user's current performance, and will be highly dynamic - the tool will not hold the user back if they are showing rapid improvement, but it also will not move the user forward it they are not improving.

This follows the existing approach of 'unlocking' keys or ngrams once the user has met the targets of the previous items.

For this to be effective, it will be necessary to provide feedback to the user, such that they know what the targets / thresholds are and why.

### Target-WPM mechanism

The target speed (`TargetWPM`) is the one concrete numeric threshold the engine manages. It moves as follows:

- **Start fixed.** A new user begins at `STARTING_TARGET_WPM` (40). While the alphabet is still filling in, the target is held constant - a beginner should not be chasing a moving speed goal while still learning where the keys are. Breadth (unlocking letters) comes first; speed pressure comes after.
- **Raise gate.** Once **every** key is unlocked **and** the mean key `decayedScore` is at or above `TARGET_RAISE_SCORE` (0.85), the engine raises `TargetWPM` by `TARGET_WPM_STEP` (5 WPM). Because the underlying instant score is accuracy-weighted (`W_ACCURACY` 0.7 / `W_SPEED` 0.3), clearing that bar already implies the user is both accurate and at-or-near the current target speed - so the raise is gated _mostly on accuracy, with speed contributing_, as intended. At most one raise per completed lesson, mirroring the one-unlock-per-call discipline.
- **Self-limiting.** There is no hard cap. The target rises only while the user keeps clearing the bar at the new speed; when they stop improving, mean score falls below the gate and the target holds. This delivers the "constant small challenge - never held back, never pushed past demonstrated ability" intent without any manual control.

The concrete constants, evaluation point, and order of operations are specified in the [engine design doc](/docs/engine.md) (the engine spec), keeping this ADR the _why_ and that doc the _how_.

## Consequences

**Positive**

- User is able to focus only on improving their typing, allowing the tool to respond to their demonstrated competency and focus where improvement is needed, providing a constant small challenge to get incrementally better.
- Decreased interface, application logic, and database complexity.

**Negative**

- Decreased flexibility for the user, they do not have the ability to manually set targets / thresholds.
- The raise gate's constants (`TARGET_RAISE_SCORE`, `TARGET_WPM_STEP`) need tuning: too eager and the target outruns the user, too slack and progress stalls. Validated against the simulated-user harness in the [engine design doc](/docs/engine.md) rather than guessed.
