---
name: gsd-plan
agent: gsd
description: "GSD spec-driven planning: break work into wave-parallel tasks with context budgets, dependency graphs, and interface-first ordering. Activate when planning a multi-step feature, decomposing work, or when the user asks to plan before building."
---

# Spec-Driven Planning — The Iron Law

**NO PLANS WITH PLACEHOLDERS OR MISSING DETAILS.**

Plans are prompts, not documents. A PLAN is the worker prompt — specific enough that a fresh agent with zero context executes without guessing.

## Context Engineering

Quality degrades as context fills. Plan accordingly:

| Context Usage | Quality | State |
|---------------|---------|-------|
| 0-30% | PEAK | Thorough, comprehensive |
| 30-50% | GOOD | Solid work |
| 50-70% | DEGRADING | Shortcuts begin |
| 70%+ | POOR | Rushed, bugs |

**Rule:** Each worker task should complete within ~50% context. More workers, smaller scope, consistent quality.

## The Planning Process

### 1. Map the Scope

Before writing any task:

- List every file to create, modify, or delete
- For modifications: specify which functions/sections change
- Define clear boundaries between components
- Show the dependency order: what must be built first?
- Check git history and existing patterns for context

### 2. Build the Dependency Graph

For each task, determine:

- **Needs:** What must exist before this runs (files, types, APIs)
- **Creates:** What this produces (files, types, APIs others need)
- **Conflicts:** Does this touch the same file as another task?

### 3. Assign Waves

```
Wave 1: Tasks with no dependencies (run in parallel)
Wave 2: Tasks depending only on Wave 1
Wave 3: Tasks depending on Wave 2
...
```

**Same-wave tasks MUST have zero file overlap.** If two tasks touch the same file, the later one goes to a later wave.

### 4. Prefer Vertical Slices

```
GOOD (parallel):                      BAD (sequential):
Task 1: User feature (model+API+UI)   Task 1: All models
Task 2: Product feature (model+API+UI) Task 2: All APIs  
Task 3: Order feature (model+API+UI)  Task 3: All UIs
→ All three run in Wave 1              → Fully sequential
```

Vertical slices when features are independent. Horizontal layers only when a shared foundation is required (auth before protected features, types before implementations).

## Task Anatomy

Every task needs four fields:

**Files:** Exact paths — not "the config file" but `src/config/settings.ts`

**Action:** Specific instructions including what to avoid and WHY.
- Good: "Create POST endpoint with {email, password}, validate with bcrypt, return JWT in httpOnly cookie. Use jose (not jsonwebtoken — CommonJS issues with Edge runtime)."
- Bad: "Add authentication"

**Verify:** Command that proves completion in < 60 seconds.
- Good: `go test ./internal/auth/ -run TestLogin`
- Bad: "It works"

**Done:** Measurable acceptance criteria.
- Good: "Valid credentials return 200 + JWT cookie, invalid return 401"
- Bad: "Authentication is complete"

**Specificity test:** Could a different agent execute this without asking clarifying questions? If not, add detail.

## Task Sizing

| Duration | Action |
|----------|--------|
| < 15 min | Too small — combine with related task |
| 15-60 min | Right size |
| > 60 min | Too large — split |

**Split signals:** >3 tasks per plan, >5 files per task, multiple subsystems, checkpoint + implementation together.

## Scope Estimation

| Complexity | Tasks/Plan | Context Target |
|------------|------------|----------------|
| Simple (CRUD, config) | 2-3 | ~30-45% |
| Complex (auth, payments) | 1-2 | ~40-50% |
| Very complex (migrations) | 1 | ~30-40% |

## Interface-First Ordering

When tasks create interfaces consumed by later tasks:

1. **First:** Define contracts — type files, interfaces, exports
2. **Middle:** Implement against the contracts
3. **Last:** Wire — connect implementations to consumers

This prevents workers from exploring the codebase to understand contracts. They receive the contracts in the prompt.

## The No-Placeholders Rule

NEVER acceptable in a plan:

- "TBD", "TODO", "implement later", "fill in details"
- "Add appropriate error handling" (specify what errors, what handling)
- "Similar to Task N" (spell it out)
- "Standard boilerplate" (show the actual code)
- "Use the usual pattern" (write the pattern)

If you can't write the specific instructions yet, the plan isn't ready.

## Ordering Principles

1. **Infrastructure first** — data models, config, types
2. **Core logic second** — business rules, algorithms
3. **Integration third** — wiring, routes, API endpoints
4. **Cleanup last** — refactoring, documentation

Within each layer, order by dependency: build what others depend on first.

## Output Format

Present the plan with wave structure visible:

```
## Plan: [Feature Name]

### Wave 1 (parallel)
- Task 1: [name] — files: [...] 
- Task 2: [name] — files: [...]

### Wave 2 (parallel, depends on Wave 1)
- Task 3: [name] — files: [...]
- Task 4: [name] — files: [...]

### Wave 3 (depends on Wave 2)
- Task 5: [name] — files: [...]
```

This enables efficient worker delegation during execution (see gsd-execute).
