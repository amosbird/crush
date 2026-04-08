---
name: superpowers-code-review
agent: superpowers
description: "Superpowers code review workflow: pre-submission self-review checklist and structured review process. Activate when reviewing code, preparing to submit changes, or when the user asks for a code review."
---

# Code Review — The Iron Law

**NO CODE SUBMITTED WITHOUT SELF-REVIEW.**

## Pre-Submission Self-Review

Before presenting any implementation as complete, run through this checklist:

### Correctness
- [ ] Does the code do what was requested? Re-read the original requirement.
- [ ] Are all edge cases handled? (nil, empty, boundary, concurrent)
- [ ] Are error paths tested, not just happy paths?
- [ ] Do all tests pass with a fresh run?

### Quality
- [ ] Does the code follow existing project patterns? (check similar files)
- [ ] No unnecessary abstractions or premature generalizations?
- [ ] No dead code, unused imports, or commented-out blocks?
- [ ] Variable and function names are clear and consistent with the codebase?

### Security
- [ ] No secrets in code or logs?
- [ ] Input validation on all external data?
- [ ] No injection vulnerabilities (SQL, command, XSS)?
- [ ] Auth/authz checks where needed?

### Scope
- [ ] Changes are limited to what was requested?
- [ ] No "while I'm here" improvements?
- [ ] No formatting changes to lines you didn't modify?

## Performing a Code Review

When asked to review code (PR, diff, or files):

### 1. Understand Context
- What problem does this change solve?
- Read the PR description, linked issues, or user explanation
- Understand the scope of the change before reading code

### 2. Review Systematically
- **Architecture**: Does the approach make sense? Is there a simpler way?
- **Correctness**: Will it work? Edge cases? Error handling?
- **Testing**: Are tests meaningful? Do they cover the new behavior?
- **Security**: Any new attack surfaces?
- **Performance**: Any obvious N+1 queries, unnecessary allocations, or blocking calls?

### 3. Give Actionable Feedback
- Be specific: "Line 42: this nil check should also handle empty string" not "needs better validation"
- Distinguish blocking issues from suggestions
- If something is good, say so — positive feedback has value
- Use `file:line` references

### Categories:
- **BLOCKING** — Must fix before merge. Bugs, security issues, missing tests.
- **SUGGESTION** — Would improve the code. Author decides.
- **QUESTION** — Need clarification to complete review.
- **PRAISE** — Something done well worth noting.
