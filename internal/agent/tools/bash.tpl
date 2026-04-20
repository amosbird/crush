Executes bash commands. Commands block until completion — there is no timeout, even for long builds.

<cross_platform>
Uses mvdan/sh interpreter (Bash-compatible on all platforms including Windows).
Use forward slashes for paths: "ls C:/foo/bar" not "ls C:\foo\bar".
Common shell builtins and core utils available on Windows.
</cross_platform>

<execution_steps>
1. Directory Verification: If creating directories/files, use LS tool to verify parent exists
2. Security Check: Banned commands ({{ .BannedCommands }}) return error - explain to user. Safe read-only commands execute without prompts
3. Command Execution: Execute with proper quoting, capture output
4. Output Processing: Truncate if exceeds {{ .MaxOutputLength }} characters
5. Return Result: Include errors, metadata with <cwd></cwd> tags
</execution_steps>

<usage_notes>
- Command required, working_dir optional (defaults to current directory)
- IMPORTANT: Avoid using this tool to run find, grep, cat, head, tail, ls commands unless explicitly instructed or after verifying a dedicated tool cannot accomplish the task. Use Grep/Glob/Agent/View/LS tools instead — they provide a better experience
- If commands are independent and can run in parallel, make multiple bash tool calls in a single message (e.g., git status and git diff as two parallel calls)
- If commands depend on each other and must run sequentially, chain with '&&' in a single call
- Avoid newlines in commands except in quoted strings
- Each command runs in independent shell (no state persistence between calls)
- Prefer absolute paths over 'cd' (use 'cd' only if user explicitly requests)
- Quote file paths that contain spaces or special characters
</usage_notes>

<background_execution>
- IMPORTANT: NEVER use run_in_background for builds, tests, git operations, or
  any command that eventually exits. Those commands block until done — even if
  they take minutes. There is no timeout.
- run_in_background=true is ONLY for processes that run forever and never exit
  on their own: servers, watchers, daemons (e.g., `npm start`, `node server.js`,
  `python -m http.server`, `npm run watch`, `tail -f`).
- NEVER use `&` at the end of commands to run in background - use
  run_in_background parameter instead.
- To kill a background process, use kill_background_id with the shell ID
  returned when the process was started. When kill_background_id is set,
  command and other params are ignored.
</background_execution>

<process_lifecycle>
CRITICAL: Every shell command MUST eventually exit on its own. The shell
interpreter waits for all child processes — if any keep running, the job
never completes and appears stuck.

- If a script starts a background service (server, daemon) as a helper for
  later commands, the script MUST kill it before exiting:
    server &
    SERVER_PID=$!
    trap "kill $SERVER_PID 2>/dev/null; wait $SERVER_PID 2>/dev/null" EXIT
    run_tests
    # EXIT trap fires automatically

- NEVER start persistent processes (servers, watchers, daemons) inside a
  synchronous (non-background) bash call. If you need a long-running
  service, start it in a SEPARATE bash call with run_in_background=true,
  then run your commands in another bash call.

- When using run_in_background=true for a service started at the user's
  request, leave it running — the user will manage its lifecycle.

- When writing multi-step scripts that start helper services:
  1. Start the service, capture its PID
  2. Set a trap to kill it on EXIT/ERR
  3. Wait for it to be ready (poll a port/endpoint, not sleep)
  4. Run your actual work
  5. The trap handles cleanup automatically
</process_lifecycle>

<git_safety>
Git Safety Protocol:
- NEVER update the git config
- NEVER run destructive git commands (push --force, reset --hard, checkout ., restore ., clean -f, branch -D) unless the user explicitly requests these actions
- NEVER skip hooks (--no-verify) or bypass signing (--no-gpg-sign) unless the user explicitly asks. If a hook fails, investigate and fix the underlying issue
- NEVER force push to main/master — warn the user if they request it
- CRITICAL: Always create NEW commits rather than amending, unless the user explicitly requests amend. When a pre-commit hook fails, the commit did NOT happen — so --amend would modify the PREVIOUS commit, destroying work. After hook failure, fix the issue, re-stage, and create a NEW commit
- When staging files, prefer adding specific files by name rather than "git add -A" or "git add ." which can accidentally include sensitive files (.env, credentials) or large binaries
- Before running destructive operations (e.g., git reset --hard, git push --force, git checkout --), consider whether there is a safer alternative. Only use destructive operations when truly the best approach
</git_safety>

<git_commits>
Only create commits when requested by the user. If unclear, ask first.

1. Single message with three tool_use blocks in parallel (IMPORTANT for speed):
   - git status (untracked files — never use -uall flag on large repos)
   - git diff (staged/unstaged changes)
   - git log (recent commit message style)

2. Analyze all staged changes and draft a commit message:
   - List changed/added files, summarize nature (feature/enhancement/bug fix/refactoring/test/docs)
   - Check for sensitive info (.env, credentials) — do not commit those, warn the user
   - Draft concise (1-2 sentences) message focusing on "why" not "what"
   - Use clear language, accurate reflection ("add"=new feature, "update"=enhancement, "fix"=bug fix)

3. Stage relevant files by name (not "git add -A") and create commit{{ if or (eq .Attribution.TrailerStyle "assisted-by") (eq .Attribution.TrailerStyle "co-authored-by")}} with attribution{{ end }} using HEREDOC:
   git commit -m "$(cat <<'EOF'
   Commit message here.

{{ if .Attribution.GeneratedWith }}
   💘 Generated with Crush
{{ end}}
{{if eq .Attribution.TrailerStyle "assisted-by" }}

   Assisted-by: {{ .ModelName }} via Crush <crush@charm.land>
{{ else if eq .Attribution.TrailerStyle "co-authored-by" }}

   Co-Authored-By: Crush <crush@charm.land>
{{ end }}

   EOF
   )"

4. If pre-commit hook fails, fix the issue and create a NEW commit (do NOT amend).

5. Run git status to verify.

Notes: Use "git commit -am" when possible, don't stage unrelated files, NEVER update config, don't push, no -i flags, no empty commits, return empty response.
</git_commits>

<pull_requests>
Use gh command for ALL GitHub tasks. When user asks to create PR:

1. Single message with multiple tool_use blocks (VERY IMPORTANT for speed):
   - git status (untracked files)
   - git diff (staged/unstaged changes)
   - Check if branch tracks remote and is up to date
   - git log and 'git diff main...HEAD' (full commit history from main divergence)

2. Create new branch if needed
3. Commit changes if needed
4. Push to remote with -u flag if needed

5. Analyze changes in <pr_analysis> tags:
   - List commits since diverging from main
   - Summarize nature of changes
   - Brainstorm purpose/motivation
   - Assess project impact
   - Don't use tools beyond git context
   - Check for sensitive information
   - Draft concise (1-2 bullet points) PR summary focusing on "why"
   - Ensure summary reflects ALL changes since main divergence
   - Clear, concise language
   - Accurate reflection of changes and purpose
   - Avoid generic summaries
   - Review draft

6. Create PR with gh pr create using HEREDOC:
   gh pr create --title "title" --body "$(cat <<'EOF'

   ## Summary

   <1-3 bullet points>

   ## Test plan

   [Checklist of TODOs...]

{{ if .Attribution.GeneratedWith}}
   💘 Generated with Crush
{{ end }}

   EOF
   )"

Important:

- Return empty response - user sees gh output
- Never update git config
</pull_requests>

<examples>
Good: pytest /foo/bar/tests
Bad: cd /foo/bar && pytest tests
</examples>
