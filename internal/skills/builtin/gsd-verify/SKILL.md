---
name: gsd-verify
agent: gsd
description: "GSD goal-backward verification: prove goals were achieved with 4-level artifact checks, stub detection, data-flow tracing, and anti-rationalization discipline. Activate after executing a plan, before declaring work done, or when asked to verify completeness."
---

# Goal-Backward Verification — The Iron Law

**TASK COMPLETION ≠ GOAL ACHIEVEMENT.**

A "create chat component" task is complete when the file exists. But the GOAL "working chat interface" requires the component to render real messages, connect to an API, and persist data. Verify the GOAL, not the tasks.

**DO NOT trust claims.** Summaries document what was SAID. You verify what ACTUALLY EXISTS in the code.

## The Goal-Backward Process

### Step 1: State the Goal

Take the goal from the original request. Must be outcome-shaped, not task-shaped.

- Good: "Working chat interface" (outcome)
- Bad: "Build chat components" (task)

### Step 2: Derive Observable Truths

"What must be TRUE for this goal to be achieved?" List 3-7 truths from the USER's perspective.

For "working chat interface":
- User can see existing messages
- User can type a new message
- User can send the message
- Sent message appears in the list
- Messages persist across page refresh

**Test:** Each truth verifiable by a human using the application.

### Step 3: Derive Required Artifacts

For each truth: "What must EXIST for this to be true?"

"User can see existing messages" requires:
- Message list component (renders Message[])
- API route or data source (provides messages)
- Message type definition (shapes the data)

**Test:** Each artifact = a specific file with specific content.

### Step 4: Four-Level Artifact Verification

For each artifact, check all four levels:

| Level | Check | How | Status if Failed |
|-------|-------|-----|-----------------|
| **1. Exists** | File present on disk | `ls`, `view` | MISSING |
| **2. Substantive** | Real logic, not placeholder | Check line count, look for real implementation | STUB |
| **3. Wired** | Imported and used by other code | `grep` for imports and usage | ORPHANED |
| **4. Data flows** | Real data reaches rendering | Trace data source → state → render | HOLLOW |

#### Stub Detection Patterns

These indicate task "complete" but goal NOT achieved:

```
RED FLAGS:
return <div>Placeholder</div>       // Level 2: STUB
return Response.json([])             // Level 2: STUB (empty, no DB query)
onClick={() => {}}                   // Level 2: STUB (empty handler)
fetch('/api/x')                      // Level 3: no await, no response use
const [data] = useState([])          // Level 4: HOLLOW (never populated)
return { ok: true }                  // Level 4: HOLLOW (ignores query result)
```

#### Data-Flow Trace (Level 4)

For artifacts that render dynamic data:

1. **Find the data variable** — what state/prop does the artifact render?
2. **Trace the source** — where does it get populated? (fetch, store, props)
3. **Verify the source produces real data** — DB query vs. static return
4. **Check for disconnected props** — props hardcoded empty at call site

| Data Source | Produces Real Data | Status |
|------------|-------------------|--------|
| DB query found | Yes | FLOWING |
| Fetch exists, static fallback only | No | STATIC |
| No data source found | N/A | DISCONNECTED |
| Props hardcoded empty | No | HOLLOW |

### Step 5: Verify Key Links (Wiring)

Key links = critical connections where breakage causes cascading failures.

| Pattern | How to Check | Status |
|---------|-------------|--------|
| Component → API | `grep` for fetch/axios call + response handling | WIRED / PARTIAL / NOT_WIRED |
| API → Database | `grep` for query + result returned | WIRED / PARTIAL / NOT_WIRED |
| Form → Handler | `grep` for onSubmit + API call in handler | WIRED / STUB / NOT_WIRED |
| State → Render | `grep` for state variable used in JSX/template | WIRED / NOT_WIRED |

**PARTIAL** = call exists but response ignored, or query exists but result not returned.

### Step 6: Run Behavioral Spot-Checks

For 2-4 key behaviors, run a quick command to verify:

```bash
# API returns non-empty data
curl -s localhost:PORT/api/endpoint | grep -q '"id"'

# CLI produces expected output  
./tool --help | grep -q 'expected-subcommand'

# Test suite passes
go test ./... | grep -q 'PASS'

# Module exports expected functions
node -e "const m = require('./module'); console.log(typeof m.fn)"
```

Each check must complete in < 10 seconds. Don't start servers — only test what's already runnable.

### Step 7: Determine Status

| Condition | Status |
|-----------|--------|
| All truths verified, all artifacts pass 4 levels, all links wired | **PASSED** |
| Some artifacts STUB/MISSING/HOLLOW, links broken | **GAPS FOUND** — list each gap with specific fix needed |
| Programmatic checks pass but visual/UX needs human testing | **HUMAN NEEDED** — list what to test manually |

## Anti-Rationalization Rules

These are NOT acceptable substitutes for verification:

| Excuse | Reality |
|--------|---------|
| "The code looks correct" | Reading is not verification. Run it. |
| "The tests should pass" | Tests may have circular assertions. Run them. |
| "This is probably fine" | "Probably" is not "verified". Run it. |
| "I already tested similar code" | Different change. Run it again. |
| "I'm confident in this change" | Confidence is not evidence. Run it. |

**If you catch yourself writing an explanation instead of running a command, stop. Run the command.**

## Completion Statement

When reporting verification results:

1. Status: PASSED / GAPS FOUND / HUMAN NEEDED
2. Score: N/M truths verified
3. For each gap: what's wrong, which file, what specific fix is needed
4. For human-needed items: what to test, expected behavior, why automation can't verify

If gaps found, proceed to gap closure (see gsd-iterate).
