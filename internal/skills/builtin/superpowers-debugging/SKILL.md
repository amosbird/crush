---
name: superpowers-debugging
agent: superpowers
description: "Superpowers systematic debugging: 4-phase root cause investigation. Activate when encountering test failures, unexpected behavior, crashes, or any bug that is not immediately obvious."
---

# Systematic Debugging — The Iron Law

**NO FIXES WITHOUT ROOT CAUSE INVESTIGATION FIRST.**

Random "try this" fixes are forbidden. If you cannot explain WHY something is broken, you cannot fix it reliably.

## Phase 1: Reproduce and Investigate

### Reproduce
- Get a minimal, reliable reproduction. If you can't reproduce it, you can't fix it.
- Reduce to the smallest input/scenario that triggers the failure.
- Record the exact error message, stack trace, and context.

### Investigate Root Cause
- Read error messages carefully — they often contain the answer.
- Check recent changes: `git diff`, `git log --oneline -10`, `git blame <file>`.
- For multi-component systems: add diagnostic instrumentation at each layer boundary to isolate which component fails.
- Trace the data flow from input to the point of failure. Identify exactly where actual behavior diverges from expected behavior.

## Phase 2: Pattern Analysis

This phase is what separates systematic debugging from guessing.

- **Find working examples**: Search the codebase for similar code that works correctly. There is almost always a working analogue.
- **Compare against references**: Read the working code COMPLETELY, don't skim. Understand every line.
- **Identify differences**: List EVERY difference between the working code and the failing code. Don't dismiss differences as "probably not relevant" — verify each one.
- **Understand dependencies**: Map the dependency chain. A failure in module A may be caused by a change in module B that module A depends on.

## Phase 3: Hypothesis and Testing

- Form a SPECIFIC, TESTABLE hypothesis: "The function X returns nil when input Y is empty because check Z is missing" — not "something is wrong with X".
- Test ONE variable at a time. Change one thing, observe the result.
- If the hypothesis is wrong, update it based on what you learned. Don't discard the evidence.
- Keep a mental log of what you've tried and what each attempt revealed.

## Phase 4: Fix and Verify

- Address the ROOT CAUSE, not symptoms. A fix that works "for now" but doesn't address the underlying issue is not acceptable.
- Write a test that reproduces the bug BEFORE fixing it (see superpowers-tdd).
- Implement the fix.
- Verify: the reproduction test passes, the full test suite passes, no regressions.

## Escalation Rules

- **After 3 failed fix attempts**: STOP. Your hypothesis is wrong or your understanding of the system is incomplete. Step back. Re-read the code from scratch. Try a fundamentally different angle.
- **After 5 failed fix attempts**: Question the architecture. This is not a surface bug — it may be a design flaw. Consider whether the right fix is a refactor, not a patch.
- **"Quick fix for now, investigate later"**: This is a RED FLAG. Stop and follow the process. "Later" never comes, and the quick fix often masks the real problem.

## Anti-Patterns

These are signs you've left the systematic process:

- Changing random things to "see what happens"
- Copy-pasting a fix from Stack Overflow without understanding why it works
- Adding a nil check / try-catch without understanding what produces the nil / exception
- Saying "it works now" without understanding why it was broken
- Fixing a test by changing the expected value to match the (wrong) actual value
