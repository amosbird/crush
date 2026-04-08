---
name: gsd-discuss
agent: gsd
description: "GSD requirements discovery: ask until clear, capture decisions as locked/deferred/discretion, extract scope boundaries. Activate when starting a new feature, project, or when the user describes something to build."
---

# Requirements Discovery — The Iron Law

**NO PLANNING WITHOUT UNDERSTANDING FIRST.**

"I think I understand what you want" is not understanding. Ask until there are no gray areas left.

## The Discovery Process

### 1. Analyze the Request

Before asking anything, analyze what the user described:

- What's explicitly stated vs. what's assumed?
- What are the gray areas — places where two reasonable people would build different things?
- What decisions have implicit tradeoffs the user may not have considered?

### 2. Identify Gray Areas by Domain

Different types of work have different gray areas:

| Domain | Gray Areas to Probe |
|--------|-------------------|
| **UI/Frontend** | Layout, density, interactions, empty states, responsive behavior, animations |
| **APIs/Backend** | Response format, error codes, pagination, rate limiting, auth model |
| **Data/Models** | Schema design, relationships, constraints, migration strategy |
| **CLI tools** | Flags, output format, verbosity levels, exit codes |
| **Infrastructure** | Deployment target, scaling, monitoring, secrets management |

### 3. Ask Focused Questions

Ask about gray areas one domain at a time. For each:

- State what you'd do by default (so the user can just say "yes" or correct you)
- Explain WHY this matters (so the user makes informed decisions)
- Offer concrete options when possible (not open-ended)

**Good:** "For the dashboard layout — card grid (3 cols desktop, 1 mobile) or data table? Cards are better for visual scanning, tables for dense data."

**Bad:** "How should the dashboard look?"

### 4. Capture Decisions

Every discussion produces three categories:

#### Locked Decisions
User explicitly chose something. These are **NON-NEGOTIABLE** during planning and execution.

```
D-01: Use card layout for dashboard (not table)
D-02: JWT auth with httpOnly cookies (not localStorage)
D-03: PostgreSQL (not SQLite)
```

#### Deferred Ideas
User explicitly said "not now" or "later." These **MUST NOT** appear in any plan.

```
Deferred: Search functionality (v2)
Deferred: Dark mode (post-launch)
```

#### Discretion Areas
User said "you decide" or didn't have a strong opinion. Use good judgment, document choices.

```
Discretion: Error toast library (choose based on existing deps)
Discretion: Pagination style (offset vs cursor — pick what fits the data)
```

### 5. Validate Back

Before moving to planning, summarize:

- **Building:** [what you'll build]
- **Not building:** [what's explicitly excluded]
- **Key decisions:** [the locked ones]
- **Assumptions:** [your discretion calls]

Wait for confirmation. Don't proceed until you get it.

## Scope Reduction Prohibition

**NEVER silently simplify user decisions.** These patterns are PROHIBITED in later phases:

- "v1", "simplified version", "static for now", "hardcoded for now"
- "future enhancement", "placeholder", "basic version"
- "will be wired later", "skip for now"

If a decision says "display cost calculated from billing table," the implementation MUST deliver exactly that — not a static label as a "v1."

If the scope is too large, **split into phases** where each phase delivers full fidelity for a subset. Never deliver a watered-down version.

## When to Skip

Skip full discovery for:

- Bug fixes with clear reproduction steps
- Single-file changes with obvious scope
- Tasks where the user provided a complete spec
- Follow-up work where context was already established

Use judgment — the goal is clarity, not ceremony.
