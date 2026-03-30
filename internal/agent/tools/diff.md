Compares files or shows git diffs with unified diff output.

<modes>
1. **Compare two files**: Provide `file_a` and `file_b` to see a unified diff between any two files
2. **Git diff vs ref**: Provide `ref` (e.g., HEAD, main, HEAD~3) to see changes relative to a git ref. Optionally add `file_a` to limit to one file
3. **Git unstaged changes**: Provide no arguments (or just `file_a`) to see current unstaged changes
</modes>

<usage>
- Provide file_a + file_b for file comparison
- Provide ref for git diff (e.g., "HEAD", "main", "HEAD~3", "origin/main")
- Provide ref + file_a to see changes to a specific file vs a ref
- Provide just file_a to see unstaged changes to that file
- Provide nothing to see all unstaged changes
</usage>

<tips>
- Use this instead of running `git diff` via bash — it's faster and the output is structured
- Combine with `view` tool: use diff to find what changed, then view to see full context
- Use `ref: "HEAD"` to see staged + unstaged changes
- Use `ref: "main"` to see all changes on current branch vs main
</tips>

<limitations>
- Requires `diff` command for file comparison mode
- Requires `git` for git diff modes (must be in a git repo)
- Output may be large for many changes — consider limiting to specific files
</limitations>
