package chat

import (
	"encoding/json"
	"log/slog"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

type PlanModeToolRenderContext struct{}

func NewPlanModeToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &PlanModeToolRenderContext{}, canceled)
}

func (p *PlanModeToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	if opts.IsPending() {
		return pendingTool(sty, "Plan Mode", opts.Anim, opts.Compact, opts.CreatedAt)
	}

	cappedWidth := cappedMessageWidth(width)

	var meta tools.PlanModeResponseMetadata
	if opts.HasResult() {
		if err := json.Unmarshal([]byte(opts.Result.Metadata), &meta); err != nil {
			slog.Error("Failed to unmarshal tool result metadata", "tool", "plan_mode", "error", err)
		}
	}

	if !opts.HasResult() {
		var params tools.PlanModeParams
		if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
			slog.Error("Failed to unmarshal tool call input", "tool", "plan_mode", "error", err)
		}

		if params.Mode == "implement" && params.Plan != "" {
			header := toolHeader(sty, opts.Status, "Plan Mode", cappedWidth, opts.Compact, "reviewing plan")
			if opts.Compact {
				return header
			}
			if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
				return joinToolParts(header, earlyState)
			}
			bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
			body := toolOutputMarkdownContent(sty, params.Plan, bodyWidth, opts.ExpandedContent)
			return joinToolParts(header, body)
		}
		header := toolHeader(sty, opts.Status, "Plan Mode", cappedWidth, opts.Compact)
		if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
			return joinToolParts(header, earlyState)
		}
		return header
	}

	detail := "activated"
	if !meta.PlanActive {
		detail = "deactivated"
	}
	if meta.PlanActive && meta.Mode == "plan" {
		return ""
	}

	header := toolHeader(sty, ToolStatusSuccess, "Plan Mode", cappedWidth, opts.Compact, detail)
	if opts.Compact {
		return header
	}

	if meta.Plan != "" {
		bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
		body := toolOutputMarkdownContent(sty, meta.Plan, bodyWidth, opts.ExpandedContent)
		return joinToolParts(header, body)
	}

	return header
}
