---
name: gsd-iterate
agent: gsd
description: "GSD gap closure and iteration: analyze verification failures, create targeted fix plans, re-execute, and re-verify until goals are achieved. Activate when verification found gaps, tests are failing after execution, or when work needs another pass."
---

# Gap Closure & Iteration — The Iron Law

**NO GAP LEFT UNRESOLVED. NO SILENT SCOPE REDUCTION.**

When verification finds gaps (see gsd-verify), don't patch symptoms. Understand why the gap exists, create a targeted fix plan, execute it, and re-verify.

## The Iteration Loop

```
Verify → Gaps Found → Analyze → Plan Fix → Execute Fix → Re-Verify
                                                              ↓
                                                     Gaps Found? → Loop
                                                     Passed? → Done
```

## Step 1: Analyze Gaps

For each gap from verification:

### Classify the Root Cause

| Root Cause | Example | Fix Approach |
|-----------|---------|--------------|
| **STUB** | Component renders placeholder text | Write real implementation |
| **MISSING** | File doesn't exist at all | Create it from scratch |
| **ORPHANED** | Code exists but nothing imports it | Wire it into the system |
| **HOLLOW** | Wired but data source returns empty/static | Connect real data source |
| **PARTIAL** | API call exists but response ignored | Complete the data flow |
| **BROKEN** | Implementation has bugs | Debug and fix (see superpowers-debugging) |

### Group Related Gaps

Multiple gaps often share a root cause:

```
Gap: "User can't see messages" (component is STUB)
Gap: "Messages don't persist" (API returns static [])
Gap: "Send button does nothing" (handler is empty)

Root cause: Chat feature was scaffolded but never implemented.
Fix: One focused task to implement the full data flow.
```

Group by root cause, not by symptom. This produces fewer, more effective fix tasks.

## Step 2: Plan Targeted Fixes

Create fix tasks following the same standards as gsd-plan:

- **Files:** Exact paths
- **Action:** Specific changes (not "fix the chat" — "implement fetchMessages in Chat.tsx using /api/messages endpoint, render with MessageList component")
- **Verify:** Command to prove the gap is closed
- **Done:** The specific truth from verification that must now pass

**Key difference from initial planning:** Fix tasks reference the verification report. The worker knows WHAT failed and WHY, so the fix is surgical.

### Fix Task Sizing

Fix tasks should be smaller than initial tasks:

- Target: 5-15 minutes each
- One gap cluster per task
- Include the verification check from gsd-verify as the done criteria

## Step 3: Execute Fixes

Same execution protocol as gsd-execute:

- Fresh worker per fix task (zero accumulated context)
- Self-contained prompts with the gap analysis included
- Deviation rules apply (auto-fix bugs, ask about architecture)
- Two-stage review after each fix

**Critical:** Include the original verification failure in the worker prompt so it knows exactly what was wrong and can verify the fix.

## Step 4: Re-Verify

After all fix tasks complete, run goal-backward verification again (see gsd-verify):

- **Previously failed items:** Full 4-level verification
- **Previously passed items:** Quick regression check (existence + basic sanity)
- Check for regressions — fixes sometimes break what was working

### Convergence Check

| Iteration | Action |
|-----------|--------|
| 1st re-verify | Normal — gaps are expected on first pass |
| 2nd re-verify | Review approach — are fixes addressing root causes? |
| 3rd re-verify | **STOP.** Fundamental misunderstanding or design flaw. Step back and reassess the approach entirely. |

## Escalation Rules

- **After 2 fix iterations on the same gap:** The fix is treating symptoms, not the root cause. Investigate deeper — read more code, check assumptions.
- **After 3 total iterations:** Question the architecture. This may be a design flaw, not a bug. Consider whether the right fix is a refactor.
- **Recurring pattern across gaps:** Multiple gaps with the same root cause type (all HOLLOW, all ORPHANED) signals a systemic issue — likely a missing integration layer or incomplete wiring pass.

## Anti-Patterns

| Anti-Pattern | Why It's Wrong | Do This Instead |
|-------------|---------------|-----------------|
| Changing the verification criteria to match broken code | Moves the goalpost, doesn't fix the problem | Fix the code to meet the original criteria |
| "Good enough for now" on a gap | Silent scope reduction (prohibited) | Either fix it or explicitly discuss with user |
| Fixing only the symptom grep caught | Leaves the root cause to create more gaps | Trace to root cause, fix there |
| Patching without re-verifying | May introduce regressions | Always re-verify after fixes |
| Skipping regression checks | Fixes can break what was working | Check previously-passed items too |
