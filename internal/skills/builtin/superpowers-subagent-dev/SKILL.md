---
name: superpowers-subagent-dev
agent: superpowers
description: "Superpowers subagent-driven development: dispatch fresh workers per task with two-stage review. Activate when executing a multi-task plan or implementing a feature with multiple independent components."
---

# Subagent-Driven Development

Fresh subagent per task + two-stage review = high quality at speed.

## Core Principle

Each implementation task gets a fresh worker agent with:
- Zero accumulated context (no stale assumptions)
- A self-contained prompt (everything it needs to succeed)
- Specific scope (one task, one deliverable)

## Execution Workflow

### 1. Prepare the Task

Before dispatching a worker, write a complete, self-contained prompt:

**Must include:**
- What to build/change (specific files, functions, behavior)
- Why (purpose and motivation — workers calibrate depth based on this)
- Constraints (patterns to follow, existing code to match, libraries to use)
- Verification command (how the worker proves the task is done)
- Relevant file paths and line numbers

**Anti-pattern (lazy delegation):**
> "Based on your findings, fix the auth bug"

The worker has no findings. It starts with zero context.

**Good delegation:**
> "Fix the null pointer in src/auth/validate.ts:42. The `user` field on Session (src/auth/types.ts:15) is undefined when sessions expire but the token remains cached. Add a nil check before accessing user.Email. Write a test in src/auth/validate_test.ts. Run `npm test -- --grep auth`."

### 2. Dispatch

- **Independent tasks on different files** → launch multiple workers in parallel
- **Dependent tasks** → sequential, one worker at a time
- **Tasks on the same file** → do it yourself (workers may conflict)

### 3. Two-Stage Review

After each worker completes:

#### Stage 1: Spec Compliance Review
- Does the code match the plan/spec? Not more, not less.
- Are all required files created/modified?
- Does the verification command pass?
- Any over-building (features not requested) or under-building (requirements missed)?

#### Stage 2: Code Quality Review
- Is the implementation well-structured?
- Are error cases handled?
- Does it follow the project's existing patterns?
- Any obvious performance issues or security concerns?
- Are tests meaningful (see superpowers-tdd quality checklist)?

### 4. Handle Issues

If review finds problems:
- **Minor issues** (naming, formatting): fix them yourself (you have the context from review)
- **Significant issues** (wrong approach, missing functionality): re-dispatch the worker with specific feedback about what to fix
- **Systemic issues** (plan was wrong): update the plan, don't patch around it

## Decision Matrix: Worker vs. Self

| Situation | Action |
|-----------|--------|
| Research explored the exact files to edit | Do it yourself (files already in context) |
| Multiple independent file changes | Spawn parallel workers |
| Correcting a worker's failure | Do it yourself (you have the error context) |
| Independent test writing | Spawn a worker |
| Same file needs multiple changes | Do it yourself (avoid conflicts) |
| Task requires deep codebase understanding | Do it yourself (context matters) |
| Mechanical/repetitive changes across files | Spawn workers |
