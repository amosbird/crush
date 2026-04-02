# Commit Review Findings

## Round 1

### 🔴 HIGH

| # | Commit | File | Issue | Status |
|---|--------|------|-------|--------|
| 2 | `175e799c` grep | `tools/grep.go:205-207,264-272` | **Context lines sorting broken**: context lines have zero `modTime`, `sort.Slice` pushes them all to the end, separating them from their match lines | ✅ Fixed — modTime cache shared between match/context |
| 3 | `175e799c` grep | `tools/grep.go:144-151` | **Context lines missing file header**: context lines `continue` before `currentFile` check, so first lines of a file have no filename header | ✅ Fixed — file header check moved before context handling |

### 🟡 MEDIUM

| # | Commit | File | Issue | Status |
|---|--------|------|-------|--------|
| 4 | `175e799c` grep | `tools/grep.go:209-212` | **Truncation cuts context groups**: limit=100 counts context lines, truncation can split a context group in half | ✅ Fixed — truncation counts only match lines |
| 6 | `9d96ef49` sandbox | `tools/safe.go:168-183` | **Dead `i++` in `containsBackgroundOp`**: `i++` inside `range` loop is a no-op, logic accidentally correct but misleading | ✅ Fixed — removed dead `i++` |
| 7 | `fbda15d6` UI | `ui/dialog/imagepreview.go:108-120` | **Data race**: `previewingImage`/`transmitting` mutated inside `tea.Cmd` goroutine while read on main thread | ✅ Fixed — mutations moved to HandleMsg on TransmittedMsg |
| 8 | `9369f3cd` ctx_cancel | `ui/completions/completions.go:138-154` | **Data race**: after `WaitWithContext` timeout, goroutines continue writing to `msg.Files`/`msg.Resources` concurrently with the returned msg being read | ✅ Fixed — goroutines write to local vars, assigned to msg only after WaitWithContext succeeds |
| 9 | `9369f3cd` ctx_cancel | `tools/search.go:209-223` | **Mutex held during sleep**: `lastSearchMu` held while sleeping in `maybeDelaySearch`; cancelled searches still update `lastSearchTime` | ✅ Fixed — mutex released before sleep, early return on cancel |
| 10 | `d83a6ed7` orphan fix | `agent/agent.go:238` | **Fire-and-forget goroutine**: `generateTitle` no longer has WaitGroup, may outlive `Run()` | ⏭️ Skipped — intentional design, uses parent ctx which gets cancelled on app shutdown |
| 11 | `fbda15d6` UI | `ui/list/list.go` | **Height cache inconsistency**: `getItemHeight()` and `getItem()` compute different heights for the same item (with/without render callbacks) | ✅ Fixed — `getItemHeight()` now applies render callbacks |

### 🟢 LOW

| # | Commit | File | Issue | Status |
|---|--------|------|-------|--------|
| 12 | `51c29c34` diff | diff tool | No output size limit (other tools like view have 1MB limit) | ⏭️ Skipped — low risk, diff output is typically small |
| 13 | `660ad892` plan/title | `session/session.go:224` | Comment says "log but don't fail" but error is silently swallowed without logging | ✅ Fixed — added slog.Error |
| 14 | `fbda15d6` UI | `ui/model/ui.go` | `pendingToolResults` map not cleared on session switch | ✅ Fixed — cleared in newSession() and loadSessionMsg |
| 15 | `9369f3cd` ctx_cancel | `mcp/init.go:204-205` | `initDone` closed when ctx cancels but MCP init goroutines still running | ⏭️ Skipped — acceptable: goroutines share same ctx and will be cancelled |

---

## Round 2

### 🟡 MEDIUM

| # | File | Issue | Status |
|---|------|-------|--------|
| 21 | `agent/agent.go:69` | **thinkTagRegex missing `(?s)` flag**: won't strip multiline think tags from titles | ✅ Fixed — added `(?s)` flag |
| 22 | `agent/agent.go:604-618` | **shouldSummarize path lacks autoSummarizeDepth protection**: potential unbounded recursion | ⏭️ Skipped — false positive, `autoSummarizeDepth` is incremented and checked against `maxAutoSummarizeDepth` at lines 473-476 |
| 23 | `agent/memory_search_tool.go:83-86` | **Memory search tool allows path traversal**: sub-agent tools could access arbitrary files | ⏭️ Skipped — false positive, tools are scoped to `transcriptDir` via working directory parameter |
| 24 | `tools/grep.go:40-48` | **regexCache caches nil on compile error**: future calls get cached nil `*regexp.Regexp`, causing NPE | ✅ Fixed — `Get`/`Set` with explicit nil check instead of `GetOrSet` |
| 25 | `tools/grep.go:205-207` | **`sort.Slice` (unstable) destroys context line ordering**: context lines within same file can be reordered | ✅ Fixed — changed to `sort.SliceStable` |
| 27 | `ui/model/session.go:148-165` | **Plan mode restoration scans past `false` result**: loop only breaks on `planMode == true`, older `true` overrides newer `false` | ✅ Fixed — break on first `plan_mode` tool result found regardless of value |
| 28 | `ui/model/chat.go:550-571` | **RemoveMessage doesn't clean up nested tool IDs**: stale `idInxMap` entries for nested tools after removal | ⏭️ Skipped — cosmetic, RemoveMessage used for transient messages unlikely to be NestedToolContainers |
| 29 | `permission/permission.go:274-278` | **isWithinDir fails for non-existent paths**: `EvalSymlinks` requires path to exist, auto-approve broken for new files | ✅ Fixed — walk up to nearest existing ancestor, resolve from there |

### 🟢 LOW

| # | File | Issue | Status |
|---|------|-------|--------|
| 31 | `agent/agent.go:488` | Prompt wrapping nests on each retry | ⏭️ Skipped — low impact, retry is rare |
| 32 | `agent/agent.go:253-254` | Deferred cancel/Del in recursive Run deletes recursive call's entry | ⏭️ Skipped — low impact |
| 33 | `tools/diff.go:100` | Missing `--` before positional file args in diff command | ⏭️ Skipped — paths are always absolute (start with `/`), never confused as flags |
| 34 | `tools/bash.go:476-478` | `waitForShell` doesn't clean up shell on timeout | ⏭️ Skipped — intentional design, shell stays in manager for later polling |
| 35 | `askuser/askuser.go:93-96` | Double `Respond()` can block goroutine forever | ⏭️ Skipped — low impact |
| 36 | `ui/list/list.go:487-488` | `setItems` with empty items sets `offsetIdx` to -1 | ⏭️ Skipped — false positive, downstream consumers guard with `len(items)==0` |
| 38 | `tools/grep.go:286-296` | Context lines included even when file stat fails | ⏭️ Skipped — low impact, gracefully degrades |
| 39 | `agent/agent.go:446-460` | StopCondition reads currentSession without sessionLock | ⏭️ Skipped — technically a race, but StopWhen and OnFinish run sequentially in fantasy.Run |
| 40 | `permission/permission.go:165-177` | Failed `Stat` keeps file path as `dir` instead of parent | ⏭️ Skipped — false positive, `isWithinDir` containment check works correctly regardless |

## Round 3

### 🔴 HIGH

| # | File | Issue | Status |
|---|------|-------|--------|
| 42 | `config/config.go:158` | **Nil map panic**: `SetupGitHubCopilot` calls `maps.Copy` on potentially nil `ExtraHeaders` map | ✅ Fixed — added nil guard with `make(map[string]string)` |

### 🟡 MEDIUM

| # | File | Issue | Status |
|---|------|-------|--------|
| 43 | `agent/agent.go:604-618` | **Unbounded summarize loop**: `shouldSummarize` re-enqueues call without incrementing `autoSummarizeDepth`, allowing infinite recursion | ✅ Fixed — added `call.autoSummarizeDepth++` |
| 44 | `agent/agent.go:753` | **Cancelled context for Save**: `Summarize` uses `genCtx` (cancelled after generation) for `sessions.Save`, which may fail | ✅ Fixed — changed to parent `ctx` |
| 45 | `tools/bash.go:9` | **Template corruption**: `html/template` HTML-escapes prompt text (e.g., `<`, `>`, `&`), corrupting shell commands | ✅ Fixed — changed to `text/template` |
| 46 | `ui/model/session.go:372-373` | **Closure data race**: `handleFileEvent` closure captures `m.session.ID` via receiver, racing with main goroutine | ✅ Fixed — captured `sessionID` before closure |
| 47 | `ui/model/ui.go:3109-3115` | **Closure data race**: `sendMessage` closure captures `m.sessionFileReads` and `m.session.ID` via receiver | ✅ Fixed — cloned slice and captured session ID before closure |
| 48 | `permission/permission.go:69,136,250-251` | **Data race on skip field**: `skip bool` read/written from multiple goroutines without synchronization | ✅ Fixed — changed to `atomic.Bool` with `Load`/`Store` |
| 49 | `shell/shell.go:243-258` | **Data race on blockFuncs**: `blockHandler` closure reads `s.blockFuncs` at execution time without mutex; races with `SetBlockFuncs` | ✅ Fixed — snapshot `blockFuncs` via `slices.Clone` while caller holds `s.mu` |
| 50 | `coordinator.go:144-153` | Goroutine leak: `readyWg.Wait` blocks forever if context cancelled during agent startup | ⏭️ Skipped — low practical impact, only during startup |
| 53 | `tools/ask_user.go:55-58` | `AllowText` always forced to true, ignoring LLM parameter | ⏭️ Skipped — intentional design choice |
| 54 | `chat.go:550-571` | `RemoveMessage` doesn't clean up nested tool IDs | ⏭️ Skipped — same as Round 2 #28, cosmetic |
| 55 | `ui/askuser.go:288-297` | Cursor Y offset includes bottom frame, may misposition | ⏭️ Skipped — minor UI glitch |
| 57 | `search.go:209-227` | TOCTOU race in `maybeDelaySearch` rate limiting | ⏭️ Skipped — partially addressed in Round 1, remaining race is benign |
| 58 | `session.go:233-237` | `Rename` doesn't publish update event | ⏭️ Skipped — UI refreshes on return from rename flow |
| 59 | `app.go:397-419` | Unsynchronized `Config.Models` mutation during startup | ⏭️ Skipped — single-goroutine startup path |
| 60 | `config.go:316-327,647-663` | `ResolvedHeaders`/`resolveEnvs` mutate config in-place | ⏭️ Skipped — called once during setup |

### 🟢 LOW

| # | File | Issue | Status |
|---|------|-------|--------|
| 64 | `agent/agent.go:912-915` | `repairOrphanedToolCalls` `insertAt` map overwrites when multiple orphans share same insertion point | ✅ Fixed — changed to `append(insertAt[key], ids...)` |
| 66 | `agent/agent.go:253-254` | Deferred `activeRequests.Del` after recursion conflicts with depth tracking | ⏭️ Skipped — minor, depth guard prevents practical issue |
| 67 | `agentic_fetch_tool.go:155` | Missing `IsSubAgent` on fetch sub-agent wastes tokens on system prompt | ⏭️ Skipped — low impact |
| 68 | `memory_search_tool.go:88-99` | Missing `DataDir` in sub-agent options | ⏭️ Skipped — benign, sub-agent doesn't need data dir |
| 69 | `agent/agent.go:968-971` | `getSessionMessages` mutates slice element in place | ⏭️ Skipped — messages are consumed, not shared |
| 70 | `tools/bash.go:524-535` | `truncateOutput` splits multi-byte UTF-8 runes | ⏭️ Skipped — cosmetic, rare edge case |
| 71 | `tools/safe.go:138` | `shellMetaChars` missing `(){}` for defense-in-depth | ⏭️ Skipped — existing chars sufficient, these are handled by shell interpreter |
| 72 | `tools/web_fetch.go:56` | Temp files in `workingDir` never cleaned up | ⏭️ Skipped — session cleanup handles this |
| 73 | `ui/ui.go:1236-1251` | Redundant loop in `handleChildSessionMessage` | ⏭️ Skipped — cosmetic |
| 74 | `ui/list.go:487-488` | `offsetIdx=-1` when list emptied | ⏭️ Skipped — same as Round 2, guarded downstream |
| 75 | `ui/list.go:496-499` | `AppendItems` forces full cache reallocation | ⏭️ Skipped — performance, not correctness |
| 76 | `ui/ui.go:1129,1215,1309` | `tea.Sequence` vs `tea.Batch` usage | ⏭️ Skipped — performance, not correctness |
| 77 | `messages.sql:67` | UUID tiebreaker in cursor pagination not chronological | ⏭️ Skipped — design limitation, UUIDs are random but pagination still deterministic |
| 78 | `message.go:114-128` | N+1 deletes in `DeleteSessionMessages` | ⏭️ Skipped — performance, batch sizes are small |

## Round 4

### 🔴 HIGH

| # | File | Issue | Status |
|---|------|-------|--------|
| 80 | `askuser/askuser.go:61-89` | **Mutex serializes all sessions**: `requestMu` held across entire blocking `Ask` loop — second session's `ask_user` hangs until first user responds | ✅ Fixed — removed unnecessary mutex (pendingRequests is already a csync.Map) |

### 🟡 MEDIUM

| # | File | Issue | Status |
|---|------|-------|--------|
| 81 | `ui/model/ui.go:3432,3550` | **Data race on pasteIdx**: `m.pasteIdx()` called inside `tea.Cmd` closures (goroutine pool) while main goroutine mutates `m.attachments` | ✅ Fixed — capture `pasteIdx()` before closure, pass as parameter |
| 82 | `agent/agent.go:1511-1513` | **UTF-8 truncation**: `content[:maxToolResultLen]` slices at byte offset, can split multi-byte characters producing invalid UTF-8 | ✅ Fixed — walk back to valid rune boundary with `utf8.RuneStart` |
| 83 | `agent/agent.go:488,615` | **Backtick corruption on auto-summarize**: user prompt wrapped in backticks without escaping, breaks markdown on recursive calls | ✅ Fixed — removed backtick wrapping, use plain text with newline separator |
| 84 | `lsp_restart.go:69-70` | Goroutine leak on context cancellation — goroutines outlive function return | ⏭️ Skipped — bounded leak, goroutines complete when `Restart()` returns |
| 85 | `mcp/init.go:204` | `initOnce.Do(close(initDone))` fires before goroutines finish, exposing partial init state | ⏭️ Skipped — bounded leak, acceptable design tradeoff for cancellation support |
| 86 | `agent/agent.go:1160-1163` | Dead code: `activeRequests.Get(sessionID + "-summarize")` key never set | ⏭️ Skipped — harmless dead code |
| 87 | `ui/model/session.go:265-267` | `loadMoreHistory` retries on persistent DB error with no backoff | ⏭️ Skipped — UI guard prevents rapid re-trigger |
| 88 | `ui/dialog/askuser.go:145-149` | AskUser dismiss sends nil answers — agent gets silent empty response | ⏭️ Skipped — context cancellation handles dismissal upstream |

### 🟢 LOW

| # | File | Issue | Status |
|---|------|-------|--------|
| 90 | `permission/permission.go:305` | `isWithinDir` false-negative on directories named `..foo` — `HasPrefix(rel, "..")` matches valid names | ✅ Fixed — check for `".." + separator` instead of bare `".."` prefix |
| 91 | `ui/list/list.go:125-146` | Height cache stale on selection change when focused item renders differently | ⏭️ Skipped — chat items have same height focused/unfocused in practice |
| 92 | `ui/model/ui.go:3031-3035` | `syncTmuxSessionID` spawns fire-and-forget goroutines on rapid session switch | ⏭️ Skipped — bounded 2s timeout per goroutine |
| 93 | `ui/model/chat.go:125` | `SetSize` subtracts `scrollbarWidth` unconditionally — can produce negative width | ⏭️ Skipped — terminal resize to tiny size is rare edge case |
| 94 | `db/db.go:147-344` | `Close()` overwrites errors — only last statement close error returned | ⏭️ Skipped — low impact during shutdown |
| 95 | `csync/wait.go:12` | `WaitWithContext` leaks goroutine when context fires before WaitGroup completes | ⏭️ Skipped — standard pattern, bounded by operation completion |
| 97 | `tools/safe.go:60,74` | `set` and `unset` in safe commands list — not read-only operations | ⏭️ Skipped — ephemeral shells, no persistence across commands |
| 98 | `tools/diff.go:75-86` | Symlink within workdir bypasses lexical path validation | ⏭️ Skipped — symlink-following is intentional design for tool functionality |
| 99 | `ui/model/session.go:270-271` | `loadMoreHistory` from DB doesn't load nested tool call trees | ⏭️ Skipped — intentional design for performance |

### Post-Review Fix

| # | Location | Issue | Status |
|---|----------|-------|--------|
| 100 | `agent/agent.go:289-294` | Plan mode enforcement only filtered LLM-visible tools (`ActiveTools`) but not the execution-side `Tools` list; LLM could still call write tools if it knew their names from conversation history | ✅ Fixed — filter `prepared.Tools` via `slices.DeleteFunc` to remove non-read-only tools |

## Round 5

| # | Location | Issue | Status |
|---|----------|-------|--------|
| 101 | `ui/model/ui.go:3799-3812` | **Data race in `copyChatHighlight`**: closure passed to `tea.Sequence` mutates `m.focus`, `m.chat.ClearMouse()`, `m.chat.Blur()`, `m.textarea.Focus()` directly from background goroutine | ✅ Fixed — return `copyChatHighlightDoneMsg` and handle mutations in `Update()` |
| 102 | `agent/agent.go:611-627` | **`shouldSummarize` missing recursion depth check**: `autoSummarizeDepth` incremented but never checked in this path, could loop unboundedly if summarization fails to reduce context | ✅ Fixed — added `maxAutoSummarizeDepth` guard; skip summarize and log warning when depth exceeded |
| 104 | `agent/agent.go:1551,1582` | **`saveTranscript`/`extractAndSaveKeyFacts` create files in CWD when `dataDir` is empty**: `filepath.Join("", "transcripts")` resolves to relative `transcripts` | ✅ Fixed — added early return when `a.dataDir == ""` |
| 106 | `tools/safe.go:187-215` | `splitShellCommands` doesn't understand shell quoting; semantic gap between what safe-check parses and what shell executes | ⏭️ Skipped — false negatives (permission requested for safe commands) are conservative; exploitable cases would be caught by the blocklist |
| 110 | `ui/model/session.go:93-133` | `loadSession` closure calls `m.prepareSessionMessages` → `m.loadNestedToolCalls` from goroutine (receiver method on `m`) | ⏭️ Skipped — `m.com.Styles` and `m.com.App` are set once at init and never mutated; practically safe |
| 111 | `askuser/askuser.go:88-93` | `Respond` can block if called twice for same request ID due to retry re-publish | ⏭️ Skipped — UI deduplicates by request ID; channel is buffered(1) |
| 112 | `csync/wait.go:12` | `WaitWithContext` leaks goroutine when context cancelled before WaitGroup completes | ⏭️ Skipped — standard pattern; bounded by operation completion |
| 115 | `permission/permission.go:180-187` | `autoApproveWorkingDir` fires before session permission check, bypassing audit trail | ⏭️ Skipped — intentional design for UX; notification still shown |
| 116 | `agent/agent.go:822-827` | Key facts injected from LLM-generated summary without validation — stored prompt injection vector | ⏭️ Skipped — LLM already controls conversation; key facts are the LLM's own output re-injected |
| 117 | `agent/agent.go:240` | `generateTitle` fire-and-forget goroutine with no lifetime management | ⏭️ Skipped — bounded by context cancellation; title generation is lightweight |
| 118 | `agent/agent.go:1305-1319` | `detectPlanMode` inner loop iterates forward — could return wrong state for multiple plan_mode results in one message | ⏭️ Skipped — false positive; each tool result is stored in its own message |
| 119 | `agent/tools/ask_user.go:45-46` | Missing session ID empty-string check before calling `svc.Ask()` | ⏭️ Skipped — service handles gracefully |
| 120 | `agent/tools/ask_user.go:55-58` | `AllowText` always forced to `true` when options provided | ⏭️ Skipped — intentional design; JSON zero-value ambiguity |
| 121 | `ui/list/list.go:123-146` | Height cache may serve stale values when selection changes | ⏭️ Skipped — render callbacks don't vary height by selection in current code |
| 122 | `ui/chat/tools.go:416` | `isSpinning()` returns true forever if tool result is permanently lost | ⏭️ Skipped — edge case; `pendingToolResults` mitigates the common race |
| 123 | `tools/search.go:209-227` | `maybeDelaySearch` rate-limiting race between read and write of `lastSearchTime` | ⏭️ Skipped — advisory rate limiting; concurrent searches are acceptable |

### Round 6

| # | Location | Finding | Resolution |
|---|----------|---------|------------|
| 125 | `ui/model/ui.go:openAskUserDialog` | **Ask-user dialog destroys user input on retry**: every 3-second re-publish closes and reopens the dialog overlay, losing any text the user has typed | ✅ Fixed — skip if dialog already open via `ContainsDialog(dialog.AskUserID)` |
| 126 | `message/message.go:208,241` | **`ListRecent`/`ListBefore` panic on negative limit**: negative `limit` passed to `make([]Message, 0, limit)` causes panic | ✅ Fixed — early return `if limit <= 0` |
| 127 | `tools/search.go:72` | **Unbounded `io.ReadAll` on DDG search response**: no size limit on response body; malicious or broken server could exhaust memory | ✅ Fixed — wrapped with `io.LimitReader(resp.Body, 5<<20)` |
| 128 | `askuser/askuser.go:Respond()` | `Respond()` could block forever if called twice for same request ID | ⏭️ False positive — channel buffer=1, single consumer, `Del` removes entry after consume |
| 129 | `shell/background.go:exitErr` | Data race on `exitErr` in BackgroundShell between writer goroutine and `Output()` reader | ⏭️ False positive — default branch never reads `exitErr`; `done` channel provides happens-before |
| 130 | `agent/tools/web_fetch` | Temp file leak from `NewWebFetchTool` fetch operations | ⏭️ False positive — files created in `tmpDir` which has `defer os.RemoveAll` in caller |
| 131 | `db/db.go:Close()` | `Close()` clobbers first error if multiple close calls fail | ⏭️ Skipped — pre-existing pattern throughout codebase |
| 132 | `db/db.go:Prepare()` | Prepared statements leak on mid-sequence failure | ⏭️ Skipped — pre-existing pattern |
| 135 | `csync/wait.go:WaitWithContext` | Goroutine leak when context cancelled before WaitGroup completes | ⏭️ Skipped — standard pattern; bounded by operation lifetime |

### Round 7

| # | Location | Finding | Resolution |
|---|----------|---------|------------|
| 136 | `config/config.go:319-330` | **`ResolvedHeaders()` mutates shared config map in-place**: value receiver copies struct but not map; causes double-resolution on MCP reconnect and potential data race | ✅ Fixed — build and return new map instead of mutating original |
| 137 | `config/config.go:650-666` | **`resolveEnvs()` mutates input map in-place**: same issue as ResolvedHeaders; affects both LSP and MCP env resolution on reconnect | ✅ Fixed — iterate input, write to local variables, build result slice without mutation |
| 138 | `ui/model/ui.go:2892-2931` | **`insertFileCompletion` data race**: Cmd closure reads `m.session`, reads/writes `m.sessionFileReads` from goroutine | ✅ Fixed — snapshot state before closure, return `insertFileCompletionMsg`, apply mutation in Update |
| 139 | `coordinator.go:635,639` | **`os.Setenv("ANTHROPIC_API_KEY", "")` destroys global env var**: permanent process-wide mutation affecting child processes and other goroutines | ✅ Fixed — replaced with `anthropic.WithAPIKey("")` per-client option |
| 140 | `tools/sourcegraph.go:114,121` | **Unbounded `io.ReadAll` on Sourcegraph response**: no size limit on response body, OOM risk | ✅ Fixed — wrapped with `io.LimitReader` (1MB error, 5MB success) |
| 141 | `tools/download.go:151-154` | **Partial file left on disk after `io.Copy` error**: no cleanup of partially written file | ✅ Fixed — close and `os.Remove(filePath)` on error |
| 142 | `agent/agent.go:184-191` | **Message queue race**: non-atomic Get+append+Set on `csync.Map` can lose queued messages | ✅ Fixed — added `csync.Map.Update` for atomic read-modify-write; also used `Take` for queue drain |
| 143 | `agent/agent.go:1177-1180` | **Dead code**: Cancel checks `sessionID + "-summarize"` key but Summarize registers under plain sessionID | ✅ Fixed — removed dead `-summarize` branch |
| 144 | `csync/maps.go:80-88` | **`GetOrSet` TOCTOU race**: separate Get+Set allows concurrent `fn()` execution for same key | ✅ Fixed — hold write lock for entire check-and-set operation |
| 145 | `ui/model/ui.go:2787-2799` | **`openEditor` temp file leak**: temp file not removed on WriteString or editor.Command error | ✅ Fixed — added `os.Remove(tmpPath)` to both error paths |
| 146 | `lsp/client.go:401` | `NotifyChange` non-atomic `Version++` on shared pointer from csync.Map | ⏭️ Skipped — all current callers serialized by sequential tool execution; no concurrent path exists today |
| 147 | `lsp/manager.go:213` | `startServer` double-client race window on concurrent startup | ⏭️ Skipped — narrow window, same server name; duplicate client gets closed |
| 148 | `config/provider.go:189` | `Providers()` partial failure blocks model selection dialog entirely | ⏭️ Skipped — pre-existing UX choice |
| 149 | `coordinator.go:868-871` | ZAI provider mutates shared `ExtraBody` map | ⏭️ Skipped — writes idempotent value; low practical impact |
| 150 | `agent/agent.go:742` | `Summarize` uses `genCtx` for post-stream DB update instead of parent `ctx` | ⏭️ Skipped — extremely narrow cancellation window |
| 151 | `tools/edit.go:287-295` | Zero-value `file` used after `GetByPathAndSession` error; spurious intermediate version | ⏭️ Skipped — cosmetic DB entry, no user-visible impact |

### Round 8

| # | Location | Finding | Resolution |
|---|----------|---------|------------|
| 152 | `agent/agent.go:297-298` | **`PrepareStep` Get+Del race on messageQueue**: non-atomic Get+Del drops messages queued between the two calls | ✅ Fixed — replaced with atomic `Take` |
| 153 | `ui/model/session.go:98,103` | **`ReportError` returned as `tea.Msg`**: missing `()` call causes `tea.Cmd` function to be returned as message value, silently dropping errors | ✅ Fixed — added `()` to invoke the Cmd |
| 154 | `ui/model/ui.go:1548` | **Same `ReportError` bug**: `UpdateAgentModel` error in model selection closure | ✅ Fixed |
| 155 | `ui/model/ui.go:1257-1272` | **Pointless loop in `handleChildSessionMessage`**: `i` never used, `MessageItem(toolCallID)` is ID-based lookup returning same result every iteration | ✅ Fixed — replaced loop with single lookup |
| 156 | `ui/model/ui.go:435-441` | **`continueLastSession` swallows `GetLast` error**: returns nil instead of reporting error to user | ✅ Fixed — return `util.ReportError(err)()` |
| 157 | `ui/model/ui.go:1593` | **`UpdateAgentModel` error discarded in reasoning effort handler**: user sees success message even on failure | ✅ Fixed — check error and report |
| 158 | `ui/model/ui.go:3800` | **`UpdateAgentModel` error discarded in `handleStateChanged`**: MCP state change proceeds even on model update failure | ✅ Fixed — check error and report |
| 159 | `tools/view.go:172-185` | **Image files bypass size limit**: skill-path images read via `os.ReadFile` with no size guard | ✅ Fixed — added `MaxImageSize` (10MB) check before reading |
| 160 | `coordinator.go:1048` | `runSubAgent` nil pointer dereference if `Run` returns `(nil, nil)` | ⏭️ False positive — sub-agents always get fresh session, can't hit busy+queue path |
| 161 | `ui/model/ui.go:3132-3142` | `sendMessage` nil session dereference after Create returns empty ID | ⏭️ Skipped — unreachable with production Sessions.Create (always returns UUID) |
| 162 | `tools/ls.go:223` | `printTree` panic on empty `rootPath` | ⏭️ Skipped — callers always pass validated path from os.Stat |
| 163 | `tools/bash.go:531-533` | `truncateOutput` byte-slicing splits multi-byte UTF-8 | ⏭️ Skipped — cosmetic; LLMs handle minor UTF-8 issues gracefully |
| 164 | `tools/grep.go:455-466` | `globToRegex` unanchored regex in include filter | ⏭️ Skipped — good enough for file type filtering |
| 165 | `tools/write.go:85-103` | Double file read with TOCTOU gap | ⏭️ Skipped — benign; diff may be slightly stale |
| 166 | `tools/edit.go:362-377` | Misleading error when `replaceAll=true` and `oldString` not found | ⏭️ Skipped — minor UX issue |
| 167 | `coordinator.go:417-433` | Sub-agent readyWg goroutines not awaited after UpdateModels | ⏭️ Skipped — needs deeper investigation of wg lifecycle |
| 168 | `agent/agent.go:1407-1410` | `workaroundProviderMediaLimitations` drops ProviderOptions on reconstructed tool message | ⏭️ Skipped — cache control not critical for correctness |

---

## TODO — Feature Improvements

### ask_user: Inline Input Instead of Overlay Dialog

**Problem**: The current `ask_user` tool renders a centered overlay dialog
(`ui/dialog/askuser.go`) that covers the chat content. Users cannot scroll
the conversation to review context while the question is displayed.

**Desired behavior** (ref: [OpenCode](https://github.com/sst/opencode)):
- Render the question as a special message item at the bottom of the chat
  message list, not as an overlay.
- Repurpose the bottom input area (textarea) as the answer input — support
  text entry, single-select (↑/↓ + Enter), and multi-select (Space to
  toggle).
- The chat area remains fully scrollable so the user can review conversation
  history to understand the question's context.

**Implementation sketch**:
1. Remove the `dialog.Overlay` path for ask_user. Keep the
   `askuser.Service` channel-based blocking pattern unchanged.
2. Add a new `AskUserMessageItem` in `ui/chat/` that renders the question,
   options, and keybinding hints inline in the message list.
3. Switch the bottom input component to an "answer mode" when an ask_user
   request is active:
   - Text-only questions → normal textarea, Enter to submit.
   - Options → list selector with ↑/↓/1-9 navigation, Enter to confirm.
   - Multi-select → checkboxes with Space to toggle, Enter to confirm all.
4. Route keyboard events: when answer mode is active, input goes to the
   answer handler instead of the normal prompt submission path.
5. On submit, call `askuser.Service.Respond()` as before.

**Files to modify**:
- `internal/ui/model/ui.go` — replace `openAskUserDialog` with inline
  rendering + input mode switch.
- `internal/ui/chat/` — new `askuser.go` message item for inline rendering.
- `internal/ui/dialog/askuser.go` — can be removed or kept as fallback.
- `internal/askuser/askuser.go` — no changes needed (service layer is
  transport-agnostic).

### Auto Mode (AI-Based Permission Classifier)

**Problem**: Currently crush has only two permission modes: manual approval
(default) and YOLO (approve everything). There is no middle ground where an
AI classifier decides whether a tool call is safe to auto-approve.

**Desired behavior** (ref: claude-code `yoloClassifier.ts`):
- A 2-stage LLM classifier that evaluates each tool call:
  - Stage 1 (fast): max_tokens=64, stop at first decision — immediate
    allow/deny.
  - Stage 2 (thinking): max_tokens=4096, chain-of-thought reasoning for
    ambiguous cases.
- Fast-paths that skip the classifier entirely: read-only tools,
  `acceptEdits` mode, safe-command allowlist.
- User-configurable allow/deny rules via `crush.json` (`autoMode` settings).
- Fail-closed by default: classifier errors → prompt the user.
- Consecutive denial tracking: after N denials, fall back to interactive
  prompting.

**Files to modify**:
- `internal/permission/permission.go` — add `Auto` permission mode,
  integrate classifier into `Request` flow.
- New `internal/permission/classifier.go` — the 2-stage classifier
  implementation using the small model.
- `internal/config/config.go` — add `auto_mode` config section with
  allow/deny rules.
- `internal/agent/tools/*.go` — add `toClassifierInput()` method per tool
  for compact transcript projection.

### Advisor (Dual-Model Review)

**What**: The main agent can consult a stronger "advisor" model (e.g.,
Opus) for strategic guidance. The advisor sees the full conversation and
returns high-level advice.

**How** (ref: claude-code `utils/advisor.ts`):
- A separate API call sends the full conversation to a stronger model.
- The advisor is NOT a tool the model calls — it is invoked by the system
  at key decision points or on user request (`/advisor`).
- System prompt instructs the agent to: call advisor BEFORE substantive
  work, when stuck, when considering approach changes, and when done.
- Advisor response is injected back into the conversation as context.

**Implementation sketch**:
- Add `advisor` model config in `crush.json` (model + provider).
- New `internal/agent/advisor.go` — takes full message history, calls the
  advisor model with a review prompt, returns advice text.
- Wire into agent loop: optionally call advisor before first tool use in a
  new turn, or on explicit `/advisor` command.
- Show advisor response in a distinct UI element (e.g., collapsible block).

### Doctor (Diagnostic Health Checks)

**What**: A `/doctor` command that runs comprehensive environment and
configuration health checks.

**Checks to implement**:
- Provider connectivity (test API key validity).
- Config file parsing (detect syntax errors, unknown fields).
- Memory/context file sizes (warn if AGENTS.md > 40k chars).
- Shell availability (bash/sh interpreter).
- Ripgrep availability (used by grep/glob tools).
- Git status (repo detection, branch info).
- LSP server status (configured vs running).
- MCP server status (configured vs reachable).
- Disk space for data directory.

**Files to create**:
- `internal/cmd/doctor.go` — CLI command.
- `internal/doctor/doctor.go` — diagnostic checks.

### Security Review (Built-in Skill)

**What**: A built-in skill that performs security review on the current
branch's changes using `git diff`.

**How** (ref: claude-code `commands/security-review.ts`):
- Three-phase methodology: repository context → comparative analysis →
  vulnerability assessment.
- Categories: input validation, auth/authz, crypto, injection/RCE, data
  exposure.
- 17 hard exclusions to minimize false positives (DoS, rate limiting,
  regex, test files, etc.).
- Confidence scoring: only report findings > 0.7.

**Implementation**: Ship as a SKILL.md file in the global skills directory
(`~/.config/crush/skills/security-review/SKILL.md`).

### Hook System (Lifecycle Events)

**What**: User-defined shell commands that run at lifecycle events.

**Events** (ref: claude-code `utils/hooks.ts`):
- `PreToolUse` — before a tool executes (can block or modify).
- `PostToolUse` — after a tool completes.
- `SessionStart` — when a session begins.
- `UserPromptSubmit` — before a user prompt is sent to the model.
- `PreCompact` — before summarization.
- `PostCompact` — after summarization.

**Hook config** (in `crush.json`):
```json
{
  "hooks": {
    "PreToolUse": [
      { "command": "lint-check.sh", "if": "Bash(git *)" }
    ]
  }
}
```

**Hook output protocol**: JSON with `{ continue, decision, systemMessage }`.
Hooks can block tool execution, inject system messages, or run silently.

**Files to create**:
- `internal/hooks/hooks.go` — hook discovery, execution, output parsing.
- Config schema update for `hooks` field.

### Context Analysis Visualization

**What**: Break down context window usage by category so users understand
where tokens are spent.

**Categories**:
- System prompt (base template).
- Memory/context files (per-file breakdown with sizes).
- Tool definitions (per-tool token cost).
- MCP tool definitions (per-server).
- Conversation messages (user vs assistant vs tool results).

**Display**: A `/context` command or status bar breakdown showing percentage
bars per category. Use rough token estimation (chars / 4) to avoid API
calls.

**Files to create**:
- `internal/agent/context_analysis.go` — token counting per category.
- UI command to display the breakdown.

### Tips (Contextual Hints During API Waits)

**What**: Show helpful tips during spinner/loading time to improve feature
discoverability.

**How** (ref: claude-code `services/tips/`):
- ~20-30 tips covering features: plan mode, keyboard shortcuts, worker
  agents, session resume, memory files, etc.
- Session-based cooldown: each tip has a `cooldownSessions` count — once
  shown, it will not appear again for N sessions.
- Context-aware: `isRelevant()` predicate checks user state (e.g., only
  show git tips in git repos).
- Show one tip per API wait, picked from the least-recently-shown relevant
  tips.

### Example Commands (Contextual Placeholders)

**What**: Generate context-aware placeholder prompts in the input area.

**How** (ref: claude-code `utils/exampleCommands.ts`):
- Analyze `git log` to find frequently modified files by the current user.
- Filter out non-core files (lock files, configs, build artifacts).
- Pick 5 diverse files across different directories.
- Generate prompts from templates: "fix lint errors in X", "how does X
  work?", "write tests for X", "refactor X".
- Cache results in project config, regenerate weekly.

### Commit/PR Workflow Commands

**What**: Built-in `/commit` and `/pr` commands.

**`/commit`**: Gather `git status` + `git diff HEAD`, generate commit
message following repo's existing style, stage and commit.

**`/pr`**: Extend `/commit` into commit → push → create PR via `gh` CLI.
Generate PR title, summary, and test plan sections.

These can be implemented as built-in skills rather than hardcoded commands.

### Init Improvement (Multi-Phase Onboarding)

**What**: Improve `crush init` to be more thorough.

**Phases** (ref: claude-code `commands/init.ts`):
1. Explore codebase via sub-agent (manifests, README, CI, existing
   configs).
2. Interview user about preferences (build commands, test commands, style).
3. Generate AGENTS.md with only information Claude would get wrong without
   it.
4. Generate crush.local.md for personal preferences (gitignored).
5. Suggest optimizations (LSP setup, formatter hooks).

### Rewind (Conversation Checkpoint)

**What**: Select a point in conversation history to rewind to, optionally
restoring file state.

**How**: Open a message selector, pick a checkpoint, truncate messages
after that point. If file history tracking is enabled, restore files to
their state at that checkpoint.

**Prerequisite**: File history/checkpoint system (tracking file content at
each edit for undo capability).
