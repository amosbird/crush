Creates and manages a structured task list for tracking progress on complex, multi-step coding tasks. This helps you track progress, organize complex tasks, and demonstrate thoroughness to the user. It also helps the user understand the progress and overall status of their requests.

<when_to_use>
Use this tool proactively in these scenarios:

- Complex multi-step tasks requiring 3+ distinct steps or actions
- Non-trivial tasks requiring careful planning or multiple operations
- User explicitly requests todo list management
- User provides multiple tasks (numbered or comma-separated list)
- After receiving new instructions to capture requirements
- When starting work on a task (mark as in_progress BEFORE beginning)
- After completing a task (mark completed and add new follow-up tasks discovered during implementation)
</when_to_use>

<when_not_to_use>
Skip this tool when:

- Single, straightforward task
- Trivial task with no organizational benefit
- Task completable in less than 3 trivial steps
- Purely conversational or informational request
</when_not_to_use>

<usage_examples>
Example 1 — Multi-step feature:
  User: "Add dark mode toggle to settings. Run tests when done!"
  → Create todos: 1) Create toggle component, 2) Add state management, 3) Implement theme styles, 4) Update components for theme switching, 5) Run tests and build

Example 2 — Scoped discovery then action:
  User: "Rename getCwd to getCurrentWorkingDirectory across the project"
  → First search to understand scope, THEN create todos for each file

Example 3 — DON'T use:
  User: "What does git status do?" → Just answer, no todo needed
  User: "Add a comment to the calculateTotal function" → Single edit, no todo needed
</usage_examples>

<task_states>
- **pending**: Task not yet started
- **in_progress**: Currently working on (limit to ONE task at a time)
- **completed**: Task finished successfully

**IMPORTANT**: Each task requires two forms:
- **content**: Imperative form describing what needs to be done (e.g., "Run tests", "Build the project")
- **active_form**: Present continuous form shown during execution (e.g., "Running tests", "Building the project")
</task_states>

<task_management>
- Update task status in real-time as you work
- Mark tasks complete IMMEDIATELY after finishing (don't batch completions)
- Exactly ONE task must be in_progress at any time (not less, not more)
- Complete current tasks before starting new ones
- Remove tasks that are no longer relevant from the list entirely
</task_management>

<completion_requirements>
ONLY mark a task as completed when you have FULLY accomplished it.

Never mark completed if:
- Tests are failing
- Implementation is partial
- You encountered unresolved errors
- You couldn't find necessary files or dependencies

If blocked:
- Keep task as in_progress
- Create new task describing what needs to be resolved
</completion_requirements>

<task_breakdown>
- Create specific, actionable items
- Break complex tasks into smaller, manageable steps
- Use clear, descriptive task names
- Always provide both content and active_form
</task_breakdown>

<output_behavior>
**NEVER** print or list todos in your response text. The user sees the todo list in real-time in the UI.
</output_behavior>

<tips>
- When in doubt, use this tool — being proactive demonstrates attentiveness
- One task in_progress at a time keeps work focused
- Update immediately after state changes for accurate tracking
</tips>
