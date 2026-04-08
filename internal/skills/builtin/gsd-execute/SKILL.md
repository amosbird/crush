---
name: gsd-execute
agent: gsd
description: "GSD wave-based execution: dispatch fresh workers per task with deviation rules, atomic verification, and two-stage review. Activate when executing a multi-task plan, building from a spec, or implementing a feature with multiple components."
---

# Wave-Based Execution — The Iron Law

**FRESH WORKER PER TASK. ZERO ACCUMULATED GARBAGE.**

Context rot is the #1 cause of quality degradation. Each task gets a fresh worker with zero accumulated context — no stale assumptions, no degraded attention.

## Execution Protocol

### 1. Read the Plan

Before dispatching any worker:

- Parse the wave structure — which tasks are parallel, which are sequential
- Identify file ownership — no two parallel workers should touch the same file
- Check for locked decisions from discussion (see gsd-discuss) — workers must honor them
- Extract interface contracts that workers will need

### 2. Dispatch by Wave

```
Wave 1: Launch all Wave 1 workers in parallel
  ↓ wait for all to complete
  ↓ two-stage review each result
Wave 2: Launch all Wave 2 workers in parallel
  ↓ wait for all to complete
  ↓ two-stage review each result
...continue until all waves done
```

### 3. Write Self-Contained Worker Prompts

The worker has ZERO context about your conversation. Write prompts accordingly:

**Must include:**
- What to build/change (specific files, functions, behavior)
- Why (purpose and motivation — workers calibrate depth based on this)
- Constraints (patterns to follow, existing code to match, libraries to use)
- Interface contracts (types, exports the worker needs — don't make them search)
- Verification command (how the worker proves the task is done)
- Relevant file paths and line numbers

**Anti-pattern (lazy delegation):**
> "Based on your findings, fix the auth bug"

The worker has no findings. It starts fresh.

**Good delegation:**
> "Fix the null pointer in src/auth/validate.ts:42. The `user` field on Session (src/auth/types.ts:15) is undefined when sessions expire but the token remains cached. Add a nil check before accessing user.Email. Write a test in src/auth/validate_test.ts. Run `go test ./internal/auth/`."

### 4. Worker Dispatch Rules

| Situation | Action |
|-----------|--------|
| Independent tasks on different files | Launch parallel workers |
| Dependent tasks (needs output of prior) | Sequential, one at a time |
| Same file needs changes | Do it yourself (workers conflict) |
| Correcting a worker's failure | Do it yourself (you have error context) |
| Files already in your context | Do it yourself (faster) |
| Mechanical/repetitive changes | Spawn workers |

### 5. Two-Stage Review

After each worker completes:

#### Stage 1: Spec Compliance
- Does the code match the plan? Not more, not less.
- Are all required files created/modified?
- Does the verification command pass?
- Any over-building (features not requested) or under-building (requirements missed)?
- Are locked decisions honored exactly? (see gsd-discuss)

#### Stage 2: Code Quality
- Well-structured? Error cases handled?
- Follows existing project patterns?
- No security concerns? (injection, auth bypass, secrets in logs)
- Tests meaningful? (not circular, not happy-path-only)

### 6. Handle Issues

- **Minor issues** (naming, formatting): fix yourself (you have review context)
- **Significant issues** (wrong approach, missing functionality): re-dispatch with specific feedback
- **Systemic issues** (plan was wrong): update the plan, don't patch around it

## Deviation Rules

Workers WILL discover work not in the plan. Apply these rules:

| Rule | Trigger | Action |
|------|---------|--------|
| **Auto-fix bugs** | Code doesn't work (errors, wrong output) | Fix inline, no permission needed |
| **Auto-add critical** | Missing error handling, validation, auth, security | Fix inline, no permission needed |
| **Auto-fix blockers** | Missing dependency, broken import, wrong types | Fix inline, no permission needed |
| **Ask about architecture** | New DB table, major schema change, switching libs | STOP — ask user |

**Scope boundary:** Only auto-fix issues directly caused by the current task. Pre-existing warnings are out of scope.

**Fix attempt limit:** After 3 auto-fix attempts on a single issue, stop. Document the issue and move on.

## Analysis Paralysis Guard

If you make 5+ consecutive read/search calls without any edit/write action:

**STOP.** State in one sentence why you haven't written anything yet. Then either:
1. Write code (you have enough context), or
2. Report "blocked" with the specific missing information

Analysis without action is a stuck signal.

## Verification Per Task

After each task:

1. Run the verification command specified in the plan
2. Read the FULL output — don't assume success from partial output
3. If it fails, fix immediately (deviation rules apply)
4. Check for unwired code — new functions nothing calls, new config nothing reads

**Never acceptable:** "The code looks correct" or "This should work." Run the command.

## Progress Tracking

Use the todos tool to track wave execution:

```
- [completed] Wave 1, Task 1: User model
- [completed] Wave 1, Task 2: Product model
- [in_progress] Wave 2, Task 3: User API
- [pending] Wave 2, Task 4: Product API
- [pending] Wave 3, Task 5: Dashboard
```

After all waves complete, proceed to goal-backward verification (see gsd-verify).
