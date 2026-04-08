---
name: superpowers-verification
agent: superpowers
description: "Superpowers verification workflow: prove completion with evidence, not assumptions. Activate before declaring any task done."
---

# Verification Before Completion — The Iron Law

**NEVER DECLARE A TASK DONE WITHOUT PROOF.**

"It should work" is not proof. "The code looks correct" is not proof. Only actual command output is proof.

## The Verification Checklist

Before declaring ANY task complete:

1. **Run a fresh proving command** — the project's test suite or the relevant subset. Not the cached result from earlier. A fresh run.
2. **Read the FULL output** — don't assume success from partial output or the absence of errors. Read every line. Failures can hide in warnings.
3. **Check lint/typecheck** — if LSP is available, fix all errors in files you changed.
4. **Verify edge cases** — did you handle nil inputs, empty collections, boundary values, concurrent access?
5. **Re-read the original request** — does your implementation address EVERY requirement? Not just the first one?
6. **Check for unwired code** — new functions that nothing calls, new config that nothing reads, new types with no tests.

## Anti-Rationalization Rules

These are NOT acceptable substitutes for running a proving command:

| Excuse | Reality |
|--------|---------|
| "The code looks correct based on my reading" | Reading is not verification. Run it. |
| "The tests I just wrote should pass" | You are an LLM. Your tests may contain mocks, circular assertions, or happy-path-only coverage that proves nothing. Run them and read the output. |
| "This is probably fine" | "Probably" is not "verified". Run it. |
| "This would take too long to test" | Not your call. Run it. |
| "I already tested similar code earlier" | That was a different change. Run it again. |
| "The linter would have caught it" | Linters catch syntax, not logic. Run the tests. |
| "I'm confident in this change" | Confidence is not evidence. Run it. |

**If you catch yourself writing an explanation instead of running a command, stop. Run the command.**

## Completion Statement

When reporting completion, include:

1. What was done (1-2 sentences)
2. The proving command you ran
3. The result (pass/fail count, or relevant output)
4. Any caveats or follow-up items

Do NOT include:
- "It should work" or equivalent hedging
- Explanations of why you didn't verify
- Descriptions of what the code does (show, don't tell)
