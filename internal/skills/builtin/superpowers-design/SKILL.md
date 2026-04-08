---
name: superpowers-design
agent: superpowers
description: "Superpowers design-first workflow: explore, design, confirm before coding. Activate for non-trivial features, architectural changes, or when the user asks you to design/plan something."
---

# Design First — The Iron Law

**NO IMPLEMENTATION WITHOUT DESIGN FIRST** for any non-trivial task.

"This is too simple to need a design" is an anti-pattern. If a task touches more than one file or introduces a new concept, it needs design.

## The Design Process

### 1. Explore

Before forming any opinion, understand the problem space:

- Search the codebase for related files, patterns, and dependencies
- Read existing code that does something similar
- Check `git log` and `git blame` for historical context and past decisions
- Identify constraints: performance requirements, API contracts, backward compatibility
- Map the dependency graph: what depends on what you're changing?

### 2. Design

Form 2-3 possible solutions. For each:

- **Approach**: One-sentence summary of the strategy
- **Pros**: What it gets right (simplicity, performance, extensibility)
- **Cons**: What it costs (complexity, breaking changes, testing burden)
- **Files affected**: Concrete list of files to create/modify/delete
- **Risk**: What could go wrong

Present as a structured comparison (table or bullet list). Be honest about tradeoffs — don't advocate for one solution unless it's clearly superior.

### 3. Confirm

For significant architectural decisions:

- Present the design to the user with the structured comparison
- Wait for approval before implementing
- If the user asks questions, answer them. Don't proceed until you have a clear go-ahead
- If the user picks an approach, acknowledge and proceed

For moderate decisions (clear best option, low risk):

- State your recommendation and rationale in 1-2 sentences
- Proceed unless the user objects

### 4. Plan

Break the approved design into concrete implementation steps:

- Each step should be independently testable
- Order steps to minimize risk (infrastructure first, features second, cleanup last)
- Include verification commands for each step
- Identify which steps can be parallelized (for worker delegation)

### 5. Execute

Implement step by step:

- Follow TDD for each step (see superpowers-tdd)
- Run tests after each change
- If the design doesn't work in practice, STOP and revisit rather than hacking around it

## When to Skip

Skip directly to implementation for:

- Single-file changes with obvious solutions
- Bug fixes with clear root cause
- Typo, config, or documentation changes
- Changes that follow an established pattern exactly

Use judgment — the goal is quality, not ceremony. When in doubt, design.
