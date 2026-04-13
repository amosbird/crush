// Package agent is the core orchestration layer for Crush AI agents.
//
// It provides session-based AI agent functionality for managing
// conversations, tool execution, and message handling. It coordinates
// interactions between language models, messages, sessions, and tools while
// handling features like automatic summarization, queuing, and token
// management.
package agent

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/filetracker"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/stringext"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/charmbracelet/x/exp/charmtone"
)

const (
	DefaultSessionName = "Untitled Session"

	// autoSummarizeBufferTokens is the buffer reserved below the effective
	// context window (context window minus max output tokens) to trigger
	// auto-summarization. Modeled after claude-code's AUTOCOMPACT_BUFFER_TOKENS.
	autoSummarizeBufferTokens = 13_000

	// maxOutputTokensReserve is the maximum output tokens to reserve when
	// calculating the effective context window for auto-summarization.
	maxOutputTokensReserve = 20_000

	// maxToolResultSize is the maximum character length for a single tool
	// result before it gets truncated when sent to the LLM. This acts as a
	// safety net to prevent oversized tool results from blowing up the
	// context window. Individual tools should still enforce their own limits;
	// this is a centralized backstop.
	maxToolResultSize = 80_000

	// streamIdleTimeout cancels and retries an LLM streaming request when
	// no SSE data arrives within this duration. This prevents indefinite
	// hangs caused by provider connection issues.
	streamIdleTimeout = 2 * time.Minute
)

var userAgent = fmt.Sprintf("Charm-Crush/%s (https://charm.land/crush)", version.Version)

// maxAutoSummarizeDepth limits recursive auto-summarize attempts to prevent
// unbounded loops when summarization fails to reduce context.
const maxAutoSummarizeDepth = 3

// autoSummarizeContinuationPrompt is the prompt sent to the model after
// auto-summarization to resume work. It instructs the model to continue
// directly without acknowledging the summary or asking questions.
const autoSummarizeContinuationPrompt = `The conversation was automatically summarized because the context got too long. The summary above contains the full conversation state including any tool results and user answers. Continue the conversation from where it left off without asking the user any further questions. Resume directly — do not acknowledge the summary, do not recap what was happening, do not preface with "I'll continue" or similar. Pick up the last task as if the break never happened. IMPORTANT: Continue responding in the same spoken language the user was using before the summary.`

//go:embed templates/title.md
var titlePrompt []byte

//go:embed templates/summary.md
var summaryPrompt []byte

// Used to remove <think> tags from generated titles.
var thinkTagRegex = regexp.MustCompile(`(?s)<think>.*?</think>`)

type SessionAgentCall struct {
	SessionID        string
	Prompt           string
	ProviderOptions  fantasy.ProviderOptions
	Attachments      []message.Attachment
	MaxOutputTokens  int64
	Temperature      *float64
	TopP             *float64
	TopK             *int64
	FrequencyPenalty *float64
	PresencePenalty  *float64
	NonInteractive   bool

	// autoSummarizeDepth tracks recursive auto-summarize attempts to
	// prevent unbounded recursion.
	autoSummarizeDepth int
}

type SessionAgent interface {
	Run(context.Context, SessionAgentCall) (*fantasy.AgentResult, error)
	SetModels(large Model, small Model, summary *Model)
	SetTools(tools []fantasy.AgentTool)
	SetSystemPrompt(systemPrompt string)
	Cancel(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	Summarize(context.Context, string, fantasy.ProviderOptions) error
	Model() Model
	SmallModel() Model
	SummaryModel() Model
}

type Model struct {
	Model      fantasy.LanguageModel
	CatwalkCfg catwalk.Model
	ModelCfg   config.SelectedModel
}

type sessionAgent struct {
	largeModel         *csync.Value[Model]
	smallModel         *csync.Value[Model]
	summaryModel       *Model
	summaryModelMu     sync.RWMutex
	systemPromptPrefix *csync.Value[string]
	systemPrompt       *csync.Value[string]
	tools              *csync.Slice[fantasy.AgentTool]

	isSubAgent           bool
	agentName            string
	sessions             session.Service
	messages             message.Service
	fileTracker          filetracker.Service
	disableAutoSummarize bool
	maxTokensToSummarize int64
	autoTitle            bool
	isYolo               bool
	dataDir              string
	notify               pubsub.Publisher[notify.Notification]
	tokenRefresher       func(ctx context.Context) error

	messageQueue   *csync.Map[string, []SessionAgentCall]
	activeRequests *csync.Map[string, context.CancelFunc]
	circuitBreaker *summarizeCircuitBreaker
}

type SessionAgentOptions struct {
	LargeModel           Model
	SmallModel           Model
	SummaryModel         *Model
	SystemPromptPrefix   string
	SystemPrompt         string
	IsSubAgent           bool
	AgentName            string
	FileTracker          filetracker.Service
	DisableAutoSummarize bool
	MaxTokensToSummarize int64
	AutoTitle            bool
	IsYolo               bool
	DataDir              string
	Sessions             session.Service
	Messages             message.Service
	Tools                []fantasy.AgentTool
	Notify               pubsub.Publisher[notify.Notification]
	TokenRefresher       func(ctx context.Context) error
}

func NewSessionAgent(
	opts SessionAgentOptions,
) SessionAgent {
	return &sessionAgent{
		largeModel:           csync.NewValue(opts.LargeModel),
		smallModel:           csync.NewValue(opts.SmallModel),
		summaryModel:         opts.SummaryModel,
		systemPromptPrefix:   csync.NewValue(opts.SystemPromptPrefix),
		systemPrompt:         csync.NewValue(opts.SystemPrompt),
		isSubAgent:           opts.IsSubAgent,
		agentName:            opts.AgentName,
		sessions:             opts.Sessions,
		messages:             opts.Messages,
		fileTracker:          opts.FileTracker,
		disableAutoSummarize: opts.DisableAutoSummarize,
		maxTokensToSummarize: opts.MaxTokensToSummarize,
		autoTitle:            opts.AutoTitle,
		tools:                csync.NewSliceFrom(opts.Tools),
		isYolo:               opts.IsYolo,
		dataDir:              opts.DataDir,
		notify:               opts.Notify,
		tokenRefresher:       opts.TokenRefresher,
		messageQueue:         csync.NewMap[string, []SessionAgentCall](),
		activeRequests:       csync.NewMap[string, context.CancelFunc](),
		circuitBreaker:       newSummarizeCircuitBreaker(),
	}
}

func (a *sessionAgent) Run(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
	if call.Prompt == "" && !message.ContainsTextAttachment(call.Attachments) {
		return nil, ErrEmptyPrompt
	}
	if call.SessionID == "" {
		return nil, ErrSessionMissing
	}

	// Queue the message if busy
	if a.IsSessionBusy(call.SessionID) {
		a.messageQueue.Update(call.SessionID, func(existing []SessionAgentCall) []SessionAgentCall {
			return append(existing, call)
		})
		slog.Debug("Message queued (session busy)",
			"session_id", call.SessionID,
			"queue_size", a.QueuedPrompts(call.SessionID),
			"prompt_preview", truncateString(call.Prompt, 80),
		)
		return nil, nil
	}

	// Merge any previously queued messages into the current prompt.
	if queued, ok := a.messageQueue.Take(call.SessionID); ok && len(queued) > 0 {
		slog.Debug("Merging queued messages",
			"session_id", call.SessionID,
			"queued_count", len(queued),
		)
		var merged strings.Builder
		for _, q := range queued {
			if q.Prompt != "" {
				merged.WriteString(q.Prompt)
				merged.WriteString("\n\n")
			}
			call.Attachments = append(call.Attachments, q.Attachments...)
		}
		merged.WriteString(call.Prompt)
		call.Prompt = merged.String()
	}

	// Copy mutable fields under lock to avoid races with SetTools/SetModels.
	agentTools := a.tools.Copy()
	largeModel := a.largeModel.Get()
	systemPrompt := a.systemPrompt.Get()
	promptPrefix := a.systemPromptPrefix.Get()
	var instructions strings.Builder

	for _, server := range mcp.GetStates() {
		if server.State != mcp.StateConnected {
			continue
		}
		if s := server.Client.InitializeResult().Instructions; s != "" {
			instructions.WriteString(s)
			instructions.WriteString("\n\n")
		}
	}

	var mcpInstructions string
	if s := instructions.String(); s != "" {
		mcpInstructions = "<mcp-instructions>\n" + s + "\n</mcp-instructions>"
	}

	if len(agentTools) > 0 {
		// Add Anthropic caching to the last tool.
		agentTools[len(agentTools)-1].SetProviderOptions(a.getCacheControlOptions())
	}

	agent := fantasy.NewAgent(
		largeModel.Model,
		fantasy.WithSystemPrompt(systemPrompt),
		fantasy.WithTools(agentTools...),
		fantasy.WithUserAgent(userAgent),
	)

	sessionLock := sync.Mutex{}
	currentSession, err := a.sessions.Get(ctx, call.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// Pre-flight summarize: check current token usage *before* sending
	// to the LLM. If the threshold is reached, summarize first then
	// replay the user message with fresh context. Uses effective context
	// window calculation that reserves space for output tokens.
	summarizeThreshold := a.autoSummarizeThreshold(largeModel)
	if summarizeThreshold > 0 && !a.disableAutoSummarize && !a.circuitBreaker.isTripped(call.SessionID) {
		tokens := currentSession.CompletionTokens + currentSession.PromptTokens
		if tokens >= summarizeThreshold {
			if call.autoSummarizeDepth >= maxAutoSummarizeDepth {
				slog.Warn("Skipping pre-flight auto-summarize, max depth reached",
					"session_id", call.SessionID,
					"depth", call.autoSummarizeDepth,
				)
			} else {
				slog.Debug("Pre-flight auto-summarize triggered",
					"session_id", call.SessionID,
					"used_tokens", tokens,
					"threshold", summarizeThreshold,
				)
				if summarizeErr := a.Summarize(ctx, call.SessionID, call.ProviderOptions); summarizeErr != nil {
					a.circuitBreaker.recordFailure(call.SessionID)
					slog.Error("Pre-flight auto-summarize failed, continuing without summarization", "error", summarizeErr)
				} else {
					a.circuitBreaker.recordSuccess(call.SessionID)
					call.autoSummarizeDepth++
					slog.Debug("Pre-flight summarize done, recursing into Run()",
						"session_id", call.SessionID,
						"queue_size", a.QueuedPrompts(call.SessionID),
						"prompt_preview", truncateString(call.Prompt, 80),
					)
					return a.Run(ctx, call)
				}
			}
		}
	}

	msgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		return nil, fmt.Errorf("failed to get session messages: %w", err)
	}

	// Generate or update the session title in the background.
	// When autoTitle is enabled, update the title on every turn.
	// Otherwise, only generate a title for the first message.
	// Title generation runs independently and does not block Run() return.
	if a.autoTitle || len(msgs) == 0 {
		titleCtx := ctx // Copy to avoid race with ctx reassignment below.
		go a.generateTitle(titleCtx, call.SessionID, msgs, call.Prompt)
	}

	// Add the user message to the session.
	_, err = a.createUserMessage(ctx, call)
	if err != nil {
		return nil, err
	}

	// Add the session to the context.
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, call.SessionID)

	genCtx, cancel := context.WithCancel(ctx)
	a.activeRequests.Set(call.SessionID, cancel)

	defer cancel()
	defer a.activeRequests.Del(call.SessionID)

	history, files := a.preparePrompt(msgs, call.Attachments...)

	startTime := time.Now()
	a.eventPromptSent(call.SessionID)

	var currentAssistant *message.Message
	var shouldSummarize bool
	sw := newStreamingWriter(a.messages)
	// Don't send MaxOutputTokens if 0 — some providers (e.g. LM Studio) reject it
	var maxOutputTokens *int64
	if call.MaxOutputTokens > 0 {
		maxOutputTokens = &call.MaxOutputTokens
	}
	result, err := agent.Stream(genCtx, fantasy.AgentStreamCall{
		Prompt:            message.PromptWithTextAttachments(call.Prompt, call.Attachments),
		Files:             files,
		Messages:          history,
		ProviderOptions:   call.ProviderOptions,
		MaxOutputTokens:   maxOutputTokens,
		TopP:              call.TopP,
		Temperature:       call.Temperature,
		PresencePenalty:   call.PresencePenalty,
		TopK:              call.TopK,
		FrequencyPenalty:  call.FrequencyPenalty,
		StreamIdleTimeout: streamIdleTimeout,
		PrepareStep: func(callContext context.Context, options fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
			if a.tokenRefresher != nil {
				if refreshErr := a.tokenRefresher(callContext); refreshErr != nil {
					slog.Warn("Token refresh in PrepareStep failed", "error", refreshErr)
				}
			}

			prepared.Messages = options.Messages
			for i := range prepared.Messages {
				prepared.Messages[i].ProviderOptions = nil
			}

			// Use latest tools (updated by SetTools when MCP tools change).
			prepared.Tools = a.tools.Copy()

			queuedCalls, _ := a.messageQueue.Take(call.SessionID)
			for _, queued := range queuedCalls {
				userMessage, createErr := a.createUserMessage(callContext, queued)
				if createErr != nil {
					return callContext, prepared, createErr
				}
				// Insert a synthetic assistant message before the queued
				// user message so the provider does not merge it into
				// the preceding tool-result user block, which would
				// cause the LLM to overlook the new instruction.
				prepared.Messages = append(prepared.Messages,
					fantasy.Message{
						Role:    fantasy.MessageRoleAssistant,
						Content: []fantasy.MessagePart{fantasy.TextPart{Text: "[New user message received]"}},
					},
				)
				prepared.Messages = append(prepared.Messages, userMessage.ToAIMessage()...)
			}

			prepared.Messages = a.workaroundProviderMediaLimitations(prepared.Messages, largeModel)
			cw := int64(largeModel.CatwalkCfg.ContextWindow)
			usedTokens := currentSession.CompletionTokens + currentSession.PromptTokens
			prepared.Messages = clearToolResultsAfterIdleGap(prepared.Messages, currentSession.UpdatedAt)
			prepared.Messages = clearOldToolResults(prepared.Messages, cw, usedTokens)
			prepared.Messages = truncateLargeToolResults(prepared.Messages)

			lastSystemRoleInx := 0
			systemMessageUpdated := false
			for i, msg := range prepared.Messages {
				// Only add cache control to the last message.
				if msg.Role == fantasy.MessageRoleSystem {
					lastSystemRoleInx = i
				} else if !systemMessageUpdated {
					prepared.Messages[lastSystemRoleInx].ProviderOptions = a.getCacheControlOptions()
					systemMessageUpdated = true
				}
				// Than add cache control to the last 2 messages.
				if i > len(prepared.Messages)-3 {
					prepared.Messages[i].ProviderOptions = a.getCacheControlOptions()
				}
			}

			if mcpInstructions != "" {
				mcpMsg := fantasy.NewSystemMessage(mcpInstructions)
				insertIdx := lastSystemRoleInx + 1
				prepared.Messages = slices.Insert(prepared.Messages, insertIdx, mcpMsg)
				// Move the cache breakpoint from the base system prompt
				// to the MCP instructions message. This keeps the total
				// number of breakpoints unchanged (still 4) while letting
				// Anthropic's prefix cache cover the base prompt naturally.
				prepared.Messages[lastSystemRoleInx].ProviderOptions = nil
				prepared.Messages[insertIdx].ProviderOptions = a.getCacheControlOptions()
			}

			if promptPrefix != "" {
				prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(promptPrefix)}, prepared.Messages...)
			}

			var assistantMsg message.Message
			assistantMsg, err = a.messages.Create(callContext, call.SessionID, message.CreateMessageParams{
				Role:     message.Assistant,
				Parts:    []message.ContentPart{},
				Model:    largeModel.ModelCfg.Model,
				Provider: largeModel.ModelCfg.Provider,
			})
			if err != nil {
				return callContext, prepared, err
			}
			callContext = context.WithValue(callContext, tools.MessageIDContextKey, assistantMsg.ID)
			callContext = context.WithValue(callContext, tools.SupportsImagesContextKey, largeModel.CatwalkCfg.SupportsImages)
			callContext = context.WithValue(callContext, tools.ModelNameContextKey, largeModel.CatwalkCfg.Name)
			currentAssistant = &assistantMsg
			return callContext, prepared, err
		},
		OnReasoningStart: func(id string, reasoning fantasy.ReasoningContent) error {
			currentAssistant.AppendReasoningContent(reasoning.Text)
			return sw.Update(genCtx, *currentAssistant)
		},
		OnReasoningDelta: func(id string, text string) error {
			currentAssistant.AppendReasoningContent(text)
			return sw.Update(genCtx, *currentAssistant)
		},
		OnReasoningEnd: func(id string, reasoning fantasy.ReasoningContent) error {
			// handle anthropic signature
			if anthropicData, ok := reasoning.ProviderMetadata[anthropic.Name]; ok {
				if reasoning, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok {
					currentAssistant.AppendReasoningSignature(reasoning.Signature)
				}
			}
			if googleData, ok := reasoning.ProviderMetadata[google.Name]; ok {
				if reasoning, ok := googleData.(*google.ReasoningMetadata); ok {
					currentAssistant.AppendThoughtSignature(reasoning.Signature, reasoning.ToolID)
				}
			}
			if openaiData, ok := reasoning.ProviderMetadata[openai.Name]; ok {
				if reasoning, ok := openaiData.(*openai.ResponsesReasoningMetadata); ok {
					currentAssistant.SetReasoningResponsesData(reasoning)
				}
			}
			currentAssistant.FinishThinking()
			return sw.Update(genCtx, *currentAssistant)
		},
		OnTextDelta: func(id string, text string) error {
			// Strip leading newline from initial text content. This is is
			// particularly important in non-interactive mode where leading
			// newlines are very visible.
			if len(currentAssistant.Parts) == 0 {
				text = strings.TrimPrefix(text, "\n")
			}

			currentAssistant.AppendContent(text)
			return sw.Update(genCtx, *currentAssistant)
		},
		OnToolInputStart: func(id string, toolName string) error {
			sw.Flush(ctx)
			toolCall := message.ToolCall{
				ID:               id,
				Name:             toolName,
				ProviderExecuted: false,
				Finished:         false,
			}
			currentAssistant.AddToolCall(toolCall)
			// Use parent ctx instead of genCtx to ensure the update succeeds
			// even if the request is canceled mid-stream
			return a.messages.Update(ctx, *currentAssistant)
		},
		OnRetry: func(err *fantasy.ProviderError, delay time.Duration) {
			if currentAssistant != nil {
				currentAssistant.Parts = nil
				if updateErr := a.messages.Update(ctx, *currentAssistant); updateErr != nil {
					slog.Error("Failed to clean up assistant message on retry", "error", updateErr)
				}
			}
			if err != nil {
				slog.Warn("Retrying LLM request",
					"error", err.Message,
					"status_code", err.StatusCode,
					"delay", delay,
					"session", call.SessionID,
				)
			} else {
				slog.Warn("Retrying LLM request",
					"error", "stream idle timeout",
					"delay", delay,
					"session", call.SessionID,
				)
			}
		},
		OnToolCall: func(tc fantasy.ToolCallContent) error {
			sw.Flush(ctx)
			toolCall := message.ToolCall{
				ID:               tc.ToolCallID,
				Name:             tc.ToolName,
				Input:            tc.Input,
				ProviderExecuted: false,
				Finished:         true,
			}
			currentAssistant.AddToolCall(toolCall)
			return a.messages.Update(ctx, *currentAssistant)
		},
		OnToolInputDelta: func() func(id, delta string) error {
			var pending int
			return func(id, delta string) error {
				if len(delta) == 0 {
					return nil
				}
				for _, tc := range currentAssistant.ToolCalls() {
					if tc.ID == id {
						tc.Input += delta
						currentAssistant.AddToolCall(tc)
						pending += len(delta)
						if pending >= 200 {
							pending = 0
							return a.messages.Update(ctx, *currentAssistant)
						}
						return nil
					}
				}
				return nil
			}
		}(),
		OnToolResult: func(result fantasy.ToolResultContent) error {
			toolResult := a.convertToToolResult(result)
			// Use parent ctx instead of genCtx to ensure the message is created
			// even if the request is canceled mid-stream
			_, createMsgErr := a.messages.Create(ctx, currentAssistant.SessionID, message.CreateMessageParams{
				Role: message.Tool,
				Parts: []message.ContentPart{
					toolResult,
				},
			})
			return createMsgErr
		},
		OnStepFinish: func(stepResult fantasy.StepResult) error {
			sw.Flush(ctx)
			finishReason := message.FinishReasonUnknown
			switch stepResult.FinishReason {
			case fantasy.FinishReasonLength:
				finishReason = message.FinishReasonMaxTokens
			case fantasy.FinishReasonStop:
				finishReason = message.FinishReasonEndTurn
			case fantasy.FinishReasonToolCalls:
				finishReason = message.FinishReasonToolUse
			}
			currentAssistant.AddFinish(finishReason, "", "")
			sessionLock.Lock()
			defer sessionLock.Unlock()

			updatedSession, getSessionErr := a.sessions.Get(ctx, call.SessionID)
			if getSessionErr != nil {
				return getSessionErr
			}
			a.updateSessionUsage(largeModel, &updatedSession, stepResult.Usage, a.openrouterCost(stepResult.ProviderMetadata))
			_, sessionErr := a.sessions.Save(ctx, updatedSession)
			if sessionErr != nil {
				return sessionErr
			}
			currentSession = updatedSession
			if err := a.messages.Update(genCtx, *currentAssistant); err != nil {
				return err
			}
			return nil
		},
		StopWhen: []fantasy.StopCondition{
			func(_ []fantasy.StepResult) bool {
				cw := int64(largeModel.CatwalkCfg.ContextWindow)
				// If context window is unknown (0), skip auto-summarize
				// to avoid immediately truncating custom/local models.
				if cw == 0 {
					return false
				}
				sessionLock.Lock()
				tokens := currentSession.CompletionTokens + currentSession.PromptTokens
				sessionLock.Unlock()
				threshold := a.autoSummarizeThreshold(largeModel)
				shouldTrigger := threshold > 0 && tokens >= threshold
				if shouldTrigger && !a.disableAutoSummarize && !a.circuitBreaker.isTripped(call.SessionID) {
					shouldSummarize = true
					return true
				}
				return false
			},
			func(steps []fantasy.StepResult) bool {
				return hasRepeatedToolCalls(steps, loopDetectionWindowSize, loopDetectionMaxRepeats)
			},
		},
	})

	sw.Flush(ctx)
	a.eventPromptResponded(call.SessionID, time.Since(startTime).Truncate(time.Second))

	if err != nil {
		var contextTooLargeErr *fantasy.ProviderError
		if errors.As(err, &contextTooLargeErr) && contextTooLargeErr.IsContextTooLarge() && !a.disableAutoSummarize && !a.circuitBreaker.isTripped(call.SessionID) {
			if call.autoSummarizeDepth >= maxAutoSummarizeDepth {
				return nil, fmt.Errorf("context too large after %d auto-summarize attempts", maxAutoSummarizeDepth)
			}
			slog.Warn("Context too large, triggering auto-summarize",
				"session_id", call.SessionID,
				"used_tokens", contextTooLargeErr.ContextUsedTokens,
				"max_tokens", contextTooLargeErr.ContextMaxTokens,
			)
			cancel()
			a.activeRequests.Del(call.SessionID)
			if summarizeErr := a.Summarize(ctx, call.SessionID, call.ProviderOptions); summarizeErr != nil {
				a.circuitBreaker.recordFailure(call.SessionID)
				slog.Error("Auto-summarize after context overflow failed", "error", summarizeErr)
				return nil, fmt.Errorf("context too large and auto-summarize failed: %w", summarizeErr)
			}
			a.circuitBreaker.recordSuccess(call.SessionID)
			call.Prompt = autoSummarizeContinuationPrompt
			call.autoSummarizeDepth++
			return a.Run(ctx, call)
		}

		isCancelErr := errors.Is(err, context.Canceled)
		isPermissionErr := errors.Is(err, permission.ErrorPermissionDenied)
		slog.Debug("agent.Stream returned error",
			"session_id", call.SessionID,
			"error", err,
			"error_type", fmt.Sprintf("%T", err),
			"is_cancel", isCancelErr,
			"is_permission", isPermissionErr,
		)
		if currentAssistant == nil {
			return result, err
		}
		// Ensure we finish thinking on error to close the reasoning state.
		currentAssistant.FinishThinking()
		toolCalls := currentAssistant.ToolCalls()
		// INFO: we use the parent context here because the genCtx has been cancelled.
		msgs, createErr := a.messages.List(ctx, currentAssistant.SessionID)
		if createErr != nil {
			return nil, createErr
		}
		for _, tc := range toolCalls {
			if !tc.Finished {
				tc.Finished = true
				tc.Input = "{}"
				currentAssistant.AddToolCall(tc)
				updateErr := a.messages.Update(ctx, *currentAssistant)
				if updateErr != nil {
					return nil, updateErr
				}
			}

			found := false
			for _, msg := range msgs {
				if msg.Role == message.Tool {
					for _, tr := range msg.ToolResults() {
						if tr.ToolCallID == tc.ID {
							found = true
							break
						}
					}
				}
				if found {
					break
				}
			}
			if found {
				continue
			}
			content := fmt.Sprintf("Tool execution interrupted: %v", err)
			if isCancelErr {
				content = "Error: user cancelled assistant tool calling"
			} else if isPermissionErr {
				content = "User denied permission"
			}
			toolResult := message.ToolResult{
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Content:    content,
				IsError:    true,
			}
			_, createErr = a.messages.Create(ctx, currentAssistant.SessionID, message.CreateMessageParams{
				Role: message.Tool,
				Parts: []message.ContentPart{
					toolResult,
				},
			})
			if createErr != nil {
				return nil, createErr
			}
		}
		var fantasyErr *fantasy.Error
		var providerErr *fantasy.ProviderError
		const defaultTitle = "Provider Error"
		linkStyle := lipgloss.NewStyle().Foreground(charmtone.Guac).Underline(true)
		if isCancelErr {
			currentAssistant.AddFinish(message.FinishReasonCanceled, "User canceled request", "")
		} else if isPermissionErr {
			currentAssistant.AddFinish(message.FinishReasonPermissionDenied, "User denied permission", "")
		} else if errors.Is(err, hyper.ErrUnauthorized) {
			currentAssistant.AddFinish(message.FinishReasonError, "Unauthorized", `Please re-authenticate with Hyper. You can also run "crush auth" to re-authenticate.`)
			if a.notify != nil {
				a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
					SessionID:    call.SessionID,
					SessionTitle: currentSession.Title,
					Type:         notify.TypeReAuthenticate,
					ProviderID:   largeModel.ModelCfg.Provider,
				})
			}
		} else if errors.Is(err, hyper.ErrNoCredits) {
			url := hyper.BaseURL()
			link := linkStyle.Hyperlink(url, "id=hyper").Render(url)
			currentAssistant.AddFinish(message.FinishReasonError, "No credits", "You're out of credits. Add more at "+link)
		} else if errors.As(err, &providerErr) {
			if providerErr.Message == "The requested model is not supported." {
				url := "https://github.com/settings/copilot/features"
				link := linkStyle.Hyperlink(url, "id=copilot").Render(url)
				currentAssistant.AddFinish(
					message.FinishReasonError,
					"Copilot model not enabled",
					fmt.Sprintf("%q is not enabled in Copilot. Go to the following page to enable it. Then, wait 5 minutes before trying again. %s", largeModel.CatwalkCfg.Name, link),
				)
			} else {
				currentAssistant.AddFinish(message.FinishReasonError, cmp.Or(stringext.Capitalize(providerErr.Title), defaultTitle), providerErr.Message)
			}
		} else if errors.As(err, &fantasyErr) {
			currentAssistant.AddFinish(message.FinishReasonError, cmp.Or(stringext.Capitalize(fantasyErr.Title), defaultTitle), fantasyErr.Message)
		} else {
			currentAssistant.AddFinish(message.FinishReasonError, defaultTitle, err.Error())
		}
		// Note: we use the parent context here because the genCtx has been
		// cancelled.
		updateErr := a.messages.Update(ctx, *currentAssistant)
		if updateErr != nil {
			return nil, updateErr
		}
		return nil, err
	}

	// Send notification that agent has finished its turn (skip for
	// nested/non-interactive sessions).
	if !call.NonInteractive && a.notify != nil {
		a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			SessionID:    call.SessionID,
			SessionTitle: currentSession.Title,
			Type:         notify.TypeAgentFinished,
		})
	}

	if shouldSummarize {
		if call.autoSummarizeDepth >= maxAutoSummarizeDepth {
			slog.Warn("Skipping auto-summarize, max depth reached",
				"session_id", call.SessionID,
				"depth", call.autoSummarizeDepth,
			)
		} else {
			a.activeRequests.Del(call.SessionID)
			if summarizeErr := a.Summarize(ctx, call.SessionID, call.ProviderOptions); summarizeErr != nil {
				a.circuitBreaker.recordFailure(call.SessionID)
				slog.Warn("Auto-summarize failed",
					"session_id", call.SessionID,
					"error", summarizeErr,
					"tripped", a.circuitBreaker.isTripped(call.SessionID),
				)
				return nil, summarizeErr
			}
			a.circuitBreaker.recordSuccess(call.SessionID)
			// If the agent wasn't done, continue with fresh context.
			if len(currentAssistant.ToolCalls()) > 0 {
				call.Prompt = autoSummarizeContinuationPrompt
				call.autoSummarizeDepth++
				return a.Run(ctx, call)
			}
		}
	}
	// Release active request.
	a.activeRequests.Del(call.SessionID)
	cancel()

	// Drain the queue: if there are queued messages, process the next one.
	// Skip if the run was cancelled — the user wanted to stop, not continue
	// with queued messages.
	if errors.Is(err, context.Canceled) {
		a.messageQueue.Del(call.SessionID)
	} else if drainResult, drainErr := a.drainQueue(ctx, call.SessionID); drainResult != nil || drainErr != nil {
		return drainResult, drainErr
	}

	return result, err
}

// drainQueue processes any queued messages for the given session.
// It takes the first queued item and runs it; the rest are re-queued
// for the recursive Run to merge. Returns (nil, nil) if the queue
// was empty.
func (a *sessionAgent) drainQueue(ctx context.Context, sessionID string) (*fantasy.AgentResult, error) {
	queued, ok := a.messageQueue.Take(sessionID)
	if !ok || len(queued) == 0 {
		return nil, nil
	}
	slog.Debug("Draining queued messages",
		"session_id", sessionID,
		"count", len(queued),
	)
	next := queued[0]
	if len(queued) > 1 {
		remaining := queued[1:]
		a.messageQueue.Update(sessionID, func(existing []SessionAgentCall) []SessionAgentCall {
			return append(remaining, existing...)
		})
	}
	return a.Run(ctx, next)
}

func (a *sessionAgent) Summarize(ctx context.Context, sessionID string, opts fantasy.ProviderOptions) error {
	slog.Debug("Summarize() starting",
		"session_id", sessionID,
		"is_busy", a.IsSessionBusy(sessionID),
		"queue_size", a.QueuedPrompts(sessionID),
	)
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	genCtx, cancel := context.WithCancel(ctx)
	a.activeRequests.Set(sessionID, cancel)

	// Release the active request and drain the queue when done.
	// We avoid defer for activeRequests.Del so we can drain the
	// queue after the session is no longer busy.
	defer func() {
		cancel()
		a.activeRequests.Del(sessionID)
		a.drainQueue(ctx, sessionID)
	}()

	// Copy mutable fields under lock to avoid races with SetModels.
	summaryModel := a.SummaryModel()
	systemPromptPrefix := a.systemPromptPrefix.Get()

	currentSession, err := a.sessions.Get(genCtx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	msgs, err := a.getSessionMessages(genCtx, currentSession)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		// Nothing to summarize.
		return nil
	}

	go a.saveTranscriptFromMessages(sessionID, msgs)

	aiMsgs, _ := a.preparePrompt(msgs)

	// Microcompact pre-pass: aggressively clear old tool results before
	// sending to the summary model. This reduces the token cost of the
	// summarize API call itself. The summary model only needs recent
	// tool results for context — older ones are already captured in the
	// conversation flow.
	aiMsgs = clearToolResultsKeeping(aiMsgs, keepRecentToolResultsAfterIdle)

	// Strip binary content (images, documents) before sending to the
	// summary model. Images waste tokens in the summary call and are not
	// needed for generating a text summary.
	aiMsgs = stripBinaryFromMessages(aiMsgs)

	agent := fantasy.NewAgent(summaryModel.Model,
		fantasy.WithSystemPrompt(string(summaryPrompt)),
		fantasy.WithUserAgent(userAgent),
	)
	summaryMessage, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:             message.Assistant,
		Model:            summaryModel.ModelCfg.Model,
		Provider:         summaryModel.ModelCfg.Provider,
		IsSummaryMessage: true,
	})
	if err != nil {
		return err
	}

	summaryPromptText := buildSummaryPrompt(a.dataDir, sessionID, currentSession.Todos, "")

	summarySW := newStreamingWriter(a.messages)
	resp, err := agent.Stream(genCtx, fantasy.AgentStreamCall{
		Prompt:            summaryPromptText,
		Messages:          aiMsgs,
		ProviderOptions:   opts,
		StreamIdleTimeout: streamIdleTimeout,
		PrepareStep: func(callContext context.Context, options fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
			prepared.Messages = options.Messages
			if systemPromptPrefix != "" {
				prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(systemPromptPrefix)}, prepared.Messages...)
			}
			return callContext, prepared, nil
		},
		OnReasoningDelta: func(id string, text string) error {
			summaryMessage.AppendReasoningContent(text)
			return summarySW.Update(genCtx, summaryMessage)
		},
		OnReasoningEnd: func(id string, reasoning fantasy.ReasoningContent) error {
			// Handle anthropic signature.
			if anthropicData, ok := reasoning.ProviderMetadata["anthropic"]; ok {
				if signature, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok && signature.Signature != "" {
					summaryMessage.AppendReasoningSignature(signature.Signature)
				}
			}
			summaryMessage.FinishThinking()
			return summarySW.Update(genCtx, summaryMessage)
		},
		OnTextDelta: func(id, text string) error {
			summaryMessage.AppendContent(text)
			return summarySW.Update(genCtx, summaryMessage)
		},
	})
	summarySW.Flush(ctx)
	if err != nil {
		isCancelErr := errors.Is(err, context.Canceled)
		if isCancelErr {
			// User cancelled summarize we need to remove the summary message.
			deleteErr := a.messages.Delete(ctx, summaryMessage.ID)
			return deleteErr
		}
		// Mark the message as finished so the spinner stops.
		summaryMessage.AddFinish(message.FinishReasonError, "", err.Error())
		_ = a.messages.Update(ctx, summaryMessage)
		return err
	}

	summaryMessage.AddFinish(message.FinishReasonEndTurn, "", "")

	// Strip the <analysis> scratchpad block from the summary. The model
	// uses it as a reasoning step but the content wastes context tokens.
	summaryMessage.StripTextContent(stripAnalysisBlock)

	err = a.messages.Update(genCtx, summaryMessage)
	if err != nil {
		return err
	}

	if err := a.extractAndSaveKeyFacts(sessionID, summaryMessage.Content().Text); err != nil {
		slog.Warn("failed to save key facts", "error", err)
	}

	var openrouterCost *float64
	for _, step := range resp.Steps {
		stepCost := a.openrouterCost(step.ProviderMetadata)
		if stepCost != nil {
			newCost := *stepCost
			if openrouterCost != nil {
				newCost += *openrouterCost
			}
			openrouterCost = &newCost
		}
	}

	a.updateSessionUsage(summaryModel, &currentSession, resp.TotalUsage, openrouterCost)

	// Just in case, get just the last usage info.
	usage := resp.Response.Usage
	currentSession.SummaryMessageID = summaryMessage.ID
	currentSession.CompletionTokens = usage.OutputTokens
	currentSession.PromptTokens = 0
	_, err = a.sessions.Save(ctx, currentSession)
	if err != nil {
		return err
	}

	// Update the session title using only the post-summary messages so the
	// title model sees the condensed conversation instead of the full history.
	postSummaryMsgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		slog.Error("Failed to load post-summary messages for title generation", "error", err)
		postSummaryMsgs = nil
	}
	go a.generateTitle(ctx, sessionID, postSummaryMsgs, "")

	// Post-summarize cleanup: invalidate caches that hold data from the
	// now-truncated conversation to prevent stale state.
	tools.ResetCache()
	a.circuitBreaker.recordSuccess(sessionID)

	slog.Debug("Summarize() completed",
		"session_id", sessionID,
		"queue_size", a.QueuedPrompts(sessionID),
	)

	return nil
}

func (a *sessionAgent) getCacheControlOptions() fantasy.ProviderOptions {
	if t, _ := strconv.ParseBool(os.Getenv("CRUSH_DISABLE_ANTHROPIC_CACHE")); t {
		return fantasy.ProviderOptions{}
	}
	return fantasy.ProviderOptions{
		anthropic.Name: &anthropic.ProviderCacheControlOptions{
			CacheControl: anthropic.CacheControl{Type: "ephemeral"},
		},
		bedrock.Name: &anthropic.ProviderCacheControlOptions{
			CacheControl: anthropic.CacheControl{Type: "ephemeral"},
		},
		vercel.Name: &anthropic.ProviderCacheControlOptions{
			CacheControl: anthropic.CacheControl{Type: "ephemeral"},
		},
	}
}

func (a *sessionAgent) createUserMessage(ctx context.Context, call SessionAgentCall) (message.Message, error) {
	parts := []message.ContentPart{message.TextContent{Text: call.Prompt}}
	var attachmentParts []message.ContentPart
	for _, attachment := range call.Attachments {
		attachmentParts = append(attachmentParts, message.BinaryContent{Path: attachment.FilePath, MIMEType: attachment.MimeType, Data: attachment.Content})
	}
	parts = append(parts, attachmentParts...)
	msg, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
		Role:      message.User,
		Parts:     parts,
		AgentName: a.agentName,
	})
	if err != nil {
		return message.Message{}, fmt.Errorf("failed to create user message: %w", err)
	}
	return msg, nil
}

func (a *sessionAgent) preparePrompt(msgs []message.Message, attachments ...message.Attachment) ([]fantasy.Message, []fantasy.FilePart) {
	var history []fantasy.Message
	hasSummary := false
	for _, msg := range msgs {
		if msg.IsSummaryMessage {
			hasSummary = true
			break
		}
	}
	if !a.isSubAgent {
		history = append(history, fantasy.NewUserMessage(
			fmt.Sprintf("<system_reminder>%s</system_reminder>",
				`This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware.
If you are working on tasks that would benefit from a todo list please use the "todos" tool to create one.
If not, please feel free to ignore. Again do not mention this message to the user.
IMPORTANT: This reminder is in English but you MUST continue responding in the same language the user has been using.`,
			),
		))
		if hasSummary {
			history = append(history, fantasy.NewUserMessage(
				fmt.Sprintf("<system_reminder>%s</system_reminder>",
					`This session was summarized. If you need specific details from before the summary (commands, code, file paths, errors, decisions), use the "memory_search" tool to search the full transcript instead of guessing.
IMPORTANT: This reminder is in English but you MUST continue responding in the same language the user has been using.`,
				),
			))
			if len(msgs) > 0 && a.dataDir != "" {
				if facts := loadKeyFacts(a.dataDir, msgs[0].SessionID); facts != "" {
					history = append(history, fantasy.NewUserMessage(
						fmt.Sprintf("<key_facts>\n%s\n</key_facts>", facts),
					))
				}
			}
			if len(msgs) > 0 {
				if recentFiles := loadRecentlyReadFiles(context.Background(), a.fileTracker, msgs[0].SessionID); recentFiles != "" {
					history = append(history, fantasy.NewUserMessage(recentFiles))
				}
			}
		}
		if wasInterrupted(msgs) {
			history = append(history, fantasy.NewUserMessage(
				fmt.Sprintf("<system_reminder>%s</system_reminder>",
					`This session was previously interrupted mid-task. The last response was cut short. Continue from where you left off — review the conversation above and resume the task without re-doing completed work.`,
				),
			))
		}
	}
	for _, m := range msgs {
		if len(m.Parts) == 0 {
			continue
		}
		// Assistant message without content or tool calls (cancelled before it
		// returned anything).
		if m.Role == message.Assistant && len(m.ToolCalls()) == 0 && m.Content().Text == "" && m.ReasoningContent().String() == "" {
			continue
		}
		history = append(history, m.ToAIMessage()...)
	}

	history = repairOrphanedToolCalls(history)

	var files []fantasy.FilePart
	for _, attachment := range attachments {
		if attachment.IsText() {
			continue
		}
		files = append(files, fantasy.FilePart{
			Filename:  attachment.FileName,
			Data:      attachment.Content,
			MediaType: attachment.MimeType,
		})
	}

	return history, files
}

// repairOrphanedToolCalls scans the message history for assistant tool_use
// blocks that have no corresponding tool_result and injects synthetic error
// results so the provider API does not reject the request with a 400.
// This can happen when the app exits while a tool (e.g. bash) is waiting
// for input — the tool_use is persisted but the tool_result is not.
func repairOrphanedToolCalls(history []fantasy.Message) []fantasy.Message {
	// First pass: find which assistant messages have orphaned tool calls.
	type orphanInfo struct {
		// insertAfter is the index in history after which we inject the
		// synthetic tool result message (after the last consecutive tool
		// message following the assistant, or right after the assistant
		// if there are none).
		insertAfter int
		ids         []string
	}
	var orphans []orphanInfo

	for i, msg := range history {
		if msg.Role != fantasy.MessageRoleAssistant {
			continue
		}

		// Collect all tool call IDs from this assistant message.
		var pendingIDs []string
		for _, part := range msg.Content {
			if tc, ok := fantasy.AsContentType[fantasy.ToolCallPart](part); ok {
				pendingIDs = append(pendingIDs, tc.ToolCallID)
			}
		}
		if len(pendingIDs) == 0 {
			continue
		}

		// Look ahead for matching tool results in subsequent tool messages.
		lastToolIdx := i
		for j := i + 1; j < len(history); j++ {
			next := history[j]
			if next.Role != fantasy.MessageRoleTool {
				break
			}
			lastToolIdx = j
			for _, part := range next.Content {
				if tr, ok := fantasy.AsContentType[fantasy.ToolResultPart](part); ok {
					pendingIDs = removeFromSlice(pendingIDs, tr.ToolCallID)
				}
			}
		}

		if len(pendingIDs) > 0 {
			orphans = append(orphans, orphanInfo{
				insertAfter: lastToolIdx,
				ids:         pendingIDs,
			})
		}
	}

	if len(orphans) == 0 {
		return history
	}

	// Second pass: rebuild history with synthetic tool results injected.
	insertAt := make(map[int][]string, len(orphans))
	for _, o := range orphans {
		insertAt[o.insertAfter] = append(insertAt[o.insertAfter], o.ids...)
	}

	var repaired []fantasy.Message
	for i, msg := range history {
		repaired = append(repaired, msg)
		if ids, ok := insertAt[i]; ok {
			var parts []fantasy.MessagePart
			for _, id := range ids {
				parts = append(parts, fantasy.ToolResultPart{
					ToolCallID: id,
					Output: fantasy.ToolResultOutputContentError{
						Error: errors.New("no response received (session was interrupted)"),
					},
				})
			}
			repaired = append(repaired, fantasy.Message{
				Role:    fantasy.MessageRoleTool,
				Content: parts,
			})
		}
	}
	return repaired
}

// removeFromSlice returns a new slice with the first occurrence of val removed.
func removeFromSlice(s []string, val string) []string {
	result := make([]string, 0, len(s))
	removed := false
	for _, v := range s {
		if !removed && v == val {
			removed = true
			continue
		}
		result = append(result, v)
	}
	return result
}

func (a *sessionAgent) getSessionMessages(ctx context.Context, session session.Session) ([]message.Message, error) {
	msgs, err := a.messages.List(ctx, session.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	if session.SummaryMessageID != "" {
		summaryMsgIndex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
				summaryMsgIndex = i
				break
			}
		}
		if summaryMsgIndex != -1 {
			msgs = msgs[summaryMsgIndex:]
			// Copy the summary message before changing its role so we
			// don't mutate the original slice element. Wrap the summary
			// text with context preamble so the model understands this
			// is a continuation of a previous conversation.
			summaryMsg := msgs[0]
			summaryMsg.Role = message.User
			summaryMsg.StripTextContent(wrapSummaryMessage)
			msgs[0] = summaryMsg
		}
	}
	return msgs, nil
}

// generateTitle generates or updates a session title based on the conversation.
func (a *sessionAgent) generateTitle(ctx context.Context, sessionID string, msgs []message.Message, userPrompt string) {
	if userPrompt == "" && len(msgs) == 0 {
		return
	}

	smallModel := a.smallModel.Get()
	largeModel := a.largeModel.Get()
	systemPromptPrefix := a.systemPromptPrefix.Get()

	var maxOutputTokens int64 = 40
	if smallModel.CatwalkCfg.CanReason {
		maxOutputTokens = smallModel.CatwalkCfg.DefaultMaxTokens
	}

	newAgent := func(m fantasy.LanguageModel, p []byte, tok int64) fantasy.Agent {
		return fantasy.NewAgent(m,
			fantasy.WithSystemPrompt(string(p)+"\n /no_think"),
			fantasy.WithMaxOutputTokens(tok),
			fantasy.WithUserAgent(userAgent),
		)
	}

	// Reuse the same prompt preparation as summarization so the title model
	// sees the full conversation context (tool calls, results, etc.).
	aiMsgs, _ := a.preparePrompt(msgs)

	titleUserPrompt := "Generate a concise title for this conversation.\n\n<think>\n\n</think>"
	if len(msgs) == 0 {
		titleUserPrompt = fmt.Sprintf("Generate a concise title for the following message:\n\n%s\n\n<think>\n\n</think>", truncateString(userPrompt, 500))
		aiMsgs = nil
	}

	streamCall := fantasy.AgentStreamCall{
		Prompt:   titleUserPrompt,
		Messages: aiMsgs,
		PrepareStep: func(callCtx context.Context, opts fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
			prepared.Messages = opts.Messages
			if systemPromptPrefix != "" {
				prepared.Messages = append([]fantasy.Message{
					fantasy.NewSystemMessage(systemPromptPrefix),
				}, prepared.Messages...)
			}
			return callCtx, prepared, nil
		},
	}

	// Use the small model to generate the title.
	model := smallModel
	agent := newAgent(model.Model, titlePrompt, maxOutputTokens)
	resp, err := agent.Stream(ctx, streamCall)
	if err == nil {
		// We successfully generated a title with the small model.
		slog.Debug("Generated title with small model")
	} else {
		// It didn't work. Let's try with the big model.
		slog.Error("Error generating title with small model; trying big model", "err", err)
		model = largeModel
		agent = newAgent(model.Model, titlePrompt, maxOutputTokens)
		resp, err = agent.Stream(ctx, streamCall)
		if err == nil {
			slog.Debug("Generated title with large model")
		} else {
			// Welp, the large model didn't work either. Use the default
			// session name and return.
			slog.Error("Error generating title with large model", "err", err)
			saveErr := a.sessions.Rename(ctx, sessionID, DefaultSessionName)
			if saveErr != nil {
				slog.Error("Failed to save session title", "error", saveErr)
			}
			return
		}
	}

	if resp == nil {
		// Actually, we didn't get a response so we can't. Use the default
		// session name and return.
		slog.Error("Response is nil; can't generate title")
		saveErr := a.sessions.Rename(ctx, sessionID, DefaultSessionName)
		if saveErr != nil {
			slog.Error("Failed to save session title", "error", saveErr)
		}
		return
	}

	// Clean up title.
	var title string
	title = strings.ReplaceAll(resp.Response.Content.Text(), "\n", "\n")

	// Remove thinking tags if present.
	title = thinkTagRegex.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)

	// Parse two-line response: line 1 = title, line 2 = short title.
	var shortTitle string
	if parts := strings.SplitN(title, "\n", 2); len(parts) == 2 {
		title = strings.TrimSpace(parts[0])
		shortTitle = strings.TrimSpace(parts[1])
	}

	title = cmp.Or(title, DefaultSessionName)

	// Calculate usage and cost.
	var openrouterCost *float64
	for _, step := range resp.Steps {
		stepCost := a.openrouterCost(step.ProviderMetadata)
		if stepCost != nil {
			newCost := *stepCost
			if openrouterCost != nil {
				newCost += *openrouterCost
			}
			openrouterCost = &newCost
		}
	}

	modelConfig := model.CatwalkCfg
	cost := modelConfig.CostPer1MInCached/1e6*float64(resp.TotalUsage.CacheCreationTokens) +
		modelConfig.CostPer1MOutCached/1e6*float64(resp.TotalUsage.CacheReadTokens) +
		modelConfig.CostPer1MIn/1e6*float64(resp.TotalUsage.InputTokens) +
		modelConfig.CostPer1MOut/1e6*float64(resp.TotalUsage.OutputTokens)

	// Use override cost if available (e.g., from OpenRouter).
	if openrouterCost != nil {
		cost = *openrouterCost
	}

	// Only accumulate cost from title generation; do not add prompt/completion
	// tokens because they reflect the title model's input (the full conversation
	// history) and would inflate the session's context-usage counters, undoing
	// any token reset performed by Summarize().
	saveErr := a.sessions.UpdateTitleAndUsage(ctx, sessionID, title, shortTitle, 0, 0, cost)
	if saveErr != nil {
		slog.Error("Failed to save session title and usage", "error", saveErr)
		return
	}
}

// truncateString returns s truncated to maxLen runes with "..." appended if
// it was longer.
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func (a *sessionAgent) openrouterCost(metadata fantasy.ProviderMetadata) *float64 {
	openrouterMetadata, ok := metadata[openrouter.Name]
	if !ok {
		return nil
	}

	opts, ok := openrouterMetadata.(*openrouter.ProviderMetadata)
	if !ok {
		return nil
	}
	return &opts.Usage.Cost
}

func (a *sessionAgent) updateSessionUsage(model Model, session *session.Session, usage fantasy.Usage, overrideCost *float64) {
	modelConfig := model.CatwalkCfg
	cost := modelConfig.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		modelConfig.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		modelConfig.CostPer1MIn/1e6*float64(usage.InputTokens) +
		modelConfig.CostPer1MOut/1e6*float64(usage.OutputTokens)

	a.eventTokensUsed(session.ID, model, usage, cost)

	if overrideCost != nil {
		session.Cost += *overrideCost
	} else {
		session.Cost += cost
	}

	session.CompletionTokens = usage.OutputTokens
	session.PromptTokens = usage.InputTokens + usage.CacheReadTokens
}

func (a *sessionAgent) Cancel(sessionID string) {
	// Cancel regular requests. Don't use Take() here - we need the entry to
	// remain in activeRequests so IsBusy() returns true until the goroutine
	// fully completes (including error handling that may access the DB).
	// The defer in processRequest will clean up the entry.
	if cancel, ok := a.activeRequests.Get(sessionID); ok && cancel != nil {
		slog.Debug("Request cancellation initiated", "session_id", sessionID)
		cancel()
	}

	if a.QueuedPrompts(sessionID) > 0 {
		slog.Debug("Clearing queued prompts", "session_id", sessionID)
		a.messageQueue.Del(sessionID)
	}
}

func (a *sessionAgent) ClearQueue(sessionID string) {
	if a.QueuedPrompts(sessionID) > 0 {
		slog.Debug("Clearing queued prompts", "session_id", sessionID)
		a.messageQueue.Del(sessionID)
	}
}

func (a *sessionAgent) CancelAll() {
	if !a.IsBusy() {
		return
	}
	for key := range a.activeRequests.Seq2() {
		a.Cancel(key) // key is sessionID
	}

	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for a.IsBusy() {
		select {
		case <-timeout:
			return
		case <-ticker.C:
		}
	}
}

func (a *sessionAgent) IsBusy() bool {
	var busy bool
	for cancelFunc := range a.activeRequests.Seq() {
		if cancelFunc != nil {
			busy = true
			break
		}
	}
	return busy
}

func (a *sessionAgent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Get(sessionID)
	return busy
}

func (a *sessionAgent) QueuedPrompts(sessionID string) int {
	l, ok := a.messageQueue.Get(sessionID)
	if !ok {
		return 0
	}
	return len(l)
}

func (a *sessionAgent) QueuedPromptsList(sessionID string) []string {
	l, ok := a.messageQueue.Get(sessionID)
	if !ok {
		return nil
	}
	prompts := make([]string, len(l))
	for i, call := range l {
		prompts[i] = call.Prompt
	}
	return prompts
}

func (a *sessionAgent) SetModels(large Model, small Model, summary *Model) {
	a.largeModel.Set(large)
	a.smallModel.Set(small)
	a.summaryModelMu.Lock()
	a.summaryModel = summary
	a.summaryModelMu.Unlock()
}

// SummaryModel returns the summary model if configured, otherwise falls back
// to the large model.
func (a *sessionAgent) SummaryModel() Model {
	a.summaryModelMu.RLock()
	m := a.summaryModel
	a.summaryModelMu.RUnlock()
	if m != nil {
		return *m
	}
	return a.largeModel.Get()
}

// autoSummarizeThreshold returns the token count at which auto-summarization
// should be triggered. If maxTokensToSummarize is explicitly configured, it
// takes precedence. Otherwise, the threshold is calculated as:
//
//	effectiveContextWindow - autoSummarizeBufferTokens
//
// where effectiveContextWindow = contextWindow - min(maxOutputTokens, maxOutputTokensReserve).
// This reserves headroom for both the model's output and a safety buffer,
// matching the approach used by claude-code's autocompact threshold.
// Returns 0 if auto-summarization should not be triggered.
func (a *sessionAgent) autoSummarizeThreshold(model Model) int64 {
	if a.maxTokensToSummarize > 0 {
		return a.maxTokensToSummarize
	}
	cw := int64(model.CatwalkCfg.ContextWindow)
	if cw <= 0 {
		return 0
	}
	maxOut := int64(model.CatwalkCfg.DefaultMaxTokens)
	if maxOut > maxOutputTokensReserve {
		maxOut = maxOutputTokensReserve
	}
	effective := cw - maxOut
	threshold := effective - autoSummarizeBufferTokens
	if threshold <= 0 {
		return 0
	}
	return threshold
}

func (a *sessionAgent) SetTools(tools []fantasy.AgentTool) {
	a.tools.SetSlice(tools)
}

func (a *sessionAgent) SetSystemPrompt(systemPrompt string) {
	a.systemPrompt.Set(systemPrompt)
}

func (a *sessionAgent) Model() Model {
	return a.largeModel.Get()
}

func (a *sessionAgent) SmallModel() Model {
	return a.smallModel.Get()
}

// convertToToolResult converts a fantasy tool result to a message tool result.
func (a *sessionAgent) convertToToolResult(result fantasy.ToolResultContent) message.ToolResult {
	baseResult := message.ToolResult{
		ToolCallID: result.ToolCallID,
		Name:       result.ToolName,
		Metadata:   result.ClientMetadata,
	}

	switch result.Result.GetType() {
	case fantasy.ToolResultContentTypeText:
		if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result.Result); ok {
			baseResult.Content = r.Text
		}
	case fantasy.ToolResultContentTypeError:
		if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result.Result); ok {
			baseResult.Content = r.Error.Error()
			baseResult.IsError = true
		}
	case fantasy.ToolResultContentTypeMedia:
		if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result.Result); ok {
			content := r.Text
			if content == "" {
				content = fmt.Sprintf("Loaded %s content", r.MediaType)
			}
			baseResult.Content = content
			baseResult.Data = r.Data
			baseResult.MIMEType = r.MediaType
		}
	}

	return baseResult
}

// workaroundProviderMediaLimitations converts media content in tool results to
// user messages for providers that don't natively support images in tool results.
//
// Problem: OpenAI, Google, OpenRouter, and other OpenAI-compatible providers
// don't support sending images/media in tool result messages - they only accept
// text in tool results. However, they DO support images in user messages.
//
// If we send media in tool results to these providers, the API returns an error.
//
// Solution: For these providers, we:
//  1. Replace the media in the tool result with a text placeholder
//  2. Inject a user message immediately after with the image as a file attachment
//  3. This maintains the tool execution flow while working around API limitations
//
// Anthropic and Bedrock support images natively in tool results, so we skip
// this workaround for them.
//
// Example transformation:
//
//	BEFORE: [tool result: image data]
//	AFTER:  [tool result: "Image loaded - see attached"], [user: image attachment]
func (a *sessionAgent) workaroundProviderMediaLimitations(messages []fantasy.Message, largeModel Model) []fantasy.Message {
	providerSupportsMedia := largeModel.ModelCfg.Provider == string(catwalk.InferenceProviderAnthropic) ||
		largeModel.ModelCfg.Provider == string(catwalk.InferenceProviderBedrock)

	if providerSupportsMedia {
		return messages
	}

	convertedMessages := make([]fantasy.Message, 0, len(messages))

	for _, msg := range messages {
		if msg.Role != fantasy.MessageRoleTool {
			convertedMessages = append(convertedMessages, msg)
			continue
		}

		textParts := make([]fantasy.MessagePart, 0, len(msg.Content))
		var mediaFiles []fantasy.FilePart

		for _, part := range msg.Content {
			toolResult, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
			if !ok {
				textParts = append(textParts, part)
				continue
			}

			if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](toolResult.Output); ok {
				decoded, err := base64.StdEncoding.DecodeString(media.Data)
				if err != nil {
					slog.Warn("Failed to decode media data", "error", err)
					textParts = append(textParts, part)
					continue
				}

				mediaFiles = append(mediaFiles, fantasy.FilePart{
					Data:      decoded,
					MediaType: media.MediaType,
					Filename:  fmt.Sprintf("tool-result-%s", toolResult.ToolCallID),
				})

				textParts = append(textParts, fantasy.ToolResultPart{
					ToolCallID: toolResult.ToolCallID,
					Output: fantasy.ToolResultOutputContentText{
						Text: "[Image/media content loaded - see attached file]",
					},
					ProviderOptions: toolResult.ProviderOptions,
				})
			} else {
				textParts = append(textParts, part)
			}
		}

		convertedMessages = append(convertedMessages, fantasy.Message{
			Role:    fantasy.MessageRoleTool,
			Content: textParts,
		})

		if len(mediaFiles) > 0 {
			convertedMessages = append(convertedMessages, fantasy.NewUserMessage(
				"Here is the media content from the tool result:",
				mediaFiles...,
			))
		}
	}

	return convertedMessages
}

// truncateLargeToolResults is a safety net that truncates oversized tool
// result text before it is sent to the LLM. This runs in PrepareStep so it
// only affects what the LLM sees — the original data in the DB is untouched.
func truncateLargeToolResults(messages []fantasy.Message) []fantasy.Message {
	for i, msg := range messages {
		if msg.Role != fantasy.MessageRoleTool {
			continue
		}
		for j, part := range msg.Content {
			tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
			if !ok {
				continue
			}
			text, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](tr.Output)
			if !ok || len(text.Text) <= maxToolResultSize {
				continue
			}
			tr.Output = fantasy.ToolResultOutputContentText{
				Text: tools.TruncateString(text.Text, maxToolResultSize),
			}
			messages[i].Content[j] = tr
		}
	}
	return messages
}

// buildSummaryPrompt constructs the prompt text for session summarization.
// If customInstructions is non-empty, it's appended to guide what the
// summary should focus on or preserve.
func buildSummaryPrompt(dataDir string, sessionID string, todos []session.Todo, customInstructions string) string {
	var sb strings.Builder
	sb.WriteString("Provide a detailed summary of our conversation above.")

	transcriptPath := TranscriptPath(dataDir, sessionID)
	sb.WriteString("\n\n## Session Transcript\n\n")
	sb.WriteString(fmt.Sprintf("The full conversation transcript has been saved to: `%s`\n", transcriptPath))
	sb.WriteString("The resuming assistant can use the `memory_search` tool to search this transcript for specific details from the conversation.\n")

	if len(todos) > 0 {
		sb.WriteString("\n\n## Current Todo List\n\n")
		for _, t := range todos {
			fmt.Fprintf(&sb, "- [%s] %s\n", t.Status, t.Content)
		}
		sb.WriteString("\nInclude these tasks and their statuses in your summary. ")
		sb.WriteString("Instruct the resuming assistant to use the `todos` tool to continue tracking progress on these tasks.")
	}

	if customInstructions != "" {
		sb.WriteString("\n\n## Custom Instructions\n\n")
		sb.WriteString("The user has provided the following instructions for this summary. Follow them carefully:\n\n")
		sb.WriteString(customInstructions)
	}

	return sb.String()
}

// wrapSummaryMessage wraps summary text with a preamble that tells the model
// this is a continuation of a previous conversation. This matches the pattern
// from claude-code's getCompactUserSummaryMessage.
func wrapSummaryMessage(summary string) string {
	return "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n" + summary
}

// stripBinaryFromMessages creates copies of messages with binary content
// (images, documents) replaced by text markers. This reduces the token cost
// of the summary API call since images are not needed for generating a
// conversation summary. Handles both user-attached files (FilePart) and
// media in tool results (ToolResultOutputContentMedia).
func stripBinaryFromMessages(messages []fantasy.Message) []fantasy.Message {
	result := make([]fantasy.Message, len(messages))
	for i, msg := range messages {
		switch msg.Role {
		case fantasy.MessageRoleUser:
			hasBinary := false
			for _, part := range msg.Content {
				if _, ok := fantasy.AsMessagePart[fantasy.FilePart](part); ok {
					hasBinary = true
					break
				}
			}
			if !hasBinary {
				result[i] = msg
				continue
			}
			newContent := make([]fantasy.MessagePart, 0, len(msg.Content))
			for _, part := range msg.Content {
				if fp, ok := fantasy.AsMessagePart[fantasy.FilePart](part); ok {
					newContent = append(newContent, fantasy.TextPart{
						Text: fmt.Sprintf("[%s file: %s]", fp.MediaType, fp.Filename),
					})
				} else {
					newContent = append(newContent, part)
				}
			}
			result[i] = fantasy.Message{
				Role:            msg.Role,
				Content:         newContent,
				ProviderOptions: msg.ProviderOptions,
			}

		case fantasy.MessageRoleTool:
			hasMedia := false
			for _, part := range msg.Content {
				if tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part); ok {
					if _, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](tr.Output); ok {
						hasMedia = true
						break
					}
				}
			}
			if !hasMedia {
				result[i] = msg
				continue
			}
			newContent := make([]fantasy.MessagePart, 0, len(msg.Content))
			for _, part := range msg.Content {
				tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
				if !ok {
					newContent = append(newContent, part)
					continue
				}
				if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](tr.Output); ok {
					tr.Output = fantasy.ToolResultOutputContentText{
						Text: fmt.Sprintf("[%s media content]", media.MediaType),
					}
				}
				newContent = append(newContent, tr)
			}
			result[i] = fantasy.Message{
				Role:            msg.Role,
				Content:         newContent,
				ProviderOptions: msg.ProviderOptions,
			}

		default:
			result[i] = msg
		}
	}
	return result
}

// serializeTranscript converts a slice of messages to a searchable markdown
// transcript format.
func serializeTranscript(msgs []message.Message) string {
	var sb strings.Builder
	sb.WriteString("# Session Transcript\n\n")

	for _, msg := range msgs {
		roleHeader := "Message"
		switch msg.Role {
		case message.User:
			roleHeader = "User"
		case message.Assistant:
			roleHeader = "Assistant"
		case message.Tool:
			roleHeader = "Tool Results"
		}
		sb.WriteString(fmt.Sprintf("## %s\n\n", roleHeader))

		switch msg.Role {
		case message.User:
			if text := msg.Content().Text; text != "" {
				sb.WriteString("### Content\n\n")
				sb.WriteString(text)
				sb.WriteString("\n\n")
			}
			for _, part := range msg.Parts {
				if bc, ok := part.(message.BinaryContent); ok {
					sb.WriteString(fmt.Sprintf("- %s (%s)\n", bc.Path, bc.MIMEType))
				}
			}

		case message.Assistant:
			if msg.Model != "" {
				sb.WriteString(fmt.Sprintf("**Model:** %s (%s)\n", msg.Model, msg.Provider))
			}
			sb.WriteString("\n")

			if reasoning := msg.ReasoningContent(); reasoning.Thinking != "" {
				sb.WriteString("### Reasoning\n\n")
				sb.WriteString("<thinking>\n")
				sb.WriteString(reasoning.Thinking)
				sb.WriteString("\n</thinking>\n\n")
			}

			if text := msg.Content().Text; text != "" {
				sb.WriteString("### Response\n\n")
				sb.WriteString(text)
				sb.WriteString("\n\n")
			}

			toolCalls := msg.ToolCalls()
			if len(toolCalls) > 0 {
				sb.WriteString("### Tool Calls\n\n")
				for _, tc := range toolCalls {
					sb.WriteString("#### Tool Call\n\n")
					sb.WriteString(fmt.Sprintf("**Tool:** `%s`\n\n", tc.Name))
					sb.WriteString("**Input:**\n\n")
					sb.WriteString("```json\n")
					sb.WriteString(tc.Input)
					sb.WriteString("\n```\n\n")
				}
			}

		case message.Tool:
			for _, tr := range msg.ToolResults() {
				sb.WriteString("#### Tool Result\n\n")
				sb.WriteString(fmt.Sprintf("**Tool:** `%s`\n", tr.Name))
				if tr.IsError {
					sb.WriteString("**Status:** Error\n")
				} else {
					sb.WriteString("**Status:** Success\n")
				}
				content := tr.Content
				if len(content) > maxToolResultSize {
					trunc := maxToolResultSize
					for trunc > 0 && !utf8.RuneStart(content[trunc]) {
						trunc--
					}
					content = content[:trunc] + "\n... (truncated)"
					sb.WriteString("**Output:** (truncated)\n\n")
				} else {
					sb.WriteString("**Output:**\n\n")
				}
				sb.WriteString("```\n")
				sb.WriteString(content)
				sb.WriteString("\n```\n\n")
			}
		}

		sb.WriteString("---\n\n")
	}

	return sb.String()
}

// saveTranscriptFromMessages writes a transcript from pre-loaded messages,
// avoiding a redundant DB round-trip. It logs errors instead of returning
// them because it is intended to be called in a goroutine.
func (a *sessionAgent) saveTranscriptFromMessages(sessionID string, msgs []message.Message) {
	if err := a.writeTranscript(sessionID, msgs); err != nil {
		slog.Warn("failed to save transcript", "error", err)
	}
}

func (a *sessionAgent) writeTranscript(sessionID string, msgs []message.Message) error {
	if a.dataDir == "" {
		return nil
	}
	transcriptsDir := filepath.Join(a.dataDir, "transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o700); err != nil {
		return fmt.Errorf("failed to create transcripts directory: %w", err)
	}

	transcriptPath := filepath.Join(transcriptsDir, sessionID+".md")
	transcript := serializeTranscript(msgs)
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o600); err != nil {
		return fmt.Errorf("failed to write transcript: %w", err)
	}

	slog.Debug("saved transcript", "path", transcriptPath, "messages", len(msgs))
	return nil
}

// TranscriptPath returns the path where a session's transcript would be saved.
func TranscriptPath(dataDir string, sessionID string) string {
	return filepath.Join(dataDir, "transcripts", sessionID+".md")
}

// KeyFactsPath returns the path where a session's key facts would be saved.
func KeyFactsPath(dataDir string, sessionID string) string {
	return filepath.Join(dataDir, "transcripts", sessionID+".facts")
}

var keyFactsRegex = regexp.MustCompile(`(?s)<key_facts>(.*?)</key_facts>`)

// analysisBlockRegex matches the <analysis>...</analysis> scratchpad block
// that the summary prompt asks the model to use as a reasoning step.
var analysisBlockRegex = regexp.MustCompile(`(?s)<analysis>.*?</analysis>\s*`)

// stripAnalysisBlock removes the <analysis> scratchpad block from summary
// text. The summary template instructs the model to think in <analysis> tags
// before writing the actual summary — this improves quality but the analysis
// itself has no value in the stored result and wastes context tokens.
func stripAnalysisBlock(text string) string {
	return analysisBlockRegex.ReplaceAllString(text, "")
}

// extractAndSaveKeyFacts parses key facts from summary text and saves to a file.
func (a *sessionAgent) extractAndSaveKeyFacts(sessionID string, summaryText string) error {
	if a.dataDir == "" {
		return nil
	}
	matches := keyFactsRegex.FindStringSubmatch(summaryText)
	if len(matches) < 2 {
		return nil
	}

	facts := strings.TrimSpace(matches[1])
	if facts == "" {
		return nil
	}

	factsDir := filepath.Join(a.dataDir, "transcripts")
	if err := os.MkdirAll(factsDir, 0o700); err != nil {
		return fmt.Errorf("failed to create transcripts directory: %w", err)
	}

	factsPath := filepath.Join(factsDir, sessionID+".facts")
	if err := os.WriteFile(factsPath, []byte(facts), 0o600); err != nil {
		return fmt.Errorf("failed to write key facts: %w", err)
	}

	slog.Debug("saved key facts", "path", factsPath)
	return nil
}

// loadKeyFacts loads key facts for a session if they exist.
func loadKeyFacts(dataDir string, sessionID string) string {
	factsPath := filepath.Join(dataDir, "transcripts", sessionID+".facts")
	data, err := os.ReadFile(factsPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
