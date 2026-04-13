Retrieves the current output from a background shell.

<usage>
- Provide the shell ID returned from a background bash execution
- Returns the current stdout and stderr output
- Indicates whether the shell has completed execution
- Set wait=true to block until the shell completes or output activity stops
</usage>

<features>
- View output from running background processes
- Check if background process has completed
- Get cumulative output from process start
- Smart wait: when wait=true, waits up to ~15s total across multiple rounds;
  returns early if the job completes, output goes quiet, or context is canceled
</features>

<tips>
- Use this to monitor long-running processes
- Check the 'done' status to see if process completed
- Can be called multiple times to view incremental output
- Use wait=true when you need the final output and exit status (or current output if the request cancels)
- If status is "running" after wait=true, the job is still alive — check the
  agent hint at the end of the output for guidance on what to do next
- If the hint says "still making progress", call job_output again with wait=true
- If the hint says "long-running service", the job will not exit on its own;
  use job_kill when you are done with it
- If the hint says "quiet" or "blocked I/O", the job may be waiting on a
  subprocess — call job_output again or investigate
- When a bash command was automatically moved to background, use wait=true only if the result is needed for subsequent steps. Otherwise let it run — the user can monitor via the UI.
</tips>
