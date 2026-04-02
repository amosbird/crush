package chat

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/tree"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// -----------------------------------------------------------------------------
// Agent Tool
// -----------------------------------------------------------------------------

// NestedToolContainer is an interface for tool items that can contain nested tool calls.
type NestedToolContainer interface {
	NestedTools() []ToolMessageItem
	SetNestedTools(tools []ToolMessageItem)
	AddNestedTool(tool ToolMessageItem)
}

// AgentToolMessageItem is a message item that represents an agent tool call.
type AgentToolMessageItem struct {
	*baseToolMessageItem

	nestedTools []ToolMessageItem
}

var (
	_ ToolMessageItem     = (*AgentToolMessageItem)(nil)
	_ NestedToolContainer = (*AgentToolMessageItem)(nil)
)

// NewAgentToolMessageItem creates a new [AgentToolMessageItem].
func NewAgentToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) *AgentToolMessageItem {
	t := &AgentToolMessageItem{}
	t.baseToolMessageItem = newBaseToolMessageItem(sty, toolCall, result, &AgentToolRenderContext{agent: t}, canceled)
	// For the agent tool we keep spinning until the tool call is finished.
	t.spinningFunc = func(state SpinningState) bool {
		return !state.HasResult() && !state.IsCanceled()
	}
	return t
}

// Animate progresses the message animation if it should be spinning.
func (a *AgentToolMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if a.result != nil || a.Status() == ToolStatusCanceled {
		return nil
	}
	if msg.ID == a.ID() {
		return a.anim.Animate(msg)
	}
	for _, nestedTool := range a.nestedTools {
		if msg.ID != nestedTool.ID() {
			continue
		}
		if s, ok := nestedTool.(Animatable); ok {
			return s.Animate(msg)
		}
	}
	return nil
}

// NestedTools returns the nested tools.
func (a *AgentToolMessageItem) NestedTools() []ToolMessageItem {
	return a.nestedTools
}

// SetNestedTools sets the nested tools.
func (a *AgentToolMessageItem) SetNestedTools(tools []ToolMessageItem) {
	a.nestedTools = tools
	a.clearCache()
}

// AddNestedTool adds a nested tool.
func (a *AgentToolMessageItem) AddNestedTool(tool ToolMessageItem) {
	// Mark nested tools as simple (compact) rendering.
	if s, ok := tool.(Compactable); ok {
		s.SetCompact(true)
	}
	a.nestedTools = append(a.nestedTools, tool)
	a.clearCache()
}

// AgentToolRenderContext renders agent tool messages.
type AgentToolRenderContext struct {
	agent *AgentToolMessageItem
}

// RenderTool implements the [ToolRenderer] interface.
func (r *AgentToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if !opts.ToolCall.Finished && !opts.IsCanceled() && len(r.agent.nestedTools) == 0 {
		return pendingTool(sty, "Agent", opts.Anim, opts.Compact, opts.CreatedAt)
	}

	var params agent.AgentParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		slog.Error("Failed to unmarshal tool call input", "tool", "agent", "error", err)
	}

	prompt := params.Prompt
	prompt = strings.ReplaceAll(prompt, "\n", " ")

	header := toolHeader(sty, opts.Status, "Agent", cappedWidth, opts.Compact)
	if opts.Compact {
		return header
	}

	// Build the task tag and prompt.
	taskTag := sty.Tool.AgentTaskTag.Render("Task")
	taskTagWidth := lipgloss.Width(taskTag)

	// Calculate remaining width for prompt.
	remainingWidth := min(cappedWidth-taskTagWidth-3, maxTextWidth-taskTagWidth-3) // -3 for spacing

	promptText := sty.Tool.AgentPrompt.Width(remainingWidth).Render(prompt)

	header = lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		lipgloss.JoinHorizontal(
			lipgloss.Left,
			taskTag,
			" ",
			promptText,
		),
	)

	// Build tree with nested tool calls.
	childTools := tree.Root(header)

	nestedTools := r.agent.nestedTools
	collapsed := !opts.ExpandedContent && len(nestedTools) > maxCollapsedNestedTools

	if collapsed {
		hidden := len(nestedTools) - maxCollapsedNestedTools
		childTools.Child(sty.Tool.ContentTruncation.Render(
			fmt.Sprintf("… %d more tool calls [click or space to expand]", hidden),
		))
		nestedTools = nestedTools[len(nestedTools)-maxCollapsedNestedTools:]
	}

	for _, nestedTool := range nestedTools {
		childView := nestedTool.Render(remainingWidth)
		childTools.Child(childView)
	}

	// Build parts.
	var parts []string
	parts = append(parts, childTools.Enumerator(roundedEnumerator(2, taskTagWidth-5)).String())

	// Show animation and elapsed time if still running.
	if !opts.HasResult() && !opts.IsCanceled() {
		animLine := opts.Anim.Render()
		if !opts.CreatedAt.IsZero() {
			animLine += " " + sty.Tool.StateWaiting.Render(formatElapsed(time.Since(opts.CreatedAt)))
		}
		parts = append(parts, "", animLine)
	}

	result := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Add body content when completed.
	if opts.HasResult() && opts.Result.Content != "" {
		body := toolOutputMarkdownContent(sty, opts.Result.Content, cappedWidth-toolBodyLeftPaddingTotal, opts.ExpandedContent)
		return joinToolParts(result, body)
	}

	return result
}

// -----------------------------------------------------------------------------
// Agentic Fetch Tool
// -----------------------------------------------------------------------------

// AgenticFetchToolMessageItem is a message item that represents an agentic fetch tool call.
type AgenticFetchToolMessageItem struct {
	*baseToolMessageItem

	nestedTools []ToolMessageItem
}

var (
	_ ToolMessageItem     = (*AgenticFetchToolMessageItem)(nil)
	_ NestedToolContainer = (*AgenticFetchToolMessageItem)(nil)
)

// NewAgenticFetchToolMessageItem creates a new [AgenticFetchToolMessageItem].
func NewAgenticFetchToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) *AgenticFetchToolMessageItem {
	t := &AgenticFetchToolMessageItem{}
	t.baseToolMessageItem = newBaseToolMessageItem(sty, toolCall, result, &AgenticFetchToolRenderContext{fetch: t}, canceled)
	// For the agentic fetch tool we keep spinning until the tool call is finished.
	t.spinningFunc = func(state SpinningState) bool {
		return !state.HasResult() && !state.IsCanceled()
	}
	return t
}

// NestedTools returns the nested tools.
func (a *AgenticFetchToolMessageItem) NestedTools() []ToolMessageItem {
	return a.nestedTools
}

// SetNestedTools sets the nested tools.
func (a *AgenticFetchToolMessageItem) SetNestedTools(tools []ToolMessageItem) {
	a.nestedTools = tools
	a.clearCache()
}

// AddNestedTool adds a nested tool.
func (a *AgenticFetchToolMessageItem) AddNestedTool(tool ToolMessageItem) {
	// Mark nested tools as simple (compact) rendering.
	if s, ok := tool.(Compactable); ok {
		s.SetCompact(true)
	}
	a.nestedTools = append(a.nestedTools, tool)
	a.clearCache()
}

// Animate progresses the message animation if it should be spinning.
func (a *AgenticFetchToolMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if a.result != nil || a.Status() == ToolStatusCanceled {
		return nil
	}
	if msg.ID == a.ID() {
		return a.anim.Animate(msg)
	}
	for _, nestedTool := range a.nestedTools {
		if msg.ID != nestedTool.ID() {
			continue
		}
		if s, ok := nestedTool.(Animatable); ok {
			return s.Animate(msg)
		}
	}
	return nil
}

// AgenticFetchToolRenderContext renders agentic fetch tool messages.
type AgenticFetchToolRenderContext struct {
	fetch *AgenticFetchToolMessageItem
}

// agenticFetchParams matches tools.AgenticFetchParams.
type agenticFetchParams struct {
	URL    string `json:"url,omitempty"`
	Prompt string `json:"prompt"`
}

// RenderTool implements the [ToolRenderer] interface.
func (r *AgenticFetchToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if !opts.ToolCall.Finished && !opts.IsCanceled() && len(r.fetch.nestedTools) == 0 {
		return pendingTool(sty, "Agentic Fetch", opts.Anim, opts.Compact, opts.CreatedAt)
	}

	var params agenticFetchParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		slog.Error("Failed to unmarshal tool call input", "tool", "agentic_fetch", "error", err)
	}

	prompt := params.Prompt
	prompt = strings.ReplaceAll(prompt, "\n", " ")

	// Build header with optional URL param.
	var toolParams []string
	if params.URL != "" {
		toolParams = append(toolParams, params.URL)
	}

	header := toolHeader(sty, opts.Status, "Agentic Fetch", cappedWidth, opts.Compact, toolParams...)
	if opts.Compact {
		return header
	}

	// Build the prompt tag.
	promptTag := sty.Tool.AgenticFetchPromptTag.Render("Prompt")
	promptTagWidth := lipgloss.Width(promptTag)

	// Calculate remaining width for prompt text.
	remainingWidth := min(cappedWidth-promptTagWidth-3, maxTextWidth-promptTagWidth-3) // -3 for spacing

	promptText := sty.Tool.AgentPrompt.Width(remainingWidth).Render(prompt)

	header = lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		lipgloss.JoinHorizontal(
			lipgloss.Left,
			promptTag,
			" ",
			promptText,
		),
	)

	// Build tree with nested tool calls.
	childTools := tree.Root(header)

	nestedTools := r.fetch.nestedTools
	collapsed := !opts.ExpandedContent && len(nestedTools) > maxCollapsedNestedTools

	if collapsed {
		hidden := len(nestedTools) - maxCollapsedNestedTools
		childTools.Child(sty.Tool.ContentTruncation.Render(
			fmt.Sprintf("… %d more tool calls [click or space to expand]", hidden),
		))
		nestedTools = nestedTools[len(nestedTools)-maxCollapsedNestedTools:]
	}

	for _, nestedTool := range nestedTools {
		childView := nestedTool.Render(remainingWidth)
		childTools.Child(childView)
	}

	// Build parts.
	var parts []string
	parts = append(parts, childTools.Enumerator(roundedEnumerator(2, promptTagWidth-5)).String())

	// Show animation and elapsed time if still running.
	if !opts.HasResult() && !opts.IsCanceled() {
		animLine := opts.Anim.Render()
		if !opts.CreatedAt.IsZero() {
			animLine += " " + sty.Tool.StateWaiting.Render(formatElapsed(time.Since(opts.CreatedAt)))
		}
		parts = append(parts, "", animLine)
	}

	result := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Add body content when completed.
	if opts.HasResult() && opts.Result.Content != "" {
		body := toolOutputMarkdownContent(sty, opts.Result.Content, cappedWidth-toolBodyLeftPaddingTotal, opts.ExpandedContent)
		return joinToolParts(result, body)
	}

	return result
}

// -----------------------------------------------------------------------------
// Worker Tool
// -----------------------------------------------------------------------------

// WorkerToolMessageItem is a message item that represents a worker tool call.
type WorkerToolMessageItem struct {
	*baseToolMessageItem

	nestedTools []ToolMessageItem
}

var (
	_ ToolMessageItem     = (*WorkerToolMessageItem)(nil)
	_ NestedToolContainer = (*WorkerToolMessageItem)(nil)
)

// NewWorkerToolMessageItem creates a new [WorkerToolMessageItem].
func NewWorkerToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) *WorkerToolMessageItem {
	t := &WorkerToolMessageItem{}
	t.baseToolMessageItem = newBaseToolMessageItem(sty, toolCall, result, &WorkerToolRenderContext{worker: t}, canceled)
	t.spinningFunc = func(state SpinningState) bool {
		return !state.HasResult() && !state.IsCanceled()
	}
	return t
}

// Animate progresses the message animation if it should be spinning.
func (w *WorkerToolMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if w.result != nil || w.Status() == ToolStatusCanceled {
		return nil
	}
	if msg.ID == w.ID() {
		return w.anim.Animate(msg)
	}
	for _, nestedTool := range w.nestedTools {
		if msg.ID != nestedTool.ID() {
			continue
		}
		if s, ok := nestedTool.(Animatable); ok {
			return s.Animate(msg)
		}
	}
	return nil
}

// NestedTools returns the nested tools.
func (w *WorkerToolMessageItem) NestedTools() []ToolMessageItem {
	return w.nestedTools
}

// SetNestedTools sets the nested tools.
func (w *WorkerToolMessageItem) SetNestedTools(tools []ToolMessageItem) {
	w.nestedTools = tools
	w.clearCache()
}

// AddNestedTool adds a nested tool.
func (w *WorkerToolMessageItem) AddNestedTool(tool ToolMessageItem) {
	if s, ok := tool.(Compactable); ok {
		s.SetCompact(true)
	}
	w.nestedTools = append(w.nestedTools, tool)
	w.clearCache()
}

// WorkerToolRenderContext renders worker tool messages.
type WorkerToolRenderContext struct {
	worker *WorkerToolMessageItem
}

// RenderTool implements the [ToolRenderer] interface.
func (r *WorkerToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if !opts.ToolCall.Finished && !opts.IsCanceled() && len(r.worker.nestedTools) == 0 {
		return pendingTool(sty, "Worker", opts.Anim, opts.Compact, opts.CreatedAt)
	}

	var params agent.WorkerParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		slog.Error("Failed to unmarshal tool call input", "tool", "worker", "error", err)
	}

	prompt := params.Prompt
	prompt = strings.ReplaceAll(prompt, "\n", " ")

	header := toolHeader(sty, opts.Status, "Worker", cappedWidth, opts.Compact)
	if opts.Compact {
		return header
	}

	taskTag := sty.Tool.AgentTaskTag.Render("Task")
	taskTagWidth := lipgloss.Width(taskTag)

	remainingWidth := min(cappedWidth-taskTagWidth-3, maxTextWidth-taskTagWidth-3)

	promptText := sty.Tool.AgentPrompt.Width(remainingWidth).Render(prompt)

	header = lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		lipgloss.JoinHorizontal(
			lipgloss.Left,
			taskTag,
			" ",
			promptText,
		),
	)

	childTools := tree.Root(header)

	nestedTools := r.worker.nestedTools
	collapsed := !opts.ExpandedContent && len(nestedTools) > maxCollapsedNestedTools

	if collapsed {
		hidden := len(nestedTools) - maxCollapsedNestedTools
		childTools.Child(sty.Tool.ContentTruncation.Render(
			fmt.Sprintf("… %d more tool calls [click or space to expand]", hidden),
		))
		nestedTools = nestedTools[len(nestedTools)-maxCollapsedNestedTools:]
	}

	for _, nestedTool := range nestedTools {
		childView := nestedTool.Render(remainingWidth)
		childTools.Child(childView)
	}

	var parts []string
	parts = append(parts, childTools.Enumerator(roundedEnumerator(2, taskTagWidth-5)).String())

	if !opts.HasResult() && !opts.IsCanceled() {
		animLine := opts.Anim.Render()
		if !opts.CreatedAt.IsZero() {
			animLine += " " + sty.Tool.StateWaiting.Render(formatElapsed(time.Since(opts.CreatedAt)))
		}
		parts = append(parts, "", animLine)
	}

	workerResult := lipgloss.JoinVertical(lipgloss.Left, parts...)

	if opts.HasResult() && opts.Result.Content != "" {
		body := toolOutputMarkdownContent(sty, opts.Result.Content, cappedWidth-toolBodyLeftPaddingTotal, opts.ExpandedContent)
		return joinToolParts(workerResult, body)
	}

	return workerResult
}
