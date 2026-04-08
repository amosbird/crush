---
name: superpowers-tdd
agent: superpowers
description: "Superpowers TDD workflow: Red-Green-Refactor cycle with iron-law discipline. Activate when adding new functionality, fixing bugs, or when the user asks for test-driven development."
---

# Test-Driven Development — The Iron Law

**NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST.**

This is not a suggestion. This is the default workflow for all new functionality.

## The Red-Green-Refactor Cycle

### 1. RED — Write a Failing Test

- Write ONE minimal test that defines the expected behavior
- Run it. Watch it fail
- Confirm it fails for the RIGHT reason — not a syntax error, not a missing import, but the actual missing behavior
- If it passes immediately, your test is wrong — it tests existing behavior, not new behavior. Fix the test

### 2. GREEN — Write the Minimum Code

- Write the SIMPLEST code that makes the test pass
- No more, no less. No "while I'm here" additions
- Run the test. Confirm it passes
- Confirm no OTHER tests broke

### 3. REFACTOR — Clean Up

- Only after green. Never while red
- Remove duplication, improve naming, simplify
- Run tests after every refactoring step
- If tests break, undo and try a smaller refactoring

## Hard Rules

### Violations That Require Starting Over

- You wrote implementation code before writing a test → **Delete the implementation. Write the test first.**
- You want to keep untested code "as reference" → **Delete it. You will write it again after the test, and it will be better.**
- You wrote a test that passes immediately on new code → **Your test is wrong. It tests existing behavior or is tautological. Fix it.**

### Never Acceptable

- "I'll add tests after" — Tests written after implementation prove nothing. They verify what you remember, not what matters. They pass immediately, which means they never proved they could fail.
- "This is too simple to need a test" — Simple code has edge cases. Write the test. It takes 30 seconds.
- "The test would just duplicate the implementation" — Then your test is testing implementation details, not behavior. Rewrite it to test observable outcomes.

## Test Quality Self-Check

Before considering tests "done", verify every test against this checklist:

1. **Does it test behavior, not implementation?** — If you renamed an internal function and the test broke, it's testing implementation. Tests should break only when observable behavior changes.
2. **Does it fail when the implementation is wrong?** — Delete the core logic. Does the test still pass? If yes, the test is worthless.
3. **Does it cover more than the happy path?** — You need at least: one success case, one error/edge case, one boundary condition.
4. **Are assertions meaningful?** — `!= nil` and `!= undefined` are rarely sufficient. Assert on specific values, specific error types, specific state changes.
5. **Would a stranger understand what behavior it protects?** — Test names and assertions should read as a specification.

## Bug Fix Workflow

When fixing a bug, TDD applies with one extra step:

1. **Reproduce** — Write a test that reproduces the bug. Run it. Confirm it fails.
2. **Fix** — Write the minimum fix to make the test pass.
3. **Verify** — Run the full test suite. Confirm no regressions.
4. **Review** — Is this a root-cause fix or a symptom patch? If symptom, dig deeper.

## When to Scale Back

For trivial changes (typo fixes, config updates, comment-only edits), skip TDD. Use judgment — the goal is quality, not ceremony. If you're unsure whether something is "trivial", it isn't. Write the test.
