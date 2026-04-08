---
name: superpowers-planning
agent: superpowers
description: "Superpowers planning workflow: write detailed implementation plans with bite-sized tasks. Activate when breaking down a complex feature, multi-file change, or when the user asks for a plan."
---

# Writing Plans — The Iron Law

**NO PLANS WITH PLACEHOLDERS OR MISSING DETAILS.**

Write implementation plans assuming the executor has zero context and questionable taste. Every plan must be specific enough that a fresh agent (or a future you with no memory of this conversation) can execute it without guessing.

## Plan Structure

### 1. Map the File Structure First

- List every file to create, modify, or delete
- For modifications: specify which functions/sections change
- Define clear boundaries between components
- Show the dependency order: what must be built first?

### 2. Break into Bite-Sized Tasks

Each task should take 2-5 minutes of focused work. A task that takes longer should be split.

Every task must include:
- **Exact file paths** (not "the config file" — `src/config/settings.ts:42-58`)
- **Complete code** for new files or the specific changes for existing files
- **Verification command** to confirm the task is done

### 3. Task Format

```
### Task N: [Component Name]

**Files:**
- Create: `exact/path/to/file.py`
- Modify: `exact/path/to/existing.py` (function `handleRequest`)
- Test: `tests/exact/path/to/test_file.py`

Steps:
- [ ] Write failing test (with actual test code)
- [ ] Run test to verify it fails: `pytest tests/path -k test_name`
- [ ] Write minimal implementation
- [ ] Run test to verify it passes: `pytest tests/path -k test_name`
- [ ] Run full suite: `pytest tests/`
```

## The No-Placeholders Rule

These are NEVER acceptable in a plan:

- "TBD", "TODO", "implement later", "fill in details"
- "Add appropriate error handling"
- "Similar to Task N" (spell it out)
- "Standard boilerplate" (show the actual boilerplate)
- "Use the usual pattern" (write the pattern)
- Pseudo-code that handwaves the hard parts

Every code block must be complete and directly executable. If you can't write the code yet, the plan isn't ready.

## Ordering Principles

1. **Infrastructure first** — data models, config, types
2. **Core logic second** — business rules, algorithms
3. **Integration third** — wiring, routes, API endpoints
4. **Cleanup last** — refactoring, documentation

Within each layer, order by dependency: build what others depend on first.

## Worker Delegation

When a plan has independent tasks, note which can run in parallel:

```
Tasks 1-3: Sequential (each depends on the previous)
Tasks 4-5: Parallel (independent modules, different files)
Task 6: Sequential (integration, depends on 4-5)
```

This enables efficient worker delegation during execution.
