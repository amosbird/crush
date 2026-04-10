package model

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent/notify"
	agenttools "github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/search"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	fimage "github.com/charmbracelet/crush/internal/ui/image"
	"github.com/charmbracelet/crush/internal/ui/logo"
	"github.com/charmbracelet/crush/internal/ui/notification"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/update"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/charmbracelet/crush/internal/workspace"
	uv "github.com/charmbracelet/ultraviolet"
	layoutpkg "github.com/charmbracelet/ultraviolet/layout"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/editor"
)

// MouseScrollThreshold defines how many lines to scroll the chat when a mouse
// wheel event occurs.
const MouseScrollThreshold = 3

// Compact mode breakpoints.
const (
	compactModeWidthBreakpoint  = 120
	compactModeHeightBreakpoint = 30
)

// If pasted text has more than 10 newlines, treat it as a file attachment.
const pasteLinesThreshold = 10

// If pasted text has more than 1000 columns, treat it as a file attachment.
const pasteColsThreshold = 1000

// Session details panel max height.
const sessionDetailsMaxHeight = 20

// TextareaMaxHeight is the maximum height of the prompt textarea.
const TextareaMaxHeight = 15

// editorHeightMargin is the vertical margin added to the textarea height to
// account for the attachments row (top) and bottom margin.
const editorHeightMargin = 2

// TextareaMinHeight is the minimum height of the prompt textarea.
const TextareaMinHeight = 3

// uiFocusState represents the current focus state of the UI.
type uiFocusState uint8

// Possible uiFocusState values.
const (
	uiFocusNone uiFocusState = iota
	uiFocusEditor
	uiFocusMain
)

type uiState uint8

// Possible uiState values.
const (
	uiOnboarding uiState = iota
	uiInitialize
	uiLanding
	uiChat
)

type openEditorMsg struct {
	Text string
}

type scrollToBottomMsg struct{}

type (
	// cancelTimerExpiredMsg is sent when the cancel timer expires.
	cancelTimerExpiredMsg struct{}
	// removePlaceholderMsg is sent when the agent run finishes (success or
	// failure) to clean up the placeholder spinner if it hasn't been replaced
	// by a real assistant message already.
	removePlaceholderMsg struct{}
	// agentRunErrorMsg is sent when the agent run fails with an error that
	// should be shown to the user. It also cleans up the placeholder spinner.
	agentRunErrorMsg struct{ err error }
	// userCommandsLoadedMsg is sent when user commands are loaded.
	userCommandsLoadedMsg struct {
		Commands []commands.CustomCommand
	}
	// mcpPromptsLoadedMsg is sent when mcp prompts are loaded.
	mcpPromptsLoadedMsg struct {
		Prompts []commands.MCPPrompt
	}
	// mcpStateChangedMsg is sent when there is a change in MCP client states.
	mcpStateChangedMsg struct {
		states map[string]mcp.ClientInfo
	}
	// sendMessageMsg is sent to send a message.
	// currently only used for mcp prompts.
	sendMessageMsg struct {
		Content     string
		Attachments []message.Attachment
	}
	// sessionCreatedMsg is sent when a new session is created asynchronously
	// so that sendMessage can proceed without blocking Update().
	sessionCreatedMsg struct {
		session     session.Session
		content     string
		attachments []message.Attachment
	}

	// closeDialogMsg is sent to close the current dialog.
	closeDialogMsg struct{}

	// copyChatHighlightMsg is sent to copy the current chat highlight to clipboard.
	copyChatHighlightMsg struct{}

	// copyChatHighlightDoneMsg is sent after clipboard copy completes to
	// restore focus on the main goroutine.
	copyChatHighlightDoneMsg struct{}

	// mcpToggledMsg is sent after an MCP server has been toggled in a
	// background goroutine. Config mutation happens on the main goroutine
	// when this message is received to avoid data races.
	mcpToggledMsg struct {
		Name     string
		Disabled bool
		Info     string
	}

	// insertFileCompletionMsg is returned from the insertFileCompletion
	// Cmd to apply the file read tracking and attachment on the main
	// goroutine, avoiding a data race on m.sessionFileReads.
	insertFileCompletionMsg struct {
		AbsPath    string
		Attachment message.Attachment
	}

	// sessionFilesUpdatesMsg is sent when the files for this session have been updated
	sessionFilesUpdatesMsg struct {
		sessionFiles []SessionFile
	}
)

// UI represents the main user interface model.
type UI struct {
	com          *common.Common
	session      *session.Session
	sessionFiles []SessionFile

	// keeps track of read files while we don't have a session id
	sessionFileReads []string

	// initialSessionID is set when loading a specific session on startup.
	initialSessionID string
	// continueLastSession is set to continue the most recent session on startup.
	continueLastSession bool

	lastUserMessageTime int64

	// The width and height of the terminal in cells.
	width  int
	height int
	layout uiLayout

	isTransparent bool

	focus uiFocusState
	state uiState

	keyMap KeyMap
	keyenh tea.KeyboardEnhancementsMsg

	dialog *dialog.Overlay
	status *Status

	// isCanceling tracks whether the user has pressed ctrl+g once to cancel.
	isCanceling bool

	header *header

	// sendProgressBar instructs the TUI to send progress bar updates to the
	// terminal.
	sendProgressBar    bool
	progressBarEnabled bool

	// caps hold different terminal capabilities that we query for.
	caps common.Capabilities

	// Editor components
	textarea textarea.Model

	// Attachment list
	attachments *attachments.Attachments

	readyPlaceholder   string
	workingPlaceholder string

	// Completions state
	completions              *completions.Completions
	completionsOpen          bool
	completionsStartIndex    int
	completionsQuery         string
	completionsPositionStart image.Point // x,y where user typed '@'

	// Chat components
	chat *Chat

	// onboarding state
	onboarding struct {
		yesInitializeSelected bool
	}

	// lsp
	lspStates map[string]app.LSPClientInfo

	// mcp
	mcpStates      map[string]mcp.ClientInfo
	mcpItemRects   []mcpClickTarget
	landingMCPRect image.Rectangle

	// sidebar text selection
	sidebarTextRect    image.Rectangle // screen rect of title+id+cwd block
	sidebarTextContent string          // rendered content for text extraction
	sidebarMouseDown   bool
	sidebarSelStart    [2]int // [line, col]
	sidebarSelEnd      [2]int
	sidebarHasSelect   bool

	// sidebarLogo keeps a cached version of the sidebar sidebarLogo.
	sidebarLogo string

	// Notification state
	notifyBackend       notification.Backend
	notifyWindowFocused bool
	updateAvailable     *app.UpdateAvailableMsg
	// custom commands & mcp commands
	customCommands []commands.CustomCommand
	mcpPrompts     []commands.MCPPrompt

	// forceCompactMode tracks whether compact mode is forced by user toggle
	forceCompactMode bool

	// isCompact tracks whether we're currently in compact layout mode (either
	// by user toggle or auto-switch based on window size)
	isCompact bool

	// pendingToolResults buffers tool results that arrive before their
	// corresponding tool item has been created in the UI. This handles a
	// race where OnToolResult fires so fast that the CreatedEvent reaches
	// the UI before the UpdatedEvent that creates the tool item.
	pendingToolResults map[string]*message.ToolResult

	// detailsOpen tracks whether the details panel is open (in compact mode)
	detailsOpen bool

	// pills state
	pillsExpanded      bool
	focusedPillSection pillSection
	promptQueue        int
	pillsView          string

	// landingEditorRect is the dynamically computed editor rect when in
	// landing state, used for cursor positioning without mutating the layout.
	landingEditorRect image.Rectangle

	// Todo spinner
	todoSpinner    spinner.Model
	todoIsSpinning bool

	// Session loading spinner (shown on landing page while loading a session)
	loadingSpinner spinner.Model
	loadingSession bool

	// mouse highlighting related state
	lastClickTime time.Time

	// Prompt history for up/down navigation through previous messages.
	promptHistory struct {
		messages []string
		index    int
		draft    string
	}

	// Cursor-based history pagination: track the DB cursor, whether there are
	// more pages in the DB, any in-memory items not yet revealed, and whether
	// a load-more command is in flight.
	historyCursor  message.MessageCursor
	historyHasMore bool
	remainingItems []chat.MessageItem // items from the current page above the turn window
	loadingHistory bool               // true while a load-more command is in flight
}

// New creates a new instance of the [UI] model.
func New(com *common.Common, initialSessionID string, continueLast bool) *UI {
	// Editor components
	ta := textarea.New()
	ta.SetStyles(com.Styles.TextArea)
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = TextareaMinHeight
	ta.MaxHeight = TextareaMaxHeight
	ta.Focus()

	ch := NewChat(com)

	keyMap := DefaultKeyMap()

	// Completions component
	comp := completions.New(
		com.Styles.Completions.Normal,
		com.Styles.Completions.Focused,
		com.Styles.Completions.Match,
	)

	todoSpinner := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(com.Styles.Pills.TodoSpinner),
	)

	loadingSpinner := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(com.Styles.Dialog.Spinner),
	)

	// Attachments component
	attachments := attachments.New(
		attachments.NewRenderer(
			com.Styles.Attachments.Normal,
			com.Styles.Attachments.Deleting,
			com.Styles.Attachments.Image,
			com.Styles.Attachments.Text,
		),
		attachments.Keymap{
			DeleteMode: keyMap.Editor.AttachmentDeleteMode,
			DeleteAll:  keyMap.Editor.DeleteAllAttachments,
			Escape:     keyMap.Editor.Escape,
		},
	)

	header := newHeader(com)

	ui := &UI{
		com:                 com,
		dialog:              dialog.NewOverlay(),
		keyMap:              keyMap,
		textarea:            ta,
		chat:                ch,
		header:              header,
		completions:         comp,
		attachments:         attachments,
		todoSpinner:         todoSpinner,
		loadingSpinner:      loadingSpinner,
		lspStates:           make(map[string]app.LSPClientInfo),
		mcpStates:           make(map[string]mcp.ClientInfo),
		pendingToolResults:  make(map[string]*message.ToolResult),
		notifyBackend:       notification.NoopBackend{},
		notifyWindowFocused: true,
		initialSessionID:    initialSessionID,
		continueLastSession: continueLast,
	}

	status := NewStatus(com, ui)

	ui.setEditorPrompt(com.Workspace.PermissionSkipRequests())
	ui.randomizePlaceholders()
	ui.textarea.Placeholder = ui.readyPlaceholder
	ui.status = status

	// Initialize compact mode from config
	ui.forceCompactMode = com.Config().Options.TUI.CompactMode

	// set onboarding state defaults
	ui.onboarding.yesInitializeSelected = true

	desiredState := uiChat
	desiredFocus := uiFocusEditor
	if !com.Config().IsConfigured() {
		desiredState = uiOnboarding
	} else if n, _ := com.Workspace.ProjectNeedsInitialization(); n {
		desiredState = uiInitialize
	}

	// set initial state
	ui.setState(desiredState, desiredFocus)

	opts := com.Config().Options

	// disable indeterminate progress bar
	ui.progressBarEnabled = opts.Progress == nil || *opts.Progress
	// enable transparent mode
	ui.isTransparent = opts.TUI.Transparent != nil && *opts.TUI.Transparent

	return ui
}

// Init initializes the UI model.
func (m *UI) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.state == uiOnboarding {
		if cmd := m.openModelsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// load the user commands async
	cmds = append(cmds, m.loadCustomCommands())
	// load prompt history async
	cmds = append(cmds, m.loadPromptHistory())
	// load initial session if specified
	if cmd := m.loadInitialSession(); cmd != nil {
		m.loadingSession = true
		cmds = append(cmds, cmd, m.loadingSpinner.Tick)
	}
	return tea.Batch(cmds...)
}

// loadInitialSession loads the initial session if one was specified on startup.
func (m *UI) loadInitialSession() tea.Cmd {
	switch {
	case m.state != uiChat:
		// Only load if we're in chat state (i.e., fully configured)
		return nil
	case m.initialSessionID != "":
		return m.loadSession(m.initialSessionID)
	case m.continueLastSession:
		return func() tea.Msg {
			sessions, err := m.com.Workspace.ListSessions(context.Background())
			if err != nil {
				return util.ReportError(err)()
			}
			if len(sessions) == 0 {
				return nil
			}
			return m.loadSession(sessions[0].ID)()
		}
	default:
		return nil
	}
}

// sendNotification returns a command that sends a notification if allowed by policy.
func (m *UI) sendNotification(n notification.Notification) tea.Cmd {
	if !m.shouldSendNotification() {
		return nil
	}

	backend := m.notifyBackend
	return func() tea.Msg {
		if err := backend.Send(n); err != nil {
			slog.Error("Failed to send notification", "error", err)
		}
		return nil
	}
}

// shouldSendNotification returns true if notifications should be sent based on
// current state. Focus reporting must be supported, window must not focused,
// and notifications must not be disabled in config.
func (m *UI) shouldSendNotification() bool {
	cfg := m.com.Config()
	if cfg != nil && cfg.Options != nil && cfg.Options.DisableNotifications {
		return false
	}
	return m.caps.ReportFocusEvents && !m.notifyWindowFocused
}

// setState changes the UI state and focus.
func (m *UI) setState(state uiState, focus uiFocusState) {
	if state == uiLanding || state == uiInitialize {
		// Always turn off compact mode when going to landing
		m.isCompact = false
	}
	m.state = state
	m.focus = focus
	// Changing the state may change layout, so update it.
	m.updateLayoutAndSize()
}

// loadCustomCommands loads the custom commands asynchronously.
func (m *UI) loadCustomCommands() tea.Cmd {
	return func() tea.Msg {
		customCommands, err := commands.LoadCustomCommands(m.com.Config())
		if err != nil {
			slog.Error("Failed to load custom commands", "error", err)
		}
		return userCommandsLoadedMsg{Commands: customCommands}
	}
}

// loadMCPrompts loads the MCP prompts asynchronously.
func (m *UI) loadMCPrompts() tea.Msg {
	prompts, err := commands.LoadMCPPrompts()
	if err != nil {
		slog.Error("Failed to load MCP prompts", "error", err)
	}
	if prompts == nil {
		// flag them as loaded even if there is none or an error
		prompts = []commands.MCPPrompt{}
	}
	return mcpPromptsLoadedMsg{Prompts: prompts}
}

// Update handles updates to the UI model.
func (m *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.hasSession() && m.isAgentBusy() {
		queueSize := m.com.Workspace.AgentQueuedPrompts(m.session.ID)
		if queueSize != m.promptQueue {
			m.promptQueue = queueSize
			m.updateLayoutAndSize()
		}
	}
	// Update terminal capabilities
	m.caps.Update(msg)
	switch msg := msg.(type) {
	case tea.EnvMsg:
		// Is this Windows Terminal?
		if !m.sendProgressBar {
			m.sendProgressBar = slices.Contains(msg, "WT_SESSION")
		}
		cmds = append(cmds, common.QueryCmd(uv.Environ(msg)))
	case tea.ModeReportMsg:
		if m.caps.ReportFocusEvents {
			m.notifyBackend = notification.NewNativeBackend(notification.Icon)
		}
	case tea.FocusMsg:
		m.notifyWindowFocused = true
	case tea.BlurMsg:
		m.notifyWindowFocused = false
	case app.UpdateAvailableMsg:
		m.updateAvailable = &msg
		cmds = append(cmds, util.ReportInfo(fmt.Sprintf("Update available: %s → %s (use Ctrl+P → Update Crush)", msg.CurrentVersion, msg.LatestVersion)))
	case pubsub.Event[notify.Notification]:
		if cmd := m.handleAgentNotification(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case loadSessionMsg:
		m.loadingSession = false
		m.pendingToolResults = make(map[string]*message.ToolResult)
		if m.forceCompactMode {
			m.isCompact = true
		}
		m.setState(uiChat, m.focus)
		m.session = msg.session
		m.syncTmuxSessionID()
		m.sessionFiles = msg.files
		cmds = append(cmds, m.startLSPs(msg.lspFilePaths()))

		m.lastUserMessageTime = msg.lastUserMsgTime
		m.renderPills()

		// Store pagination state for "load more" on scroll-to-top.
		m.historyCursor = msg.cursor
		m.historyHasMore = msg.hasMore
		m.remainingItems = msg.remainingItems
		m.loadingHistory = false

		if len(msg.messageItems) > 0 {
			for _, item := range msg.messageItems {
				if animatable, ok := item.(chat.Animatable); ok {
					if cmd := animatable.StartAnimation(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}
			m.chat.SetMessages(msg.messageItems...)
			if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			m.chat.SelectLast()
			m.chat.ScrollToBottom()
			cmds = append(cmds, func() tea.Msg { return scrollToBottomMsg{} })
		}

		if hasInProgressTodo(m.session.Todos) {
			// only start spinner if there is an in-progress todo
			if m.isAgentBusy() {
				m.todoIsSpinning = true
				cmds = append(cmds, m.todoSpinner.Tick)
			}
			m.updateLayoutAndSize()
		}
		// Reload prompt history for the new session.
		m.historyReset()
		cmds = append(cmds, m.loadPromptHistory())
		m.updateLayoutAndSize()

	case sessionFilesUpdatesMsg:
		m.sessionFiles = msg.sessionFiles
		var paths []string
		for _, f := range msg.sessionFiles {
			paths = append(paths, f.LatestVersion.Path)
		}
		cmds = append(cmds, m.startLSPs(paths))

	case loadMoreHistoryMsg:
		m.loadingHistory = false
		m.historyCursor = msg.cursor
		m.historyHasMore = msg.hasMore
		m.remainingItems = msg.remainingItems
		if len(msg.items) > 0 {
			for _, item := range msg.items {
				if animatable, ok := item.(chat.Animatable); ok {
					if cmd := animatable.StartAnimation(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}
			m.chat.PrependMessages(msg.items...)
		}

	case sendMessageMsg:
		cmds = append(cmds, m.sendMessage(msg.Content, msg.Attachments...))

	case sessionCreatedMsg:
		if msg.session.ID != "" {
			m.session = &msg.session
			m.syncTmuxSessionID()
			m.syncTmuxPaneTitle()
			cmds = append(cmds, m.loadSession(msg.session.ID))
		}
		cmds = append(cmds, m.sendMessageWithSession(msg.content, msg.attachments...))

	case userCommandsLoadedMsg:
		m.customCommands = msg.Commands
		dia := m.dialog.Dialog(dialog.CommandsID)
		if dia == nil {
			break
		}

		commands, ok := dia.(*dialog.Commands)
		if ok {
			commands.SetCustomCommands(m.customCommands)
		}

	case mcpStateChangedMsg:
		m.mcpStates = msg.states
	case mcpPromptsLoadedMsg:
		m.mcpPrompts = msg.Prompts
		dia := m.dialog.Dialog(dialog.CommandsID)
		if dia == nil {
			break
		}

		commands, ok := dia.(*dialog.Commands)
		if ok {
			commands.SetMCPPrompts(m.mcpPrompts)
		}

	case promptHistoryLoadedMsg:
		m.promptHistory.messages = msg.messages
		m.promptHistory.index = -1
		m.promptHistory.draft = ""

	case closeDialogMsg:
		m.dialog.CloseFrontDialog()

	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.DeletedEvent {
			if m.session != nil && m.session.ID == msg.Payload.ID {
				if cmd := m.newSession(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			break
		}
		if m.session != nil && msg.Payload.ID == m.session.ID {
			prevHasInProgress := hasInProgressTodo(m.session.Todos)
			m.session = &msg.Payload
			m.syncTmuxPaneTitle()
			if !prevHasInProgress && hasInProgressTodo(m.session.Todos) {
				m.todoIsSpinning = true
				cmds = append(cmds, m.todoSpinner.Tick)
			}
			m.renderPills()
			m.updateLayoutAndSize()
		}
	case pubsub.Event[message.Message]:
		// Check if this is a child session message for an agent tool.
		if m.session == nil {
			break
		}
		if msg.Payload.SessionID != m.session.ID {
			// This might be a child session message from an agent tool.
			if cmd := m.handleChildSessionMessage(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
			break
		}
		switch msg.Type {
		case pubsub.CreatedEvent:
			cmds = append(cmds, m.appendSessionMessage(msg.Payload))
			m.renderPills()
		case pubsub.UpdatedEvent:
			cmds = append(cmds, m.updateSessionMessage(msg.Payload))
		case pubsub.DeletedEvent:
			m.chat.RemoveMessage(msg.Payload.ID)
			for _, tr := range msg.Payload.ToolResults() {
				m.chat.RemoveMessage(tr.ToolCallID)
			}
			m.chat.ScrollToBottom()
			cmds = append(cmds, func() tea.Msg { return scrollToBottomMsg{} })
			m.renderPills()
		}
		// start the spinner if there is a new message
		if hasInProgressTodo(m.session.Todos) && m.isAgentBusy() && !m.todoIsSpinning {
			m.todoIsSpinning = true
			cmds = append(cmds, m.todoSpinner.Tick)
		}
		// Stop the spinner if the agent is not busy anymore, and
		// mark any leftover in-progress todos as completed since the
		// agent will not update them.
		if m.todoIsSpinning && !m.isAgentBusy() {
			m.todoIsSpinning = false
			if m.hasSession() {
				m.completeRemainingTodos()
			}
		}
	case pubsub.Event[history.File]:
		cmds = append(cmds, m.handleFileEvent(msg.Payload))
	case pubsub.Event[app.LSPEvent]:
		m.lspStates = app.GetLSPStates()
	case pubsub.Event[mcp.Event]:
		switch msg.Payload.Type {
		case mcp.EventStateChanged:
			return m, tea.Batch(
				m.handleStateChanged(),
				m.loadMCPrompts,
			)
		case mcp.EventPromptsListChanged:
			return m, handleMCPPromptsEvent(m.com.Workspace, msg.Payload.Name)
		case mcp.EventToolsListChanged:
			return m, handleMCPToolsEvent(m.com.Workspace, msg.Payload.Name)
		case mcp.EventResourcesListChanged:
			return m, handleMCPResourcesEvent(m.com.Workspace, msg.Payload.Name)
		}
	case pubsub.Event[permission.PermissionRequest]:
		if cmd := m.openPermissionsDialog(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.sendNotification(notification.Notification{
			Title:   "Crush is waiting...",
			Message: fmt.Sprintf("Permission required to execute \"%s\"", msg.Payload.ToolName),
		}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[permission.PermissionNotification]:
		m.handlePermissionNotification(msg.Payload)
	case cancelTimerExpiredMsg:
		m.isCanceling = false
	case tea.TerminalVersionMsg:
		termVersion := strings.ToLower(msg.Name)
		// Only enable progress bar for the following terminals.
		if !m.sendProgressBar {
			m.sendProgressBar = strings.Contains(termVersion, "ghostty")
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.updateLayoutAndSize()
		if m.state == uiChat && m.chat.Follow() {
			if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case tea.KeyboardEnhancementsMsg:
		m.keyenh = msg
		if msg.SupportsKeyDisambiguation() {
			m.keyMap.Models.SetHelp("alt+m", "models")
			m.keyMap.Editor.Newline.SetHelp("shift+enter", "newline")
		}
	case copyChatHighlightMsg:
		cmds = append(cmds, m.copyChatHighlight())
	case copyChatHighlightDoneMsg:
		m.chat.ClearMouse()
		m.focus = uiFocusEditor
		m.chat.Blur()
		m.textarea.Focus() //nolint:errcheck // cursor blink cmd not needed here
	case mcpToggledMsg:
		if cfg, ok := m.com.Config().MCP[msg.Name]; ok {
			cfg.Disabled = msg.Disabled
			m.com.Config().MCP[msg.Name] = cfg
		}
		cmds = append(cmds, util.ReportInfo(msg.Info))
	case insertFileCompletionMsg:
		m.sessionFileReads = append(m.sessionFileReads, msg.AbsPath)
		m.attachments.Update(msg.Attachment)
		return m, tea.Batch(cmds...)
	case DelayedClickMsg:
		// Handle delayed single-click action (e.g., expansion).
		if _, cmd := m.chat.HandleDelayedClick(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case ImagePreviewMsg:
		if cmd := m.openImagePreviewDialog(msg.Attachment); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case TextPreviewMsg:
		m.openTextPreviewDialog(msg.Title, msg.Text)
	case DiffPreviewMsg:
		m.openDiffPreviewDialog(msg.FilePath, msg.OldContent, msg.NewContent)
	case JobPreviewMsg:
		cmds = append(cmds, m.openJobPreviewDialog(msg))
	case tea.MouseClickMsg:
		// Pass mouse events to dialogs first if any are open.
		if m.dialog.HasDialogs() {
			m.dialog.Update(msg)
			return m, tea.Batch(cmds...)
		}

		if cmd := m.handleClickFocus(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

		if len(m.attachments.List()) > 0 {
			editorRect := m.layout.editor
			if m.state == uiLanding {
				editorRect = m.landingEditorRect
			}
			if msg.Y == editorRect.Min.Y {
				relX := msg.X - editorRect.Min.X
				if m.attachments.HandleClick(relX) {
					return m, tea.Batch(cmds...)
				}
				if att := m.attachments.AttachmentAt(relX); att != nil {
					if strings.HasPrefix(att.MimeType, "image/") {
						if cmd := m.openImagePreviewDialog(*att); cmd != nil {
							cmds = append(cmds, cmd)
						}
						return m, tea.Batch(cmds...)
					}
					if strings.HasPrefix(att.MimeType, "text/") || strings.HasSuffix(att.FileName, ".txt") {
						m.openTextPreviewDialog(att.FileName, string(att.Content))
						return m, tea.Batch(cmds...)
					}
				}
			}
		}

		switch m.state {
		case uiLanding:
			if cmd := m.handleMCPClick(msg.X, msg.Y); cmd != nil {
				cmds = append(cmds, cmd)
			}
		case uiChat:
			x, y := msg.X, msg.Y
			// Adjust for chat area position
			x -= m.layout.main.Min.X
			y -= m.layout.main.Min.Y
			if image.Pt(msg.X, msg.Y).In(m.layout.sidebar) {
				if image.Pt(msg.X, msg.Y).In(m.sidebarTextRect) {
					m.sidebarMouseDown = true
					m.sidebarHasSelect = false
					line := msg.Y - m.sidebarTextRect.Min.Y
					col := msg.X - m.sidebarTextRect.Min.X
					m.sidebarSelStart = [2]int{line, col}
					m.sidebarSelEnd = [2]int{line, col}
				} else if cmd := m.handleMCPClick(msg.X, msg.Y); cmd != nil {
					cmds = append(cmds, cmd)
				}
			} else if handled, cmd := m.chat.HandleMouseDown(x, y); handled {
				m.lastClickTime = time.Now()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}

	case tea.MouseMotionMsg:
		// Pass mouse events to dialogs first if any are open.
		if m.dialog.HasDialogs() {
			m.dialog.Update(msg)
			return m, tea.Batch(cmds...)
		}

		switch m.state {
		case uiChat:
			if m.sidebarMouseDown {
				line := msg.Y - m.sidebarTextRect.Min.Y
				col := msg.X - m.sidebarTextRect.Min.X
				m.sidebarSelEnd = [2]int{line, col}
				m.sidebarHasSelect = m.sidebarSelStart != m.sidebarSelEnd
				return m, tea.Batch(cmds...)
			}
			if msg.Y <= 0 {
				if cmd := m.chat.ScrollByAndAnimate(-1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectPrev()
					if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
				if cmd := m.maybeLoadMoreHistory(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			} else if msg.Y >= m.chat.Height()-1 {
				if cmd := m.chat.ScrollByAndAnimate(1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectNext()
					if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}

			x, y := msg.X, msg.Y
			// Adjust for chat area position
			x -= m.layout.main.Min.X
			y -= m.layout.main.Min.Y
			m.chat.HandleMouseDrag(x, y)
		}

	case tea.MouseReleaseMsg:
		// Pass mouse events to dialogs first if any are open.
		if m.dialog.HasDialogs() {
			if cmd := m.handleDialogMsg(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		switch m.state {
		case uiChat:
			if m.sidebarMouseDown {
				m.sidebarMouseDown = false
				if m.sidebarHasSelect {
					if text := m.sidebarSelectedText(); text != "" {
						m.sidebarHasSelect = false
						cmds = append(cmds, common.CopyToClipboard(text, "Copied to clipboard"))
					}
				}
				return m, tea.Batch(cmds...)
			}
			x, y := msg.X, msg.Y
			// Adjust for chat area position
			x -= m.layout.main.Min.X
			y -= m.layout.main.Min.Y
			if m.chat.HandleMouseUp(x, y) && m.chat.HasHighlight() {
				cmds = append(cmds, tea.Tick(doubleClickThreshold, func(t time.Time) tea.Msg {
					if time.Since(m.lastClickTime) >= doubleClickThreshold {
						return copyChatHighlightMsg{}
					}
					return nil
				}))
			}
		}
	case tea.MouseWheelMsg:
		// Pass mouse events to dialogs first if any are open.
		if m.dialog.HasDialogs() {
			m.dialog.Update(msg)
			return m, tea.Batch(cmds...)
		}

		// Otherwise handle mouse wheel for chat.
		switch m.state {
		case uiChat:
			switch msg.Button {
			case tea.MouseWheelUp:
				if cmd := m.chat.ScrollByAndAnimate(-MouseScrollThreshold); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectFirstInView()
				}
				if cmd := m.maybeLoadMoreHistory(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case tea.MouseWheelDown:
				if cmd := m.chat.ScrollByAndAnimate(MouseScrollThreshold); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectLastInView()
				}
			}
		}
	case anim.StepMsg:
		if m.state == uiChat {
			if cmd := m.chat.Animate(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
			if m.chat.Follow() {
				if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	case spinner.TickMsg:
		if m.dialog.HasDialogs() {
			// route to dialog
			if cmd := m.handleDialogMsg(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if m.loadingSession {
			var cmd tea.Cmd
			m.loadingSpinner, cmd = m.loadingSpinner.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if m.state == uiChat && m.hasSession() && hasInProgressTodo(m.session.Todos) && m.todoIsSpinning {
			var cmd tea.Cmd
			m.todoSpinner, cmd = m.todoSpinner.Update(msg)
			if cmd != nil {
				m.renderPills()
				cmds = append(cmds, cmd)
			}
		}

	case tea.KeyPressMsg:
		if cmd := m.handleKeyPressMsg(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case tea.PasteMsg:
		if cmd := m.handlePasteMsg(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case openEditorMsg:
		prevHeight := m.textarea.Height()
		m.textarea.SetValue(msg.Text)
		m.textarea.MoveToEnd()
		cmds = append(cmds, m.updateTextareaWithPrevHeight(msg, prevHeight))
	case util.InfoMsg:
		if msg.Type == util.InfoTypeError {
			slog.Error("Error reported", "error", msg.Msg)
		}
		m.status.SetInfoMsg(msg)
		ttl := msg.TTL
		if ttl <= 0 {
			ttl = DefaultStatusTTL
		}
		cmds = append(cmds, clearInfoMsgCmd(ttl))
	case scrollToBottomMsg:
		m.chat.ScrollToBottom()
	case util.ClearStatusMsg:
		m.status.ClearInfoMsg()
	case removePlaceholderMsg:
		m.chat.RemoveMessage(chat.PlaceholderID)
	case agentRunErrorMsg:
		m.chat.RemoveMessage(chat.PlaceholderID)
		m.status.SetInfoMsg(util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  msg.err.Error(),
		})
		cmds = append(cmds, clearInfoMsgCmd(DefaultStatusTTL))
	case completions.CompletionItemsLoadedMsg:
		if m.completionsOpen {
			m.completions.SetItems(msg.Files, msg.Resources)
		}
	case uv.KittyGraphicsEvent:
		if !bytes.HasPrefix(msg.Payload, []byte("OK")) {
			slog.Warn("Unexpected Kitty graphics response",
				"response", string(msg.Payload),
				"options", msg.Options)
		}
	default:
		if m.dialog.HasDialogs() {
			if cmd := m.handleDialogMsg(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	// This logic gets triggered on any message type, but should it?
	switch m.focus {
	case uiFocusMain:
	case uiFocusEditor:
		// Textarea placeholder logic
		if m.isAgentBusy() {
			m.textarea.Placeholder = m.workingPlaceholder
		} else {
			m.textarea.Placeholder = m.readyPlaceholder
		}
		if m.com.Workspace.PermissionSkipRequests() {
			m.textarea.Placeholder = "Yolo mode!"
		}
	}

	// at this point this can only handle [message.Attachment] message, and we
	// should return all cmds anyway.
	m.attachments.Update(msg)
	return m, tea.Batch(cmds...)
}

// loadNestedToolCalls recursively loads nested tool calls for agent/agentic_fetch tools.
func (m *UI) loadNestedToolCalls(items []chat.MessageItem) {
	for _, item := range items {
		nestedContainer, ok := item.(chat.NestedToolContainer)
		if !ok {
			continue
		}
		toolItem, ok := item.(chat.ToolMessageItem)
		if !ok {
			continue
		}

		tc := toolItem.ToolCall()
		messageID := toolItem.MessageID()

		// Get the agent tool session ID.
		agentSessionID := m.com.Workspace.CreateAgentToolSessionID(messageID, tc.ID)

		// Fetch nested messages.
		nestedMsgs, err := m.com.Workspace.ListMessages(context.Background(), agentSessionID)
		if err != nil || len(nestedMsgs) == 0 {
			continue
		}

		// Build tool result map for nested messages.
		nestedMsgPtrs := make([]*message.Message, len(nestedMsgs))
		for i := range nestedMsgs {
			nestedMsgPtrs[i] = &nestedMsgs[i]
		}
		nestedToolResultMap := chat.BuildToolResultMap(nestedMsgPtrs)

		// Extract nested tool items.
		var nestedTools []chat.ToolMessageItem
		for _, nestedMsg := range nestedMsgPtrs {
			nestedItems := chat.ExtractMessageItems(m.com.Styles, nestedMsg, nestedToolResultMap)
			for _, nestedItem := range nestedItems {
				if nestedToolItem, ok := nestedItem.(chat.ToolMessageItem); ok {
					// Mark nested tools as simple (compact) rendering.
					if simplifiable, ok := nestedToolItem.(chat.Compactable); ok {
						simplifiable.SetCompact(true)
					}
					nestedTools = append(nestedTools, nestedToolItem)
				}
			}
		}

		// Recursively load nested tool calls for any agent tools within.
		nestedMessageItems := make([]chat.MessageItem, len(nestedTools))
		for i, nt := range nestedTools {
			nestedMessageItems[i] = nt
		}
		m.loadNestedToolCalls(nestedMessageItems)

		// Set nested tools on the parent.
		nestedContainer.SetNestedTools(nestedTools)
	}
}

// maybeLoadMoreHistory checks if the chat is scrolled to the top and there
// are older messages available. If so, it fires a command to load more turns.
func (m *UI) maybeLoadMoreHistory() tea.Cmd {
	if !m.chat.AtTop() || m.loadingHistory || (!m.historyHasMore && len(m.remainingItems) == 0) {
		return nil
	}
	m.loadingHistory = true
	return m.loadMoreHistory()
}

// appendSessionMessage appends a new message to the current session in the chat
// if the message is a tool result it will update the corresponding tool call message
func (m *UI) appendSessionMessage(msg message.Message) tea.Cmd {
	var cmds []tea.Cmd

	existing := m.chat.MessageItem(msg.ID)
	if existing != nil {
		// message already exists, skip
		return nil
	}

	switch msg.Role {
	case message.User:
		m.lastUserMessageTime = msg.CreatedAt
		items := chat.ExtractMessageItems(m.com.Styles, &msg, nil)
		for _, item := range items {
			if animatable, ok := item.(chat.Animatable); ok {
				if cmd := animatable.StartAnimation(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		m.chat.AppendMessages(items...)
		if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case message.Assistant:
		m.chat.RemoveMessage(chat.PlaceholderID)
		items := chat.ExtractMessageItems(m.com.Styles, &msg, nil)
		for _, item := range items {
			if animatable, ok := item.(chat.Animatable); ok {
				if cmd := animatable.StartAnimation(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		m.chat.AppendMessages(items...)
		if m.chat.Follow() {
			if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if msg.FinishPart() != nil && msg.FinishPart().Reason == message.FinishReasonEndTurn {
			infoItem := chat.NewAssistantInfoItem(m.com.Styles, &msg, m.com.Config(), time.Unix(m.lastUserMessageTime, 0))
			m.chat.AppendMessages(infoItem)
			if m.chat.Follow() {
				if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				cmds = append(cmds, func() tea.Msg { return scrollToBottomMsg{} })
			}
		}
	case message.Tool:
		for _, tr := range msg.ToolResults() {
			toolItem := m.chat.MessageItem(tr.ToolCallID)
			if toolItem == nil {
				m.pendingToolResults[tr.ToolCallID] = &tr
				continue
			}
			if toolMsgItem, ok := toolItem.(chat.ToolMessageItem); ok {
				toolMsgItem.SetResult(&tr)
				m.chat.InvalidateItemHeight(tr.ToolCallID)
				if m.chat.Follow() {
					if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}
		}
	}
	return tea.Sequence(cmds...)
}

func (m *UI) handleClickFocus(msg tea.MouseClickMsg) (cmd tea.Cmd) {
	switch {
	case m.state != uiChat:
		return nil
	case image.Pt(msg.X, msg.Y).In(m.layout.sidebar):
		return nil
	case m.focus != uiFocusEditor && image.Pt(msg.X, msg.Y).In(m.layout.editor):
		m.focus = uiFocusEditor
		cmd = m.textarea.Focus()
		m.chat.Blur()
	case m.focus != uiFocusMain && image.Pt(msg.X, msg.Y).In(m.layout.main):
		// Keep focus in editor; do not switch to chat mode.
	}
	return cmd
}

// updateSessionMessage updates an existing message in the current session in the chat
// when an assistant message is updated it may include updated tool calls as well
// that is why we need to handle creating/updating each tool call message too
func (m *UI) updateSessionMessage(msg message.Message) tea.Cmd {
	var cmds []tea.Cmd
	existingItem := m.chat.MessageItem(msg.ID)

	if existingItem != nil {
		if assistantItem, ok := existingItem.(*chat.AssistantMessageItem); ok {
			if cmd := assistantItem.SetMessage(&msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
			if msg.IsFinished() {
				m.chat.InvalidateItemHeight(msg.ID)
			}
		}
	}

	shouldRenderAssistant := chat.ShouldRenderAssistantMessage(&msg)
	// if the message of the assistant does not have any  response just tool calls we need to remove it
	if !shouldRenderAssistant && len(msg.ToolCalls()) > 0 && existingItem != nil {
		m.chat.RemoveMessage(msg.ID)
		if infoItem := m.chat.MessageItem(chat.AssistantInfoID(msg.ID)); infoItem != nil {
			m.chat.RemoveMessage(chat.AssistantInfoID(msg.ID))
		}
	}

	var items []chat.MessageItem
	for _, tc := range msg.ToolCalls() {
		existingToolItem := m.chat.MessageItem(tc.ID)
		if toolItem, ok := existingToolItem.(chat.ToolMessageItem); ok {
			existingToolCall := toolItem.ToolCall()
			// only update if finished state changed or input changed
			// to avoid clearing the cache
			if (tc.Finished && !existingToolCall.Finished) || tc.Input != existingToolCall.Input {
				toolItem.SetToolCall(tc)
				m.chat.InvalidateItemHeight(tc.ID)
			}
		}
		if existingToolItem == nil {
			pendingResult := m.pendingToolResults[tc.ID]
			delete(m.pendingToolResults, tc.ID)
			items = append(items, chat.NewToolMessageItem(m.com.Styles, msg.ID, tc, pendingResult, false))
		}
	}

	for _, item := range items {
		if animatable, ok := item.(chat.Animatable); ok {
			if cmd := animatable.StartAnimation(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	m.chat.AppendMessages(items...)

	if shouldRenderAssistant && msg.FinishPart() != nil && msg.FinishPart().Reason == message.FinishReasonEndTurn {
		if infoItem := m.chat.MessageItem(chat.AssistantInfoID(msg.ID)); infoItem == nil {
			newInfoItem := chat.NewAssistantInfoItem(m.com.Styles, &msg, m.com.Config(), time.Unix(m.lastUserMessageTime, 0))
			m.chat.AppendMessages(newInfoItem)
		}
	}
	if m.chat.Follow() {
		if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.chat.SelectLast()
	}

	return tea.Sequence(cmds...)
}

// handleChildSessionMessage handles messages from child sessions (agent tools).
func (m *UI) handleChildSessionMessage(event pubsub.Event[message.Message]) tea.Cmd {
	var cmds []tea.Cmd

	// Only process messages with content, tool calls, or results.
	hasContent := strings.TrimSpace(event.Payload.Content().Text) != ""
	hasToolCalls := len(event.Payload.ToolCalls()) > 0
	hasToolResults := len(event.Payload.ToolResults()) > 0
	if !hasContent && !hasToolCalls && !hasToolResults {
		return nil
	}

	// Check if this is an agent tool session and parse it.
	childSessionID := event.Payload.SessionID
	_, toolCallID, ok := m.com.Workspace.ParseAgentToolSessionID(childSessionID)
	if !ok {
		return nil
	}

	// Find the parent agent tool item.
	var agentItem chat.NestedToolContainer
	item := m.chat.MessageItem(toolCallID)
	if item != nil {
		if agent, ok := item.(chat.NestedToolContainer); ok {
			if toolMessageItem, ok := item.(chat.ToolMessageItem); ok {
				if toolMessageItem.ToolCall().ID == toolCallID {
					agentItem = agent
				}
			}
		}
	}

	if agentItem == nil {
		return nil
	}

	// Update streaming text from the sub-agent's assistant message.
	if hasContent {
		agentItem.SetStreamingText(event.Payload.Content().Text)
	}

	// Fast path: when only the streaming text changed (no new tool calls
	// or results), skip the expensive nested-tool rebuild and height
	// invalidation. This dramatically reduces CPU during fast token
	// streaming from sub-agents.
	if hasToolCalls || hasToolResults {
		nestedTools := agentItem.NestedTools()

		// Build an index from tool call ID to slice position for O(1) lookups.
		toolIndex := make(map[string]int, len(nestedTools))
		for i, nt := range nestedTools {
			toolIndex[nt.ToolCall().ID] = i
		}

		// Update or create nested tool calls.
		for _, tc := range event.Payload.ToolCalls() {
			if idx, ok := toolIndex[tc.ID]; ok {
				nestedTools[idx].SetToolCall(tc)
			} else {
				// Create a new nested tool item.
				nestedItem := chat.NewToolMessageItem(m.com.Styles, event.Payload.ID, tc, nil, false)
				if simplifiable, ok := nestedItem.(chat.Compactable); ok {
					simplifiable.SetCompact(true)
				}
				if animatable, ok := nestedItem.(chat.Animatable); ok {
					if cmd := animatable.StartAnimation(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
				toolIndex[tc.ID] = len(nestedTools)
				nestedTools = append(nestedTools, nestedItem)
			}
		}

		// Update nested tool results.
		for _, tr := range event.Payload.ToolResults() {
			if idx, ok := toolIndex[tr.ToolCallID]; ok {
				nestedTools[idx].SetResult(&tr)
			}
		}

		// Update the agent item with the new nested tools.
		agentItem.SetNestedTools(nestedTools)
		m.chat.InvalidateItemHeight(toolCallID)

		// Update the chat so it updates the index map for animations to work as expected
		m.chat.UpdateNestedToolIDs(toolCallID)
	}

	if m.chat.Follow() {
		if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.chat.SelectLast()
	}

	return tea.Sequence(cmds...)
}

func (m *UI) handleDialogMsg(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	action := m.dialog.Update(msg)
	if action == nil {
		return tea.Batch(cmds...)
	}

	isOnboarding := m.state == uiOnboarding

	switch msg := action.(type) {
	// Generic dialog messages
	case dialog.ActionClose:
		if isOnboarding && m.dialog.ContainsDialog(dialog.ModelsID) {
			break
		}

		if m.dialog.ContainsDialog(dialog.FilePickerID) {
			defer fimage.ResetCache()
		}

		m.dialog.CloseFrontDialog()

		if isOnboarding {
			if cmd := m.openModelsDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

		if m.focus == uiFocusEditor {
			cmds = append(cmds, m.textarea.Focus())
		}
	case dialog.ActionCmd:
		if msg.Cmd != nil {
			cmds = append(cmds, msg.Cmd)
		}

	// Session dialog messages.
	case dialog.ActionSelectSession:
		m.dialog.CloseDialog(dialog.SessionsID)
		cmds = append(cmds, m.loadSession(msg.Session.ID))

	// Open dialog message.
	case dialog.ActionOpenDialog:
		m.dialog.CloseDialog(dialog.CommandsID)
		if cmd := m.openDialog(msg.DialogID); cmd != nil {
			cmds = append(cmds, cmd)
		}

	// Command dialog messages.
	case dialog.ActionToggleYoloMode:
		yolo := !m.com.Workspace.PermissionSkipRequests()
		m.com.Workspace.PermissionSetSkipRequests(yolo)
		m.setEditorPrompt(yolo)
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionSwitchAgent:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is working, please wait..."))
			break
		}
		if err := m.com.App.AgentCoordinator.SetMainAgent(msg.AgentID); err != nil {
			cmds = append(cmds, util.ReportError(err))
		} else {
			agentCfg := m.com.Config().Agents[msg.AgentID]
			cmds = append(cmds, util.CmdHandler(util.NewInfoMsg("Switched to "+agentCfg.Name+" agent")))
			m.renderPills()
		}
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionToggleNotifications:
		cfg := m.com.Config()
		if cfg != nil && cfg.Options != nil {
			disabled := !cfg.Options.DisableNotifications
			cfg.Options.DisableNotifications = disabled
			if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.disable_notifications", disabled); err != nil {
				cmds = append(cmds, util.ReportError(err))
			} else {
				status := "enabled"
				if disabled {
					status = "disabled"
				}
				cmds = append(cmds, util.CmdHandler(util.NewInfoMsg("Notifications "+status)))
			}
		}
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionNewSession:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before starting a new session..."))
			break
		}
		if cmd := m.newSession(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionSummarize:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before summarizing session..."))
			break
		}
		cmds = append(cmds, func() tea.Msg {
			err := m.com.Workspace.AgentSummarize(context.Background(), msg.SessionID)
			if err != nil {
				return util.ReportError(err)()
			}
			return nil
		})
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionToggleHelp:
		m.status.ToggleHelp()
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionExternalEditor:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is working, please wait..."))
			break
		}
		cmds = append(cmds, m.openEditor(m.textarea.Value()))
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionToggleCompactMode:
		cmds = append(cmds, m.toggleCompactMode())
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionTogglePills:
		if cmd := m.togglePillsExpanded(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionToggleThinking:
		cmds = append(cmds, func() tea.Msg {
			cfg := m.com.Config()
			if cfg == nil {
				return util.ReportError(errors.New("configuration not found"))()
			}

			agentCfg, ok := cfg.Agents[config.AgentCoder]
			if !ok {
				return util.ReportError(errors.New("agent configuration not found"))()
			}

			currentModel := cfg.Models[agentCfg.Model]
			currentModel.Think = !currentModel.Think
			if err := m.com.Workspace.UpdatePreferredModel(config.ScopeGlobal, agentCfg.Model, currentModel); err != nil {
				return util.ReportError(err)()
			}
			m.com.Workspace.UpdateAgentModel(context.TODO())
			status := "disabled"
			if currentModel.Think {
				status = "enabled"
			}
			return util.NewInfoMsg("Thinking mode " + status)
		})
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionToggleTransparentBackground:
		cmds = append(cmds, func() tea.Msg {
			cfg := m.com.Config()
			if cfg == nil {
				return util.ReportError(errors.New("configuration not found"))()
			}

			isTransparent := cfg.Options != nil && cfg.Options.TUI.Transparent != nil && *cfg.Options.TUI.Transparent
			newValue := !isTransparent
			if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.tui.transparent", newValue); err != nil {
				return util.ReportError(err)()
			}
			m.isTransparent = newValue

			status := "disabled"
			if newValue {
				status = "enabled"
			}
			return util.NewInfoMsg("Transparent background " + status)
		})
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionQuit:
		cmds = append(cmds, tea.Quit)
	case dialog.ActionEnableDockerMCP:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, m.enableDockerMCP)
	case dialog.ActionDisableDockerMCP:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, m.disableDockerMCP)
	case dialog.ActionRefreshCopilotModels:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, m.refreshCopilotModels())
	case dialog.ActionNewWindow:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, m.openNewMuxWindow())
	case dialog.ActionSelfUpdate:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, m.selfUpdate())
	case dialog.ActionToggleMCP:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, m.toggleMCP(msg.Name, msg.Disable))
	case dialog.ActionForkSession:
		m.dialog.CloseDialog(dialog.SessionsID)
		m.dialog.CloseDialog(dialog.CommandsID)
		sid := msg.SessionID
		if sid == "" && m.session != nil {
			sid = m.session.ID
		}
		if sid == "" {
			cmds = append(cmds, util.ReportWarn("No session to fork"))
			break
		}
		cmds = append(cmds, m.forkSessionToMuxWindow(sid))
	case dialog.ActionInitializeProject:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before summarizing session..."))
			break
		}
		cmds = append(cmds, m.initializeProject())
		m.dialog.CloseDialog(dialog.CommandsID)

	case dialog.ActionSelectModel:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait..."))
			break
		}

		cfg := m.com.Config()
		if cfg == nil {
			cmds = append(cmds, util.ReportError(errors.New("configuration not found")))
			break
		}

		var (
			providerID   = msg.Model.Provider
			isCopilot    = providerID == string(catwalk.InferenceProviderCopilot)
			isConfigured = func() bool { _, ok := cfg.Providers.Get(providerID); return ok }
		)

		// Attempt to import GitHub Copilot tokens from VSCode if available.
		if isCopilot && !isConfigured() && !msg.ReAuthenticate {
			m.com.Workspace.ImportCopilot()
		}

		if !isConfigured() || msg.ReAuthenticate {
			m.dialog.CloseDialog(dialog.ModelsID)
			if cmd := m.openAuthenticationDialog(msg.Provider, msg.Model, msg.ModelType); cmd != nil {
				cmds = append(cmds, cmd)
			}
			break
		}

		if err := m.com.Workspace.UpdatePreferredModel(config.ScopeGlobal, msg.ModelType, msg.Model); err != nil {
			cmds = append(cmds, util.ReportError(err))
		} else if _, ok := cfg.Models[config.SelectedModelTypeSmall]; !ok {
			// Ensure small model is set is unset.
			smallModel := m.com.Workspace.GetDefaultSmallModel(providerID)
			if err := m.com.Workspace.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeSmall, smallModel); err != nil {
				cmds = append(cmds, util.ReportError(err))
			}
		}

		cmds = append(cmds, func() tea.Msg {
			if err := m.com.Workspace.UpdateAgentModel(context.TODO()); err != nil {
				return util.ReportError(err)()
			}

			modelMsg := fmt.Sprintf("%s model changed to %s", msg.ModelType, msg.Model.Model)

			return util.NewInfoMsg(modelMsg)
		})

		m.dialog.CloseDialog(dialog.APIKeyInputID)
		m.dialog.CloseDialog(dialog.OAuthID)
		m.dialog.CloseDialog(dialog.ModelsID)

		if isOnboarding {
			m.setState(uiLanding, uiFocusEditor)
			m.com.Config().SetupAgents()
			if err := m.com.Workspace.InitCoderAgent(context.TODO()); err != nil {
				cmds = append(cmds, util.ReportError(err))
			}
		}
	case dialog.ActionSelectReasoningEffort:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait..."))
			break
		}

		cfg := m.com.Config()
		if cfg == nil {
			cmds = append(cmds, util.ReportError(errors.New("configuration not found")))
			break
		}

		agentCfg, ok := cfg.Agents[config.AgentCoder]
		if !ok {
			cmds = append(cmds, util.ReportError(errors.New("agent configuration not found")))
			break
		}

		currentModel := cfg.Models[agentCfg.Model]
		currentModel.ReasoningEffort = msg.Effort
		if err := m.com.Workspace.UpdatePreferredModel(config.ScopeGlobal, agentCfg.Model, currentModel); err != nil {
			cmds = append(cmds, util.ReportError(err))
			break
		}

		cmds = append(cmds, func() tea.Msg {
			if err := m.com.Workspace.UpdateAgentModel(context.TODO()); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg("Reasoning effort set to " + msg.Effort)
		})
		m.dialog.CloseDialog(dialog.ReasoningID)
	case dialog.ActionPermissionResponse:
		m.dialog.CloseDialog(dialog.PermissionsID)
		switch msg.Action {
		case dialog.PermissionAllow:
			m.com.Workspace.PermissionGrant(msg.Permission)
		case dialog.PermissionAllowForSession:
			m.com.Workspace.PermissionGrantPersistent(msg.Permission)
		case dialog.PermissionDeny:
			m.com.Workspace.PermissionDeny(msg.Permission)
		}

	case dialog.ActionFilePickerSelected:
		cmds = append(cmds, tea.Sequence(
			msg.Cmd(),
			func() tea.Msg {
				m.dialog.CloseDialog(dialog.FilePickerID)
				return nil
			},
			func() tea.Msg {
				fimage.ResetCache()
				return nil
			},
		))

	case dialog.ActionRunCustomCommand:
		if len(msg.Arguments) > 0 && msg.Args == nil {
			m.dialog.CloseFrontDialog()
			argsDialog := dialog.NewArguments(
				m.com,
				"Custom Command Arguments",
				"",
				msg.Arguments,
				msg, // Pass the action as the result
			)
			m.dialog.OpenDialog(argsDialog)
			break
		}
		content := msg.Content
		if msg.Args != nil {
			content = substituteArgs(content, msg.Args)
		}
		cmds = append(cmds, m.sendMessage(content))
		m.dialog.CloseFrontDialog()
	case dialog.ActionRunMCPPrompt:
		if len(msg.Arguments) > 0 && msg.Args == nil {
			m.dialog.CloseFrontDialog()
			title := cmp.Or(msg.Title, "MCP Prompt Arguments")
			argsDialog := dialog.NewArguments(
				m.com,
				title,
				msg.Description,
				msg.Arguments,
				msg, // Pass the action as the result
			)
			m.dialog.OpenDialog(argsDialog)
			break
		}
		cmds = append(cmds, m.runMCPPrompt(msg.ClientID, msg.PromptID, msg.Args))
	case dialog.ActionOpenSearchResult:
		m.dialog.CloseDialog(dialog.SessionSearchID)
		cmds = append(cmds, m.openSearchResult(msg.SearchResult))
	case dialog.ActionOpenDirectory:
		m.dialog.CloseDialog(dialog.OpenDirectoryID)
		cmds = append(cmds, m.openDirectory(msg.Path))
	default:
		cmds = append(cmds, util.CmdHandler(msg))
	}

	return tea.Batch(cmds...)
}

// substituteArgs replaces $ARG_NAME placeholders in content with actual values.
func substituteArgs(content string, args map[string]string) string {
	for name, value := range args {
		placeholder := "$" + name
		content = strings.ReplaceAll(content, placeholder, value)
	}
	return content
}

func (m *UI) openAuthenticationDialog(provider catwalk.Provider, model config.SelectedModel, modelType config.SelectedModelType) tea.Cmd {
	var (
		dlg dialog.Dialog
		cmd tea.Cmd

		isOnboarding = m.state == uiOnboarding
	)

	switch provider.ID {
	case "hyper":
		dlg, cmd = dialog.NewOAuthHyper(m.com, isOnboarding, provider, model, modelType)
	case catwalk.InferenceProviderCopilot:
		dlg, cmd = dialog.NewOAuthCopilot(m.com, isOnboarding, provider, model, modelType)
	default:
		dlg, cmd = dialog.NewAPIKeyInput(m.com, isOnboarding, provider, model, modelType)
	}

	if m.dialog.ContainsDialog(dlg.ID()) {
		m.dialog.BringToFront(dlg.ID())
		return nil
	}

	m.dialog.OpenDialog(dlg)
	return cmd
}

func (m *UI) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	var cmds []tea.Cmd

	handleGlobalKeys := func(msg tea.KeyPressMsg) bool {
		switch {
		case key.Matches(msg, m.keyMap.Help):
			m.status.ToggleHelp()
			m.updateLayoutAndSize()
			return true
		case key.Matches(msg, m.keyMap.Commands):
			if cmd := m.openCommandsDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return true
		case key.Matches(msg, m.keyMap.Models):
			if cmd := m.openModelsDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return true
		case key.Matches(msg, m.keyMap.Sessions):
			if cmd := m.openSessionsDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return true
		case key.Matches(msg, m.keyMap.SessionSearch):
			if cmd := m.openSessionSearchDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return true
		case key.Matches(msg, m.keyMap.OpenDirectory):
			if cmd := m.openOpenDirectoryDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return true
		case key.Matches(msg, m.keyMap.Chat.Details) && m.isCompact:
			m.detailsOpen = !m.detailsOpen
			m.updateLayoutAndSize()
			return true
		case key.Matches(msg, m.keyMap.Chat.TogglePills):
			if m.state == uiChat && m.hasSession() {
				if cmd := m.togglePillsExpanded(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return true
			}
		case key.Matches(msg, m.keyMap.Chat.PillLeft):
			if m.state == uiChat && m.hasSession() && m.pillsExpanded && m.focus != uiFocusEditor {
				if cmd := m.switchPillSection(-1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return true
			}
		case key.Matches(msg, m.keyMap.Chat.PillRight):
			if m.state == uiChat && m.hasSession() && m.pillsExpanded && m.focus != uiFocusEditor {
				if cmd := m.switchPillSection(1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return true
			}
		case key.Matches(msg, m.keyMap.YoloMode):
			yolo := !m.com.Workspace.PermissionSkipRequests()
			m.com.Workspace.PermissionSetSkipRequests(yolo)
			m.setEditorPrompt(yolo)
			return true
		case key.Matches(msg, m.keyMap.ForkSession):
			if m.hasSession() {
				cmds = append(cmds, m.forkSessionToMuxWindow(m.session.ID))
			}
			return true
		case key.Matches(msg, m.keyMap.NewWindow):
			cmds = append(cmds, m.openNewMuxWindow())
			return true
		case key.Matches(msg, m.keyMap.UndoLastTurn):
			if m.isAgentBusy() {
				cmds = append(cmds, util.ReportWarn("Agent is busy, please wait..."))
				return true
			}
			if m.hasSession() {
				cmds = append(cmds, m.undoLastTurn())
			}
			return true
		case key.Matches(msg, m.keyMap.Suspend):
			if m.isAgentBusy() {
				cmds = append(cmds, util.ReportWarn("Agent is busy, please wait..."))
				return true
			}
			cmds = append(cmds, tea.Suspend)
			return true
		}
		return false
	}

	if key.Matches(msg, m.keyMap.Quit) && !m.dialog.ContainsDialog(dialog.QuitID) {
		if m.dialog.ContainsDialog(dialog.JobPreviewID) {
			return m.handleDialogMsg(msg)
		}
		if cmd := m.openQuitDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}

		return tea.Batch(cmds...)
	}

	// Route all messages to dialog if one is open.
	if m.dialog.HasDialogs() {
		return m.handleDialogMsg(msg)
	}

	// Handle cancel key when agent is busy.
	if key.Matches(msg, m.keyMap.Chat.Cancel) {
		if m.isAgentBusy() {
			if cmd := m.cancelAgent(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return tea.Batch(cmds...)
		}
	}

	switch m.state {
	case uiOnboarding:
		return tea.Batch(cmds...)
	case uiInitialize:
		cmds = append(cmds, m.updateInitializeView(msg)...)
		return tea.Batch(cmds...)
	case uiChat, uiLanding:
		switch m.focus {
		case uiFocusEditor:
			// Handle completions if open.
			if m.completionsOpen {
				if msg, ok := m.completions.Update(msg); ok {
					switch msg := msg.(type) {
					case completions.SelectionMsg[completions.FileCompletionValue]:
						cmds = append(cmds, m.insertFileCompletion(msg.Value.Path))
						if !msg.KeepOpen {
							m.closeCompletions()
						}
					case completions.SelectionMsg[completions.ResourceCompletionValue]:
						cmds = append(cmds, m.insertMCPResourceCompletion(msg.Value))
						if !msg.KeepOpen {
							m.closeCompletions()
						}
					case completions.ClosedMsg:
						m.completionsOpen = false
					}
					return tea.Batch(cmds...)
				}
			}

			if m.attachments.Update(msg) {
				return tea.Batch(cmds...)
			}

			switch {
			case key.Matches(msg, m.keyMap.Editor.AddImage):
				if !m.currentModelSupportsImages() {
					break
				}
				if cmd := m.openFilesDialog(); cmd != nil {
					cmds = append(cmds, cmd)
				}

			case key.Matches(msg, m.keyMap.Editor.PasteImage):
				if !m.currentModelSupportsImages() {
					break
				}
				idx := m.pasteIdx()
				cmds = append(cmds, func() tea.Msg { return m.pasteImageFromClipboard(idx) })

			case key.Matches(msg, m.keyMap.Editor.SendMessage):
				prevHeight := m.textarea.Height()
				value := m.textarea.Value()
				if before, ok := strings.CutSuffix(value, "\\"); ok {
					// If the last character is a backslash, remove it and add a newline.
					m.textarea.SetValue(before)
					if cmd := m.handleTextareaHeightChange(prevHeight); cmd != nil {
						cmds = append(cmds, cmd)
					}
					break
				}

				// Otherwise, send the message
				m.textarea.Reset()
				if cmd := m.handleTextareaHeightChange(prevHeight); cmd != nil {
					cmds = append(cmds, cmd)
				}

				value = strings.TrimSpace(value)
				if value == "exit" || value == "quit" {
					return m.openQuitDialog()
				}

				attachments := m.attachments.List()
				m.attachments.Reset()
				if len(value) == 0 && !message.ContainsTextAttachment(attachments) {
					return nil
				}

				m.randomizePlaceholders()
				m.historyReset()
				if value != "" {
					m.promptHistory.messages = append([]string{value}, m.promptHistory.messages...)
				}

				return m.sendMessage(value, attachments...)
			case key.Matches(msg, m.keyMap.Chat.NewSession):
				if !m.hasSession() {
					break
				}
				if m.isAgentBusy() {
					cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before starting a new session..."))
					break
				}
				if cmd := m.newSession(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case msg.Keystroke() == "alt+shift+e":
				if m.isAgentBusy() {
					cmds = append(cmds, util.ReportWarn("Agent is working, please wait..."))
					break
				}
				cmds = append(cmds, m.openEditor(m.textarea.Value()))
			case key.Matches(msg, m.keyMap.Editor.Newline):
				prevHeight := m.textarea.Height()
				m.textarea.InsertRune('\n')
				m.closeCompletions()
				cmds = append(cmds, m.updateTextareaWithPrevHeight(msg, prevHeight))
			case key.Matches(msg, m.keyMap.Editor.HistoryPrev):
				cmd := m.handleHistoryUp(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Editor.HistoryNext):
				cmd := m.handleHistoryDown(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Editor.PrevUserMessage):
				if m.chat.SelectPrevUserMessage() {
					m.chat.ScrollToSelected()
				} else {
					m.chat.ScrollToTop()
				}
				if cmd := m.maybeLoadMoreHistory(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Editor.NextUserMessage):
				if !m.chat.SelectNextUserMessage() {
					m.chat.ScrollToBottom()
					m.chat.SelectLast()
				} else {
					m.chat.ScrollToSelected()
				}
			case key.Matches(msg, m.keyMap.Editor.ScrollToEnd):
				m.chat.ScrollToBottom()
				m.chat.SelectLast()
			case key.Matches(msg, m.keyMap.Editor.Escape):
				cmd := m.handleHistoryEscape(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Editor.Commands) && m.textarea.Value() == "":
				if cmd := m.openCommandsDialog(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Tab):
				if cmd := m.cycleAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			default:
				if handleGlobalKeys(msg) {
					// Handle global keys first before passing to textarea.
					break
				}

				// Check for @ trigger before passing to textarea.
				curValue := m.textarea.Value()
				curIdx := len(curValue)

				// Trigger completions on @.
				if msg.String() == "@" && !m.completionsOpen {
					// Only show if beginning of prompt or after whitespace.
					if curIdx == 0 || (curIdx > 0 && isWhitespace(curValue[curIdx-1])) {
						m.completionsOpen = true
						m.completionsQuery = ""
						m.completionsStartIndex = curIdx
						m.completionsPositionStart = m.completionsPosition()
						depth, limit := m.com.Config().Options.TUI.Completions.Limits()
						cmds = append(cmds, m.completions.Open(depth, limit))
					}
				}

				// remove the details if they are open when user starts typing
				if m.detailsOpen {
					m.detailsOpen = false
					m.updateLayoutAndSize()
				}

				prevHeight := m.textarea.Height()
				cmds = append(cmds, m.updateTextareaWithPrevHeight(msg, prevHeight))

				// Any text modification becomes the current draft.
				m.updateHistoryDraft(curValue)

				// After updating textarea, check if we need to filter completions.
				// Skip filtering on the initial @ keystroke since items are loading async.
				if m.completionsOpen && msg.String() != "@" {
					newValue := m.textarea.Value()
					newIdx := len(newValue)

					// Close completions if cursor moved before start.
					if newIdx <= m.completionsStartIndex {
						m.closeCompletions()
					} else if msg.String() == "space" {
						// Close on space.
						m.closeCompletions()
					} else {
						// Extract current word and filter.
						word := m.textareaWord()
						if strings.HasPrefix(word, "@") {
							m.completionsQuery = word[1:]
							m.completions.Filter(m.completionsQuery)
						} else if m.completionsOpen {
							m.closeCompletions()
						}
					}
				}
			}
		case uiFocusMain:
			switch {
			case key.Matches(msg, m.keyMap.Tab):
				m.focus = uiFocusEditor
				cmds = append(cmds, m.textarea.Focus())
				m.chat.Blur()
			case key.Matches(msg, m.keyMap.Chat.NewSession):
				if !m.hasSession() {
					break
				}
				if m.isAgentBusy() {
					cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before starting a new session..."))
					break
				}
				m.focus = uiFocusEditor
				if cmd := m.newSession(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Chat.Expand):
				m.chat.ToggleExpandedSelectedItem()
			case key.Matches(msg, m.keyMap.Chat.Up):
				if cmd := m.chat.ScrollByAndAnimate(-1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectPrev()
					if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
				if cmd := m.maybeLoadMoreHistory(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Chat.Down):
				if cmd := m.chat.ScrollByAndAnimate(1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectNext()
					if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case key.Matches(msg, m.keyMap.Chat.UpOneItem):
				m.chat.SelectPrev()
				if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Chat.DownOneItem):
				m.chat.SelectNext()
				if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Chat.HalfPageUp):
				if cmd := m.chat.ScrollByAndAnimate(-m.chat.Height() / 2); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectFirstInView()
				if cmd := m.maybeLoadMoreHistory(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Chat.HalfPageDown):
				if cmd := m.chat.ScrollByAndAnimate(m.chat.Height() / 2); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectLastInView()
			case key.Matches(msg, m.keyMap.Chat.PageUp):
				if cmd := m.chat.ScrollByAndAnimate(-m.chat.Height()); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectFirstInView()
				if cmd := m.maybeLoadMoreHistory(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Chat.PageDown):
				if cmd := m.chat.ScrollByAndAnimate(m.chat.Height()); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectLastInView()
			case key.Matches(msg, m.keyMap.Chat.Home):
				if cmd := m.chat.ScrollToTopAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectFirst()
				if cmd := m.maybeLoadMoreHistory(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Chat.End):
				if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectLast()
			default:
				if ok, cmd := m.chat.HandleKeyMsg(msg); ok {
					cmds = append(cmds, cmd)
				} else {
					handleGlobalKeys(msg)
				}
			}
		default:
			handleGlobalKeys(msg)
		}
	default:
		handleGlobalKeys(msg)
	}

	return tea.Sequence(cmds...)
}

// drawHeader draws the header section of the UI.
func (m *UI) drawHeader(scr uv.Screen, area uv.Rectangle) {
	m.header.drawHeader(
		scr,
		area,
		m.session,
		m.isCompact,
		m.detailsOpen,
		area.Dx(),
	)
}

// Draw implements [uv.Drawable] and draws the UI model.
func (m *UI) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	layout := m.generateLayout(area.Dx(), area.Dy())

	if m.layout != layout {
		m.layout = layout
		m.updateSize()
	}

	// Fill the screen with the app background color.
	if !m.isTransparent {
		bgCell := &uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: m.com.Styles.Background}}
		screen.Fill(scr, bgCell)
	} else {
		screen.Clear(scr)
	}

	switch m.state {
	case uiOnboarding:
		m.drawHeader(scr, layout.header)

		// NOTE: Onboarding flow will be rendered as dialogs below, but
		// positioned at the bottom left of the screen.

	case uiInitialize:
		const landingPad = 3
		landingWidth := min(90, max(landingPad*2+20, area.Dx()-4))
		innerWidth := landingWidth - landingPad*2
		landingStr := m.landingView(innerWidth)
		initStr := m.initializeView(innerWidth)
		helpStr := m.com.Styles.Status.Help.Width(innerWidth).Render(m.status.help.View(m.status.helpKm))
		inner := lipgloss.JoinVertical(lipgloss.Left, landingStr, "", initStr, "", helpStr)
		combined := lipgloss.NewStyle().Padding(1, landingPad).Width(landingWidth).Render(inner)
		combinedW := lipgloss.Width(combined)
		combinedH := lipgloss.Height(combined)
		centerArea := layout.header.Union(layout.main).Union(layout.status)
		dialogRect := common.CenterRect(centerArea, combinedW, combinedH)
		uv.NewStyledString(combined).Draw(scr, dialogRect)

		layout.status = image.Rectangle{}

	case uiLanding:
		const landingPad = 3
		landingWidth := min(90, max(landingPad*2+20, area.Dx()-4))
		innerWidth := landingWidth - landingPad*2
		landingStr := m.landingView(innerWidth)

		var belowLanding string
		if m.loadingSession {
			belowLanding = m.landingLoadingView(innerWidth)
		} else {
			belowLanding = m.renderEditorView(innerWidth)
		}

		helpStr := m.com.Styles.Status.Help.Width(innerWidth).Render(m.status.help.View(m.status.helpKm))
		parts := []string{landingStr, ""}
		if m.pillsView != "" {
			parts = append(parts, m.pillsView)
		}
		parts = append(parts, belowLanding, "", helpStr)
		inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
		combined := lipgloss.NewStyle().Padding(1, landingPad).Width(landingWidth).Render(inner)
		combinedW := lipgloss.Width(combined)
		combinedH := lipgloss.Height(combined)
		centerArea := layout.header.Union(layout.main).Union(layout.status)
		dialogRect := common.CenterRect(centerArea, combinedW, combinedH)
		uv.NewStyledString(combined).Draw(scr, dialogRect)

		if !m.loadingSession {
			landingH := lipgloss.Height(landingStr)
			editorH := lipgloss.Height(belowLanding)
			pillsH := 0
			if m.pillsView != "" {
				pillsH = lipgloss.Height(m.pillsView)
			}
			editorTop := dialogRect.Min.Y + 1 + landingH + 1 + pillsH
			m.landingEditorRect = image.Rect(
				dialogRect.Min.X+landingPad,
				editorTop,
				dialogRect.Max.X-landingPad,
				editorTop+editorH,
			)
		}

		// Track MCP item positions on landing page for click handling.
		mcpInfoStr := m.landingMCPInfo(innerWidth)
		if mcpInfoStr != "" {
			modelInfo := m.modelInfo(innerWidth)
			// In landingView: logo + blank + divider + blank + cwd + modelInfo + blank + mcpInfo
			crushLogoH := lipgloss.Height(logo.LandingRender(m.com.Styles, m.com.Styles.LogoTitleColorA, m.com.Styles.LogoTitleColorB))
			cwdH := 1
			modelInfoH := lipgloss.Height(modelInfo)
			// logo + blank(1) + divider(1) + blank(1) + cwd + modelInfo + blank(1) + mcpInfo
			mcpOffsetInLanding := crushLogoH + 1 + 1 + 1 + cwdH + modelInfoH + 1
			mcpH := lipgloss.Height(mcpInfoStr)
			mcpTopY := dialogRect.Min.Y + 1 + mcpOffsetInLanding // +1 for padding top
			sortedMCPs := m.com.Config().MCP.Sorted()
			m.mcpItemRects = m.mcpItemRects[:0]
			for i, entry := range sortedMCPs {
				if i >= mcpH {
					break
				}
				m.mcpItemRects = append(m.mcpItemRects, mcpClickTarget{
					Name: entry.Name,
					Rect: image.Rect(
						dialogRect.Min.X+landingPad,
						mcpTopY+i,
						dialogRect.Max.X-landingPad,
						mcpTopY+i+1,
					),
				})
			}
		} else {
			m.mcpItemRects = m.mcpItemRects[:0]
		}

		layout.status = image.Rectangle{}

	case uiChat:
		if m.isCompact {
			m.drawHeader(scr, layout.header)
		} else {
			m.drawSidebar(scr, layout.sidebar)
		}

		m.chat.Draw(scr, layout.main)
		if layout.pills.Dy() > 0 && m.pillsView != "" {
			uv.NewStyledString(m.pillsView).Draw(scr, layout.pills)
		}

		editorWidth := layout.editor.Dx()
		editor := uv.NewStyledString(m.renderEditorView(editorWidth))
		editor.Draw(scr, layout.editor)

		// Draw details overlay in compact mode when open
		if m.isCompact && m.detailsOpen {
			m.drawSessionDetails(scr, layout.sessionDetails)
		}
	}

	isOnboarding := m.state == uiOnboarding

	// Add status and help layer
	m.status.SetHideHelp(isOnboarding)
	m.status.Draw(scr, layout.status)

	// Draw completions popup if open
	if !isOnboarding && m.completionsOpen && m.completions.HasItems() {
		w, h := m.completions.Size()
		x := m.completionsPositionStart.X
		y := m.completionsPositionStart.Y - h

		screenW := area.Dx()
		if x+w > screenW {
			x = screenW - w
		}
		x = max(0, x)
		y = max(0, y+1) // Offset for attachments row

		completionsView := uv.NewStyledString(m.completions.Render())
		completionsView.Draw(scr, image.Rectangle{
			Min: image.Pt(x, y),
			Max: image.Pt(x+w, y+h),
		})
	}

	// Debugging rendering (visually see when the tui rerenders)
	if os.Getenv("CRUSH_UI_DEBUG") == "true" {
		debugView := lipgloss.NewStyle().Background(lipgloss.ANSIColor(rand.Intn(256))).Width(4).Height(2)
		debug := uv.NewStyledString(debugView.String())
		debug.Draw(scr, image.Rectangle{
			Min: image.Pt(4, 1),
			Max: image.Pt(8, 3),
		})
	}

	// This needs to come last to overlay on top of everything. We always pass
	// the full screen bounds because the dialogs will position themselves
	// accordingly.
	if m.dialog.HasDialogs() {
		return m.dialog.Draw(scr, scr.Bounds())
	}

	switch m.focus {
	case uiFocusEditor:
		if m.state == uiInitialize {
			return nil
		}
		if m.layout.editor.Dy() <= 0 {
			// Don't show cursor if editor is not visible
			return nil
		}
		if m.detailsOpen && m.isCompact {
			// Don't show cursor if details overlay is open
			return nil
		}

		if m.textarea.Focused() {
			cur := m.textarea.Cursor()
			editorRect := m.layout.editor
			if m.state == uiLanding {
				editorRect = m.landingEditorRect
			}
			cur.X += editorRect.Min.X     // Adjust for sidebar + app margins
			cur.Y += editorRect.Min.Y + 1 // Offset for attachments row
			return cur
		}
	}
	return nil
}

// View renders the UI model's view.
func (m *UI) View() tea.View {
	var v tea.View
	v.AltScreen = true
	if !m.isTransparent {
		v.BackgroundColor = m.com.Styles.Background
	}
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = m.caps.ReportFocusEvents
	v.WindowTitle = "crush " + home.Short(m.com.Workspace.WorkingDir())

	canvas := uv.NewScreenBuffer(m.width, m.height)
	v.Cursor = m.Draw(canvas, canvas.Bounds())

	content := strings.ReplaceAll(canvas.Render(), "\r\n", "\n") // normalize newlines
	contentLines := strings.Split(content, "\n")
	for i, line := range contentLines {
		// Trim trailing spaces for concise rendering
		contentLines[i] = strings.TrimRight(line, " ")
	}

	content = strings.Join(contentLines, "\n")

	v.Content = content
	if m.progressBarEnabled && m.sendProgressBar && m.isAgentBusy() {
		// HACK: use a random percentage to prevent ghostty from hiding it
		// after a timeout.
		v.ProgressBar = tea.NewProgressBar(tea.ProgressBarIndeterminate, rand.Intn(100))
	}

	return v
}

// ShortHelp implements [help.KeyMap].
func (m *UI) ShortHelp() []key.Binding {
	var binds []key.Binding
	k := &m.keyMap
	commands := k.Commands
	if m.focus == uiFocusEditor && m.textarea.Value() == "" {
		commands.SetHelp("/ or ctrl+p", "commands")
	}

	switch m.state {
	case uiInitialize:
		binds = append(binds, k.Quit)
	case uiChat:
		if m.isAgentBusy() {
			cancelBinding := k.Chat.Cancel
			if m.isCanceling {
				cancelBinding.SetHelp("ctrl+g", "press again to cancel")
			} else if m.com.Workspace.AgentQueuedPrompts(m.session.ID) > 0 {
				cancelBinding.SetHelp("ctrl+g", "clear queue")
			}
			binds = append(binds, cancelBinding)
		}

		binds = append(binds,
			commands,
			k.Models,
			k.Editor.Newline,
			k.Editor.PrevUserMessage,
			k.Editor.ScrollToEnd,
		)
	default:
		binds = append(binds,
			commands,
			k.Models,
			k.Editor.Newline,
		)
	}

	binds = append(binds,
		k.Quit,
		k.Help,
	)

	return binds
}

// FullHelp implements [help.KeyMap].
func (m *UI) FullHelp() [][]key.Binding {
	var binds [][]key.Binding
	k := &m.keyMap
	help := k.Help
	help.SetHelp("ctrl+/", "less")
	hasAttachments := len(m.attachments.List()) > 0
	hasSession := m.hasSession()
	commands := k.Commands
	if m.focus == uiFocusEditor && m.textarea.Value() == "" {
		commands.SetHelp("/ or ctrl+p", "commands")
	}

	switch m.state {
	case uiInitialize:
		binds = append(binds,
			[]key.Binding{
				k.Quit,
			})
	case uiChat:
		if m.isAgentBusy() {
			cancelBinding := k.Chat.Cancel
			if m.isCanceling {
				cancelBinding.SetHelp("ctrl+g", "press again to cancel")
			} else if m.com.Workspace.AgentQueuedPrompts(m.session.ID) > 0 {
				cancelBinding.SetHelp("ctrl+g", "clear queue")
			}
			binds = append(binds, []key.Binding{cancelBinding})
		}

		mainBinds := []key.Binding{
			commands,
			k.Models,
			k.Sessions,
		}
		if hasSession {
			mainBinds = append(mainBinds, k.Chat.NewSession)
		}
		binds = append(binds, mainBinds)

		switch m.focus {
		case uiFocusEditor:
			editorBinds := []key.Binding{
				k.Editor.Newline,
				k.Editor.MentionFile,
				k.Editor.OpenEditor,
			}
			if m.currentModelSupportsImages() {
				editorBinds = append(editorBinds, k.Editor.AddImage, k.Editor.PasteImage)
			}
			binds = append(binds, editorBinds)
			binds = append(binds,
				[]key.Binding{
					k.Editor.PrevUserMessage,
					k.Editor.NextUserMessage,
					k.Editor.ScrollToEnd,
				},
			)
			if hasAttachments {
				binds = append(binds,
					[]key.Binding{
						k.Editor.AttachmentDeleteMode,
						k.Editor.DeleteAllAttachments,
						k.Editor.Escape,
					},
				)
			}
		case uiFocusMain:
			binds = append(binds,
				[]key.Binding{
					k.Chat.UpDown,
					k.Chat.UpDownOneItem,
					k.Chat.PageUp,
					k.Chat.PageDown,
				},
				[]key.Binding{
					k.Chat.HalfPageUp,
					k.Chat.HalfPageDown,
					k.Chat.Home,
					k.Chat.End,
				},
				[]key.Binding{
					k.Chat.Copy,
					k.Chat.ClearHighlight,
				},
			)
			if m.pillsExpanded && hasIncompleteTodos(m.session.Todos) && m.promptQueue > 0 {
				binds = append(binds, []key.Binding{k.Chat.PillLeft})
			}
		}
	default:
		binds = append(binds,
			[]key.Binding{
				commands,
				k.Models,
				k.Sessions,
			},
		)
		editorBinds := []key.Binding{
			k.Editor.Newline,
			k.Editor.MentionFile,
			k.Editor.OpenEditor,
		}
		if m.currentModelSupportsImages() {
			editorBinds = append(editorBinds, k.Editor.AddImage, k.Editor.PasteImage)
		}
		binds = append(binds, editorBinds)
		if hasAttachments {
			binds = append(binds,
				[]key.Binding{
					k.Editor.AttachmentDeleteMode,
					k.Editor.DeleteAllAttachments,
					k.Editor.Escape,
				},
			)
		}
	}

	binds = append(binds,
		[]key.Binding{
			help,
			k.Quit,
		},
	)

	return binds
}

func (m *UI) currentModelSupportsImages() bool {
	cfg := m.com.Config()
	if cfg == nil {
		return false
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return false
	}
	model := cfg.GetModelByType(agentCfg.Model)
	return model != nil && model.SupportsImages
}

// toggleCompactMode toggles compact mode between uiChat and uiChatCompact states.
func (m *UI) toggleCompactMode() tea.Cmd {
	m.forceCompactMode = !m.forceCompactMode

	err := m.com.Workspace.SetCompactMode(config.ScopeGlobal, m.forceCompactMode)
	if err != nil {
		return util.ReportError(err)
	}

	m.updateLayoutAndSize()

	return nil
}

// updateLayoutAndSize updates the layout and sizes of UI components.
func (m *UI) updateLayoutAndSize() {
	// Determine if we should be in compact mode
	if m.state == uiChat {
		if m.forceCompactMode {
			m.isCompact = true
		} else if m.width < compactModeWidthBreakpoint || m.height < compactModeHeightBreakpoint {
			m.isCompact = true
		} else {
			m.isCompact = false
		}
	}

	// First pass sizes components from the current textarea height.
	m.layout = m.generateLayout(m.width, m.height)
	prevHeight := m.textarea.Height()
	m.updateSize()

	// SetWidth can change textarea height due to soft-wrap recalculation.
	// If that happens, run one reconciliation pass with the new height.
	if m.textarea.Height() != prevHeight {
		m.layout = m.generateLayout(m.width, m.height)
		m.updateSize()
	}

	// Expose the chat content area to dialogs via Common.
	m.com.ChatArea = image.Rect(
		m.layout.main.Min.X,
		m.layout.main.Min.Y,
		m.layout.main.Max.X,
		m.layout.editor.Max.Y,
	)
}

// handleTextareaHeightChange checks whether the textarea height changed and,
// if so, recalculates the layout. When the chat is in follow mode it keeps
// the view scrolled to the bottom. The returned command, if non-nil, must be
// batched by the caller.
func (m *UI) handleTextareaHeightChange(prevHeight int) tea.Cmd {
	if m.textarea.Height() == prevHeight {
		return nil
	}
	m.updateLayoutAndSize()
	if m.state == uiChat && m.chat.Follow() {
		return m.chat.ScrollToBottomAndAnimate()
	}
	return nil
}

// updateTextarea updates the textarea for msg and then reconciles layout if
// the textarea height changed as a result.
func (m *UI) updateTextarea(msg tea.Msg) tea.Cmd {
	return m.updateTextareaWithPrevHeight(msg, m.textarea.Height())
}

// updateTextareaWithPrevHeight is for cases when the height of the layout may
// have changed.
//
// Particularly, it's for cases where the textarea changes before
// textarea.Update is called (for example, SetValue, Reset, and InsertRune). We
// pass the height from before those changes took place so we can compare
// "before" vs "after" sizing and recalculate the layout if the textarea grew
// or shrank.
func (m *UI) updateTextareaWithPrevHeight(msg tea.Msg, prevHeight int) tea.Cmd {
	ta, cmd := m.textarea.Update(msg)
	m.textarea = ta
	return tea.Batch(cmd, m.handleTextareaHeightChange(prevHeight))
}

// updateSize updates the sizes of UI components based on the current layout.
func (m *UI) updateSize() {
	// Set status width
	if m.state == uiLanding || m.state == uiInitialize {
		m.status.SetWidth(m.layout.editor.Dx())
	} else {
		m.status.SetWidth(m.layout.status.Dx())
	}

	m.chat.SetSize(m.layout.main.Dx(), m.layout.main.Dy())
	m.textarea.MaxHeight = TextareaMaxHeight
	m.textarea.SetWidth(m.layout.editor.Dx())
	m.renderPills()

	// Handle different app states
	switch m.state {
	case uiChat:
		if !m.isCompact {
			m.cacheSidebarLogo(m.layout.sidebar.Dx())
		}
	}
}

// generateLayout calculates the layout rectangles for all UI components based
// on the current UI state and terminal dimensions.
func (m *UI) generateLayout(w, h int) uiLayout {
	// The screen area we're working with
	area := image.Rect(0, 0, w, h)

	// The help height
	helpHeight := 1
	// The editor height: textarea height + margin for attachments and bottom spacing.
	editorHeight := m.textarea.Height() + editorHeightMargin
	// The sidebar width as a percentage of the total width.
	const sidebarPercent = 25
	// The header height
	const landingHeaderHeight = 4

	var helpKeyMap help.KeyMap = m
	if m.status != nil && m.status.ShowingAll() {
		for _, row := range helpKeyMap.FullHelp() {
			helpHeight = max(helpHeight, len(row))
		}
	}

	// Add app margins
	appHelpSplit := layoutpkg.Vertical(layoutpkg.Len(area.Dy()-helpHeight), layoutpkg.Fill(1)).Split(area)
	appRect, helpRect := appHelpSplit[0], appHelpSplit[1]
	appRect.Min.Y += 1
	appRect.Max.Y -= 1
	helpRect.Min.Y -= 1
	appRect.Min.X += 1
	appRect.Max.X -= 1

	if slices.Contains([]uiState{uiOnboarding, uiInitialize, uiLanding}, m.state) {
		// extra padding on left and right for these states
		appRect.Min.X += 1
		appRect.Max.X -= 1
	}

	uiLayout := uiLayout{
		area:   area,
		status: helpRect,
	}

	// Handle different app states
	switch m.state {
	case uiOnboarding:
		// Layout
		//
		// header
		// ------
		// main
		// ------
		// help

		hmSplit := layoutpkg.Vertical(layoutpkg.Len(landingHeaderHeight), layoutpkg.Fill(1)).Split(appRect)
		headerRect, mainRect := hmSplit[0], hmSplit[1]
		uiLayout.header = headerRect
		uiLayout.main = mainRect

	case uiInitialize, uiLanding:
		uiLayout.header = appRect
		uiLayout.main = appRect
		const landingPad = 3
		innerWidth := min(90, max(landingPad*2+20, appRect.Dx()-4)) - landingPad*2
		uiLayout.editor = image.Rect(
			appRect.Min.X, appRect.Max.Y-editorHeight,
			appRect.Min.X+innerWidth, appRect.Max.Y,
		)

	case uiChat:
		if m.isCompact {
			// Layout
			//
			// compact-header
			// ------
			// main
			// ------
			// editor
			// ------
			// help
			const compactHeaderHeight = 1
			hmSplit := layoutpkg.Vertical(layoutpkg.Len(compactHeaderHeight), layoutpkg.Fill(1)).Split(appRect)
			headerRect, mainRect := hmSplit[0], hmSplit[1]
			detailsHeight := min(sessionDetailsMaxHeight, area.Dy()-1) // One row for the header
			sessionDetailsArea := layoutpkg.Vertical(layoutpkg.Len(detailsHeight), layoutpkg.Fill(1)).Split(appRect)[0]
			uiLayout.sessionDetails = sessionDetailsArea
			uiLayout.sessionDetails.Min.Y += compactHeaderHeight // adjust for header
			// Add one line gap between header and main content
			mainRect.Min.Y += 1
			meSplit := layoutpkg.Vertical(layoutpkg.Len(mainRect.Dy()-editorHeight), layoutpkg.Fill(1)).Split(mainRect)
			mainRect, editorRect := meSplit[0], meSplit[1]
			mainRect.Max.X -= 1 // Add padding right
			uiLayout.header = headerRect
			pillsHeight := m.pillsAreaHeight()
			if pillsHeight > 0 {
				pillsHeight = min(pillsHeight, mainRect.Dy())
				cpSplit := layoutpkg.Vertical(layoutpkg.Len(mainRect.Dy()-pillsHeight), layoutpkg.Fill(1)).Split(mainRect)
				chatRect, pillsRect := cpSplit[0], cpSplit[1]
				uiLayout.main = chatRect
				uiLayout.pills = pillsRect
			} else {
				uiLayout.main = mainRect
			}
			// Add bottom margin to main
			uiLayout.main.Max.Y -= 1
			uiLayout.editor = editorRect
		} else {
			// Layout
			//
			// ----|--------
			//     |  main
			// side|--------
			//     | editor
			// ----|--------
			//     |  help

			scSplit := layoutpkg.Horizontal(layoutpkg.Percent(sidebarPercent), layoutpkg.Fill(1)).Split(appRect)
			sideRect, contentRect := scSplit[0], scSplit[1]
			sideRect.Max.X -= 1    // padding right for visual separation
			contentRect.Min.X += 1 // padding left for visual separation

			// Sidebar spans full height including help bar area.
			fullSideRect := sideRect
			fullSideRect.Min.Y = uiLayout.area.Min.Y
			fullSideRect.Max.Y = uiLayout.area.Max.Y
			uiLayout.sidebar = fullSideRect

			// Narrow the status bar to only span the right content area.
			uiLayout.status.Min.X = contentRect.Min.X
			uiLayout.status.Max.X = contentRect.Max.X

			meSplit := layoutpkg.Vertical(layoutpkg.Len(contentRect.Dy()-editorHeight), layoutpkg.Fill(1)).Split(contentRect)
			mainRect, editorRect := meSplit[0], meSplit[1]
			pillsHeight := m.pillsAreaHeight()
			if pillsHeight > 0 {
				pillsHeight = min(pillsHeight, mainRect.Dy())
				cpSplit := layoutpkg.Vertical(layoutpkg.Len(mainRect.Dy()-pillsHeight), layoutpkg.Fill(1)).Split(mainRect)
				chatRect, pillsRect := cpSplit[0], cpSplit[1]
				uiLayout.main = chatRect
				uiLayout.pills = pillsRect
			} else {
				uiLayout.main = mainRect
			}
			// Add bottom margin to main
			uiLayout.main.Max.Y -= 1
			uiLayout.editor = editorRect
		}
	}

	return uiLayout
}

// uiLayout defines the positioning of UI elements.
type uiLayout struct {
	// area is the overall available area.
	area uv.Rectangle

	// header is the header shown in special cases
	// e.x when the sidebar is collapsed
	// or when in the landing page
	// or in init/config
	header uv.Rectangle

	// main is the area for the main pane. (e.x chat, configure, landing)
	main uv.Rectangle

	// pills is the area for the pills panel.
	pills uv.Rectangle

	// editor is the area for the editor pane.
	editor uv.Rectangle

	// sidebar is the area for the sidebar.
	sidebar uv.Rectangle

	// status is the area for the status view.
	status uv.Rectangle

	// session details is the area for the session details overlay in compact mode.
	sessionDetails uv.Rectangle
}

func (m *UI) openEditor(value string) tea.Cmd {
	tmpfile, err := os.CreateTemp("", "msg_*.md")
	if err != nil {
		return util.ReportError(err)
	}
	tmpPath := tmpfile.Name()
	defer tmpfile.Close() //nolint:errcheck
	if _, err := tmpfile.WriteString(value); err != nil {
		_ = os.Remove(tmpPath)
		return util.ReportError(err)
	}
	cmd, err := editor.Command(
		"crush",
		tmpPath,
		editor.AtPosition(
			m.textarea.Line()+1,
			m.textarea.Column()+1,
		),
	)
	if err != nil {
		_ = os.Remove(tmpPath)
		return util.ReportError(err)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer func() {
			_ = os.Remove(tmpPath)
		}()

		if err != nil {
			return util.ReportError(err)
		}
		content, err := os.ReadFile(tmpPath)
		if err != nil {
			return util.ReportError(err)
		}
		if len(content) == 0 {
			return util.ReportWarn("Message is empty")
		}
		return openEditorMsg{
			Text: strings.TrimSpace(string(content)),
		}
	})
}

// setEditorPrompt configures the textarea prompt function based on whether
// yolo mode is enabled.
func (m *UI) setEditorPrompt(yolo bool) {
	if yolo {
		m.textarea.SetPromptFunc(4, m.yoloPromptFunc)
		return
	}
	m.textarea.SetPromptFunc(4, m.normalPromptFunc)
}

// normalPromptFunc returns the normal editor prompt style ("  > " on first
// line, "::: " on subsequent lines).
func (m *UI) normalPromptFunc(info textarea.PromptInfo) string {
	t := m.com.Styles
	if info.LineNumber == 0 {
		if info.Focused {
			return "  > "
		}
		return "::: "
	}
	if info.Focused {
		return t.EditorPromptNormalFocused.Render()
	}
	return t.EditorPromptNormalBlurred.Render()
}

// yoloPromptFunc returns the yolo mode editor prompt style with warning icon
// and colored dots.
func (m *UI) yoloPromptFunc(info textarea.PromptInfo) string {
	t := m.com.Styles
	if info.LineNumber == 0 {
		if info.Focused {
			return t.EditorPromptYoloIconFocused.Render()
		} else {
			return t.EditorPromptYoloIconBlurred.Render()
		}
	}
	if info.Focused {
		return t.EditorPromptYoloDotsFocused.Render()
	}
	return t.EditorPromptYoloDotsBlurred.Render()
}

// closeCompletions closes the completions popup and resets state.
func (m *UI) closeCompletions() {
	m.completionsOpen = false
	m.completionsQuery = ""
	m.completionsStartIndex = 0
	m.completions.Close()
}

// insertCompletionText replaces the @query in the textarea with the given text.
// Returns false if the replacement cannot be performed.
func (m *UI) insertCompletionText(text string) bool {
	value := m.textarea.Value()
	if m.completionsStartIndex > len(value) {
		return false
	}

	word := m.textareaWord()
	endIdx := min(m.completionsStartIndex+len(word), len(value))
	newValue := value[:m.completionsStartIndex] + text + value[endIdx:]
	m.textarea.SetValue(newValue)
	m.textarea.MoveToEnd()
	m.textarea.InsertRune(' ')
	return true
}

// insertFileCompletion inserts the selected file path into the textarea,
// replacing the @query, and adds the file as an attachment.
func (m *UI) insertFileCompletion(path string) tea.Cmd {
	prevHeight := m.textarea.Height()
	if !m.insertCompletionText(path) {
		return nil
	}
	heightCmd := m.handleTextareaHeightChange(prevHeight)

	hasSession := m.hasSession()
	var sessionID string
	if hasSession {
		sessionID = m.session.ID
	}
	sessionReads := slices.Clone(m.sessionFileReads)

	fileCmd := func() tea.Msg {
		absPath, _ := filepath.Abs(path)

		if hasSession {
			// Skip attachment if file was already read and hasn't been modified.
			lastRead := m.com.Workspace.FileTrackerLastReadTime(context.Background(), sessionID, absPath)
			if !lastRead.IsZero() {
				if info, err := os.Stat(path); err == nil && !info.ModTime().After(lastRead) {
					return nil
				}
			}
		} else if slices.Contains(sessionReads, absPath) {
			return nil
		}

		// Add file as attachment.
		content, err := os.ReadFile(path)
		if err != nil {
			// If it fails, let the LLM handle it later.
			return nil
		}

		return insertFileCompletionMsg{
			AbsPath: absPath,
			Attachment: message.Attachment{
				FilePath: path,
				FileName: filepath.Base(path),
				MimeType: mimeOf(content),
				Content:  content,
			},
		}
	}
	return tea.Batch(heightCmd, fileCmd)
}

// insertMCPResourceCompletion inserts the selected resource into the textarea,
// replacing the @query, and adds the resource as an attachment.
func (m *UI) insertMCPResourceCompletion(item completions.ResourceCompletionValue) tea.Cmd {
	displayText := cmp.Or(item.Title, item.URI)

	prevHeight := m.textarea.Height()
	if !m.insertCompletionText(displayText) {
		return nil
	}
	heightCmd := m.handleTextareaHeightChange(prevHeight)

	resourceCmd := func() tea.Msg {
		contents, err := m.com.Workspace.ReadMCPResource(
			context.Background(),
			item.MCPName,
			item.URI,
		)
		if err != nil {
			slog.Warn("Failed to read MCP resource", "uri", item.URI, "error", err)
			return nil
		}
		if len(contents) == 0 {
			return nil
		}

		content := contents[0]
		var data []byte
		if content.Text != "" {
			data = []byte(content.Text)
		} else if len(content.Blob) > 0 {
			data = content.Blob
		}
		if len(data) == 0 {
			return nil
		}

		mimeType := item.MIMEType
		if mimeType == "" && content.MIMEType != "" {
			mimeType = content.MIMEType
		}
		if mimeType == "" {
			mimeType = "text/plain"
		}

		return message.Attachment{
			FilePath: item.URI,
			FileName: displayText,
			MimeType: mimeType,
			Content:  data,
		}
	}
	return tea.Batch(heightCmd, resourceCmd)
}

// completionsPosition returns the X and Y position for the completions popup.
func (m *UI) completionsPosition() image.Point {
	cur := m.textarea.Cursor()
	editorRect := m.layout.editor
	if m.state == uiLanding {
		editorRect = m.landingEditorRect
	}
	if cur == nil {
		return image.Point{
			X: editorRect.Min.X,
			Y: editorRect.Min.Y,
		}
	}
	return image.Point{
		X: cur.X + editorRect.Min.X,
		Y: editorRect.Min.Y + cur.Y,
	}
}

// textareaWord returns the current word at the cursor position.
func (m *UI) textareaWord() string {
	return m.textarea.Word()
}

// isWhitespace returns true if the byte is a whitespace character.
func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// isAgentBusy returns true if the agent coordinator exists and is currently
// busy processing a request.
func (m *UI) isAgentBusy() bool {
	return m.com.Workspace.AgentIsReady() &&
		m.com.Workspace.AgentIsBusy()
}

// completeRemainingTodos marks any in-progress or pending todos as completed
// and persists the change. Called when the agent finishes so stale todos
// don't linger in the UI.
func (m *UI) completeRemainingTodos() {
	changed := false
	for i := range m.session.Todos {
		if m.session.Todos[i].Status != session.TodoStatusCompleted {
			m.session.Todos[i].Status = session.TodoStatusCompleted
			changed = true
		}
	}
	if changed {
		m.renderPills()
		go func() {
			_, _ = m.com.Workspace.SaveSession(context.TODO(), *m.session)
		}()
	}
}

// hasSession returns true if there is an active session with a valid ID.
func (m *UI) hasSession() bool {
	return m.session != nil && m.session.ID != ""
}

// forkSessionToMuxWindow forks the given session and opens the new session in
// a new terminal multiplexer window.
func (m *UI) forkSessionToMuxWindow(sessionID string) tea.Cmd {
	return func() tea.Msg {
		if !m.com.Mux.Available() {
			return util.NewInfoMsg("No terminal multiplexer detected (tmux/psmux required)")
		}
		newSess, err := m.com.Workspace.ForkSession(context.TODO(), sessionID)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		exe, err := os.Executable()
		if err != nil {
			return util.NewErrorMsg(err)
		}
		cwd, _ := os.Getwd()
		if err := m.com.Mux.NewWindow(cwd, exe, "--session", newSess.ID); err != nil {
			return util.NewErrorMsg(err)
		}
		return util.NewInfoMsg("Session forked to new window")
	}
}

// undoLastTurn deletes the last user message and all subsequent messages
// (assistant replies, tool calls/results) from the current session.
func (m *UI) undoLastTurn() tea.Cmd {
	sessionID := m.session.ID
	return func() tea.Msg {
		ctx := context.TODO()
		msgs, err := m.com.Workspace.ListMessages(ctx, sessionID)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if len(msgs) == 0 {
			return nil
		}

		lastUserIdx := -1
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == message.User && !msgs[i].IsSummaryMessage {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx < 0 {
			return util.NewInfoMsg("No user message to undo")
		}

		for i := len(msgs) - 1; i >= lastUserIdx; i-- {
			if err := m.com.Workspace.DeleteMessage(ctx, msgs[i].ID); err != nil {
				return util.NewErrorMsg(err)
			}
		}
		return util.NewInfoMsg("Last turn undone")
	}
}

// openNewMuxWindow opens a fresh crush instance in a new mux window,
// using the current working directory.
func (m *UI) openNewMuxWindow() tea.Cmd {
	return func() tea.Msg {
		if !m.com.Mux.Available() {
			return util.NewInfoMsg("No terminal multiplexer detected (tmux/psmux required)")
		}
		exe, err := os.Executable()
		if err != nil {
			return util.NewErrorMsg(err)
		}
		cwd, _ := os.Getwd()
		if err := m.com.Mux.NewWindow(cwd, exe); err != nil {
			return util.NewErrorMsg(err)
		}
		return util.NewInfoMsg("Opened crush in new window")
	}
}

// syncTmuxSessionID sets (or clears) the @crush_session pane user option so
// that external scripts can discover which session this instance is using.
// It is a silent no-op when no multiplexer is available.
func (m *UI) syncTmuxSessionID() {
	if !m.com.Mux.Available() {
		return
	}
	var val string
	if m.session != nil && m.session.ID != "" {
		val = m.session.ID
	}
	m.com.Mux.SetPaneOption("@crush_session", val)
}

// syncTmuxPaneTitle updates the tmux pane title to reflect the current
// session title. Called when a session is loaded or its title changes.
func (m *UI) syncTmuxPaneTitle() {
	if !m.com.Mux.Available() || m.session == nil {
		return
	}
	title := m.session.ShortTitle
	if title == "" {
		title = m.session.Title
	}
	if title == "" || title == "New Session" {
		return
	}
	m.com.Mux.SetPaneTitle(title)
}

// mimeOf detects the MIME type of the given content.
func mimeOf(content []byte) string {
	mimeBufferSize := min(512, len(content))
	return http.DetectContentType(content[:mimeBufferSize])
}

var readyPlaceholders = [...]string{
	"Ready!",
	"Ready...",
	"Ready?",
	"Ready for instructions",
}

var workingPlaceholders = [...]string{
	"Working!",
	"Working...",
	"Brrrrr...",
	"Prrrrrrrr...",
	"Processing...",
	"Thinking...",
}

// randomizePlaceholders selects random placeholder text for the textarea's
// ready and working states.
func (m *UI) randomizePlaceholders() {
	m.workingPlaceholder = workingPlaceholders[rand.Intn(len(workingPlaceholders))]
	m.readyPlaceholder = readyPlaceholders[rand.Intn(len(readyPlaceholders))]
}

// renderEditorView renders the editor view with attachments if any.
func (m *UI) renderEditorView(width int) string {
	var attachmentsView string
	if len(m.attachments.List()) > 0 {
		attachmentsView = m.attachments.Render(width)
	}
	return strings.Join([]string{
		attachmentsView,
		m.textarea.View(),
		"", // margin at bottom of editor
	}, "\n")
}

// cacheSidebarLogo renders and caches the sidebar logo at the specified width.
func (m *UI) cacheSidebarLogo(width int) {
	m.sidebarLogo = renderLogo(m.com.Styles, true, width)
}

// sendMessage sends a message with the given content and attachments.
func (m *UI) sendMessage(content string, attachments ...message.Attachment) tea.Cmd {
	if !m.com.Workspace.AgentIsReady() {
		return util.ReportError(fmt.Errorf("coder agent is not initialized"))
	}

	if !m.hasSession() {
		if m.forceCompactMode {
			m.isCompact = true
		}
		m.setState(uiChat, m.focus)
		content := content
		atts := slices.Clone(attachments)
		return func() tea.Msg {
			newSession, err := m.com.Workspace.CreateSession(context.Background(), "New Session")
			if err != nil {
				return util.InfoMsg{Type: util.InfoTypeError, Msg: err.Error()}
			}
			return sessionCreatedMsg{session: newSession, content: content, attachments: atts}
		}
	}

	return m.sendMessageWithSession(content, attachments...)
}

// sendMessageWithSession sends a message assuming m.session is already set.
func (m *UI) sendMessageWithSession(content string, attachments ...message.Attachment) tea.Cmd {
	var cmds []tea.Cmd
	ctx := context.Background()
	fileReads := slices.Clone(m.sessionFileReads)
	sessionID := m.session.ID
	cmds = append(cmds, func() tea.Msg {
		for _, path := range fileReads {
			m.com.Workspace.FileTrackerRecordRead(ctx, sessionID, path)
			m.com.Workspace.LSPStart(ctx, path)
		}
		return nil
	})

	// Show a placeholder spinner immediately so the user sees feedback
	// before the backend creates the real assistant message.
	placeholder := chat.NewPlaceholderItem(m.com.Styles)
	m.chat.AppendMessages(placeholder)
	cmds = append(cmds, placeholder.StartAnimation())
	if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Capture session ID to avoid race with main goroutine updating m.session.
	agentCmd := func() tea.Msg {
		err := m.com.Workspace.AgentRun(context.Background(), sessionID, content, attachments...)
		if err != nil {
			isCancelErr := errors.Is(err, context.Canceled)
			isPermissionErr := errors.Is(err, permission.ErrorPermissionDenied)
			if isCancelErr || isPermissionErr {
				return removePlaceholderMsg{}
			}
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("%v", err),
			}
		}
		return removePlaceholderMsg{}
	}
	cmds = append(cmds, agentCmd)
	return tea.Batch(cmds...)
}

// cycleAgent switches to the next top-level agent in round-robin order.
func (m *UI) cycleAgent() tea.Cmd {
	if m.com.App == nil || m.com.App.AgentCoordinator == nil {
		return nil
	}
	if m.isAgentBusy() {
		return util.ReportWarn("Agent is working, please wait...")
	}
	agents := config.TopLevelAgents()
	current := m.com.App.AgentCoordinator.ActiveAgent()
	nextIdx := 0
	for i, id := range agents {
		if id == current {
			nextIdx = (i + 1) % len(agents)
			break
		}
	}
	next := agents[nextIdx]
	if err := m.com.App.AgentCoordinator.SetMainAgent(next); err != nil {
		return util.ReportError(err)
	}
	agentCfg := m.com.Config().Agents[next]
	m.renderPills()
	return util.CmdHandler(util.NewInfoMsg("Switched to " + agentCfg.Name + " agent"))
}

const cancelTimerDuration = 2 * time.Second

// cancelTimerCmd creates a command that expires the cancel timer.
func cancelTimerCmd() tea.Cmd {
	return tea.Tick(cancelTimerDuration, func(time.Time) tea.Msg {
		return cancelTimerExpiredMsg{}
	})
}

// cancelAgent handles the cancel key press. The first press sets isCanceling to true
// and starts a timer. The second press (before the timer expires) actually
// cancels the agent.
func (m *UI) cancelAgent() tea.Cmd {
	if !m.hasSession() {
		return nil
	}

	if !m.com.Workspace.AgentIsReady() {
		return nil
	}

	if m.isCanceling {
		// Second ctrl+g press - actually cancel the agent.
		m.isCanceling = false
		m.com.Workspace.AgentCancel(m.session.ID)
		// Stop the spinning todo indicator.
		m.todoIsSpinning = false
		m.renderPills()
		return nil
	}

	// Check if there are queued prompts - if so, clear the queue and
	// simultaneously enter canceling state so the next press cancels the agent.
	if m.com.Workspace.AgentQueuedPrompts(m.session.ID) > 0 {
		m.com.Workspace.AgentClearQueue(m.session.ID)
		m.isCanceling = true
		return cancelTimerCmd()
	}

	// First ctrl+g press - set canceling state and start timer.
	m.isCanceling = true
	return cancelTimerCmd()
}

// openDialog opens a dialog by its ID.
func (m *UI) openDialog(id string) tea.Cmd {
	var cmds []tea.Cmd
	switch id {
	case dialog.SessionsID:
		if cmd := m.openSessionsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ModelsID:
		if cmd := m.openModelsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.CommandsID:
		if cmd := m.openCommandsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ReasoningID:
		if cmd := m.openReasoningDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.FilePickerID:
		if cmd := m.openFilesDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.SessionSearchID:
		if cmd := m.openSessionSearchDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.OpenDirectoryID:
		if cmd := m.openOpenDirectoryDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.QuitID:
		if cmd := m.openQuitDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	default:
		// Unknown dialog
		break
	}
	return tea.Batch(cmds...)
}

// openQuitDialog opens the quit confirmation dialog.
func (m *UI) openQuitDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.QuitID) {
		// Bring to front
		m.dialog.BringToFront(dialog.QuitID)
		return nil
	}

	quitDialog := dialog.NewQuit(m.com)
	m.dialog.OpenDialog(quitDialog)
	return nil
}

// openImagePreviewDialog opens the image preview dialog for the given
// attachment.
func (m *UI) openImagePreviewDialog(att message.Attachment) tea.Cmd {
	m.dialog.CloseDialog(dialog.ImagePreviewID)
	d, cmd := dialog.NewImagePreview(m.com, att, &m.caps)
	m.dialog.OpenDialog(d)
	return cmd
}

// openTextPreviewDialog opens the text preview dialog with the given content.
func (m *UI) openTextPreviewDialog(title, text string) {
	m.dialog.CloseDialog(dialog.TextPreviewID)
	d := dialog.NewTextPreview(m.com, title, text)
	m.dialog.OpenDialog(d)
}

func (m *UI) openDiffPreviewDialog(filePath, oldContent, newContent string) {
	m.dialog.CloseDialog(dialog.DiffPreviewID)
	d := dialog.NewDiffPreview(m.com, filePath, oldContent, newContent)
	m.dialog.OpenDialog(d)
}

func (m *UI) openJobPreviewDialog(msg JobPreviewMsg) tea.Cmd {
	m.dialog.CloseDialog(dialog.JobPreviewID)
	contentFunc := func() dialog.JobPreviewContentResult {
		r := msg.ContentFunc()
		return dialog.JobPreviewContentResult{Content: r.Content, Done: r.Done}
	}
	var killFunc dialog.JobPreviewKillFunc
	if msg.KillFunc != nil {
		killFunc = msg.KillFunc
	}
	d, cmd := dialog.NewJobPreview(m.com, msg.Title, contentFunc, killFunc)
	m.dialog.OpenDialog(d)
	return cmd
}

// openModelsDialog opens the models dialog.
func (m *UI) openModelsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ModelsID) {
		// Bring to front
		m.dialog.BringToFront(dialog.ModelsID)
		return nil
	}

	isOnboarding := m.state == uiOnboarding
	modelsDialog, err := dialog.NewModels(m.com, isOnboarding)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(modelsDialog)

	return nil
}

// openCommandsDialog opens the commands dialog.
func (m *UI) openCommandsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.CommandsID) {
		// Bring to front
		m.dialog.BringToFront(dialog.CommandsID)
		return nil
	}

	var sessionID string
	hasSession := m.session != nil
	if hasSession {
		sessionID = m.session.ID
	}
	hasTodos := hasSession && hasIncompleteTodos(m.session.Todos)
	hasQueue := m.promptQueue > 0

	activeAgent := config.AgentCoder
	if m.com.App != nil && m.com.App.AgentCoordinator != nil {
		activeAgent = m.com.App.AgentCoordinator.ActiveAgent()
	}

	commands, err := dialog.NewCommands(m.com, sessionID, hasSession, hasTodos, hasQueue, m.customCommands, m.mcpPrompts, activeAgent)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(commands)

	return commands.InitialCmd()
}

// openReasoningDialog opens the reasoning effort dialog.
func (m *UI) openReasoningDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ReasoningID) {
		m.dialog.BringToFront(dialog.ReasoningID)
		return nil
	}

	reasoningDialog, err := dialog.NewReasoning(m.com)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(reasoningDialog)
	return nil
}

// openSessionsDialog opens the sessions dialog. If the dialog is already open,
// it brings it to the front. Otherwise, it will list all the sessions and open
// the dialog.
func (m *UI) openSessionsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.SessionsID) {
		// Bring to front
		m.dialog.BringToFront(dialog.SessionsID)
		return nil
	}

	selectedSessionID := ""
	if m.session != nil {
		selectedSessionID = m.session.ID
	}

	dialog, err := dialog.NewSessions(m.com, selectedSessionID)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(dialog)
	return dialog.InitialPreviewCmd()
}

// openSessionSearchDialog opens the cross-project session search dialog.
func (m *UI) openSessionSearchDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.SessionSearchID) {
		m.dialog.BringToFront(dialog.SessionSearchID)
		return nil
	}
	d := dialog.NewSessionSearch(m.com)
	m.dialog.OpenDialog(d)
	return d.InitialSearchCmd()
}

// openOpenDirectoryDialog opens the Open Directory dialog.
func (m *UI) openOpenDirectoryDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.OpenDirectoryID) {
		m.dialog.BringToFront(dialog.OpenDirectoryID)
		return nil
	}
	d := dialog.NewOpenDirectory(m.com)
	m.dialog.OpenDialog(d)
	return nil
}

// openDirectory opens crush in a new mux window at the given directory path.
func (m *UI) openDirectory(path string) tea.Cmd {
	return func() tea.Msg {
		if !m.com.Mux.Available() {
			return util.NewInfoMsg("Requires a terminal multiplexer (tmux/psmux)")
		}
		exe, err := os.Executable()
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if err := m.com.Mux.NewWindow(path, exe); err != nil {
			return util.NewErrorMsg(err)
		}
		return nil
	}
}

// openSearchResult opens a session from a cross-project search result.
// If the session is already open in a mux pane, it switches to that pane.
// Otherwise it opens the session in a new mux window.
func (m *UI) openSearchResult(result search.SearchResult) tea.Cmd {
	// If already open in a mux pane, switch to it
	if result.Active && m.com.Mux.SelectPaneBySession(result.SessionID) {
		return nil
	}
	// Open in a new mux window
	return func() tea.Msg {
		if !m.com.Mux.Available() {
			return util.NewInfoMsg("Requires a terminal multiplexer (tmux/psmux)")
		}
		exe, err := os.Executable()
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if err := m.com.Mux.NewWindow(result.AbsProjectPath, exe, "--session", result.SessionID); err != nil {
			return util.NewErrorMsg(err)
		}
		return nil
	}
}

// openFilesDialog opens the file picker dialog.
func (m *UI) openFilesDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.FilePickerID) {
		// Bring to front
		m.dialog.BringToFront(dialog.FilePickerID)
		return nil
	}

	filePicker, cmd := dialog.NewFilePicker(m.com)
	filePicker.SetImageCapabilities(&m.caps)
	m.dialog.OpenDialog(filePicker)

	return cmd
}

// openPermissionsDialog opens the permissions dialog for a permission request.
func (m *UI) openPermissionsDialog(perm permission.PermissionRequest) tea.Cmd {
	// If the dialog for this exact request is already open, don't recreate it
	// — the retry ticker in permission.Service re-publishes every 3s and
	// recreating the dialog would reset the user's selection state.
	if d := m.dialog.Dialog(dialog.PermissionsID); d != nil {
		if permDlg, ok := d.(*dialog.Permissions); ok && permDlg.RequestID() == perm.ID {
			return nil
		}
	}

	// Close any existing permissions dialog first.
	m.dialog.CloseDialog(dialog.PermissionsID)

	// Get diff mode from config.
	var opts []dialog.PermissionsOption
	if diffMode := m.com.Config().Options.TUI.DiffMode; diffMode != "" {
		opts = append(opts, dialog.WithDiffMode(diffMode == "split"))
	}

	permDialog := dialog.NewPermissions(m.com, perm, opts...)
	m.dialog.OpenDialog(permDialog)
	return nil
}

// handlePermissionNotification updates tool items when permission state changes.
func (m *UI) handlePermissionNotification(notification permission.PermissionNotification) {
	toolItem := m.chat.MessageItem(notification.ToolCallID)
	if toolItem == nil {
		return
	}

	if permItem, ok := toolItem.(chat.ToolMessageItem); ok {
		if notification.Granted {
			permItem.SetStatus(chat.ToolStatusRunning)
		} else {
			permItem.SetStatus(chat.ToolStatusAwaitingPermission)
		}
		m.chat.InvalidateItemHeight(notification.ToolCallID)
	}
}

// handleAgentNotification translates domain agent events into desktop
// notifications using the UI notification backend.
func (m *UI) handleAgentNotification(n notify.Notification) tea.Cmd {
	switch n.Type {
	case notify.TypeAgentFinished:
		return m.sendNotification(notification.Notification{
			Title:   "Crush is waiting...",
			Message: fmt.Sprintf("Agent's turn completed in \"%s\"", n.SessionTitle),
		})
	case notify.TypeReAuthenticate:
		return m.handleReAuthenticate(n.ProviderID)
	default:
		return nil
	}
}

func (m *UI) handleReAuthenticate(providerID string) tea.Cmd {
	cfg := m.com.Config()
	if cfg == nil {
		return nil
	}
	providerCfg, ok := cfg.Providers.Get(providerID)
	if !ok {
		return nil
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return nil
	}
	return m.openAuthenticationDialog(providerCfg.ToProvider(), cfg.Models[agentCfg.Model], agentCfg.Model)
}

// newSession clears the current session state and prepares for a new session.
// The actual session creation happens when the user sends their first message.
// Returns a command to reload prompt history.
func (m *UI) newSession() tea.Cmd {
	if !m.hasSession() {
		return nil
	}

	m.session = nil
	m.syncTmuxSessionID()
	m.sessionFiles = nil
	m.sessionFileReads = nil
	m.setState(uiChat, uiFocusEditor)
	m.textarea.Focus()
	m.chat.Blur()
	m.chat.ClearMessages()
	m.pillsExpanded = false
	m.promptQueue = 0
	m.pillsView = ""
	m.pendingToolResults = make(map[string]*message.ToolResult)
	m.historyReset()
	agenttools.ResetCache()
	return tea.Batch(
		func() tea.Msg {
			m.com.Workspace.LSPStopAll(context.Background())
			return nil
		},
		m.loadPromptHistory(),
	)
}

// handlePasteMsg handles a paste message.
func (m *UI) handlePasteMsg(msg tea.PasteMsg) tea.Cmd {
	if m.dialog.HasDialogs() {
		return m.handleDialogMsg(msg)
	}

	if m.focus != uiFocusEditor {
		return nil
	}

	if hasPasteExceededThreshold(msg) {
		idx := m.pasteIdx()
		return func() tea.Msg {
			content := []byte(msg.Content)
			if int64(len(content)) > common.MaxAttachmentSize {
				return util.ReportWarn("Paste is too big (>5mb)")
			}

			if att, ok := tryParseBase64Image(strings.TrimSpace(msg.Content), idx); ok {
				return att
			}

			name := fmt.Sprintf("paste_%d.txt", idx)
			mimeBufferSize := min(512, len(content))
			mimeType := http.DetectContentType(content[:mimeBufferSize])
			return message.Attachment{
				FileName: name,
				FilePath: name,
				MimeType: mimeType,
				Content:  content,
			}
		}
	}

	if att, ok := tryParseBase64Image(strings.TrimSpace(msg.Content), m.pasteIdx()); ok {
		return func() tea.Msg { return att }
	}

	// Attempt to parse pasted content as file paths. If possible to parse,
	// all files exist and are valid, add as attachments.
	// Otherwise, paste as text.
	paths := fsext.ParsePastedFiles(msg.Content)
	allExistsAndValid := func() bool {
		if len(paths) == 0 {
			return false
		}
		for _, path := range paths {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				return false
			}

			lowerPath := strings.ToLower(path)
			isValid := false
			for _, ext := range common.AllowedImageTypes {
				if strings.HasSuffix(lowerPath, ext) {
					isValid = true
					break
				}
			}
			if !isValid {
				return false
			}
		}
		return true
	}
	if !allExistsAndValid() {
		prevHeight := m.textarea.Height()
		return m.updateTextareaWithPrevHeight(msg, prevHeight)
	}

	var cmds []tea.Cmd
	for _, path := range paths {
		cmds = append(cmds, m.handleFilePathPaste(path))
	}
	return tea.Batch(cmds...)
}

func hasPasteExceededThreshold(msg tea.PasteMsg) bool {
	var (
		lineCount = 0
		colCount  = 0
	)
	for line := range strings.SplitSeq(msg.Content, "\n") {
		lineCount++
		colCount = max(colCount, len(line))

		if lineCount > pasteLinesThreshold || colCount > pasteColsThreshold {
			return true
		}
	}
	return false
}

// handleFilePathPaste handles a pasted file path.
func (m *UI) handleFilePathPaste(path string) tea.Cmd {
	return func() tea.Msg {
		fileInfo, err := os.Stat(path)
		if err != nil {
			return util.ReportError(err)
		}
		if fileInfo.IsDir() {
			return util.ReportWarn("Cannot attach a directory")
		}
		if fileInfo.Size() > common.MaxAttachmentSize {
			return util.ReportWarn("File is too big (>5mb)")
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return util.ReportError(err)
		}

		mimeBufferSize := min(512, len(content))
		mimeType := http.DetectContentType(content[:mimeBufferSize])
		fileName := filepath.Base(path)
		return message.Attachment{
			FilePath: path,
			FileName: fileName,
			MimeType: mimeType,
			Content:  content,
		}
	}
}

// pasteImageFromClipboard reads image data from the system clipboard and
// creates an attachment. If no image data is found, it falls back to
// interpreting clipboard text as a file path, or pastes text into the textarea.
func (m *UI) pasteImageFromClipboard(idx int) tea.Msg {
	imageData, err := readClipboard(clipboardFormatImage)
	if err == nil && len(imageData) > 0 {
		if int64(len(imageData)) > common.MaxAttachmentSize {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  "File too large, max 5MB",
			}
		}
		name := fmt.Sprintf("paste_%d.png", idx)
		return message.Attachment{
			FilePath: name,
			FileName: name,
			MimeType: mimeOf(imageData),
			Content:  imageData,
		}
	}

	textData, textErr := readClipboard(clipboardFormatText)
	if textErr != nil || len(textData) == 0 {
		return nil
	}

	text := strings.TrimSpace(string(textData))

	if att, ok := tryParseBase64Image(text, idx); ok {
		return att
	}

	path := strings.ReplaceAll(text, "\\ ", " ")
	if _, statErr := os.Stat(path); statErr == nil {
		lowerPath := strings.ToLower(path)
		for _, ext := range common.AllowedImageTypes {
			if strings.HasSuffix(lowerPath, ext) {
				return m.loadImageAttachment(path)
			}
		}
	}

	return tea.PasteMsg{Content: string(textData)}
}

// loadImageAttachment reads an image file and returns it as an attachment.
func (m *UI) loadImageAttachment(path string) tea.Msg {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  fmt.Sprintf("Unable to read file: %v", err),
		}
	}
	if fileInfo.Size() > common.MaxAttachmentSize {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  "File too large, max 5MB",
		}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  fmt.Sprintf("Unable to read file: %v", err),
		}
	}
	return message.Attachment{
		FilePath: path,
		FileName: filepath.Base(path),
		MimeType: mimeOf(content),
		Content:  content,
	}
}

// dataURIRegexp matches data:image/<type>;base64,<data> URIs.
var dataURIRegexp = regexp.MustCompile(`^data:image/(png|jpeg|jpg|gif|webp);base64,([A-Za-z0-9+/=\s]+)$`)

// tryParseBase64Image attempts to parse a string as a base64-encoded image.
// Supports data URIs (data:image/png;base64,...) and raw base64 strings.
func tryParseBase64Image(text string, idx int) (message.Attachment, bool) {
	var imageData []byte
	var mimeType string

	if m := dataURIRegexp.FindStringSubmatch(text); m != nil {
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(m[2], " ", ""))
		if err != nil {
			return message.Attachment{}, false
		}
		imageData = decoded
		mimeType = "image/" + m[1]
	} else if len(text) > 100 && !strings.ContainsAny(text, " \t\n{}()<>") {
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return message.Attachment{}, false
		}
		detected := http.DetectContentType(decoded[:min(512, len(decoded))])
		if !strings.HasPrefix(detected, "image/") {
			return message.Attachment{}, false
		}
		imageData = decoded
		mimeType = detected
	}

	if imageData == nil {
		return message.Attachment{}, false
	}

	if int64(len(imageData)) > common.MaxAttachmentSize {
		return message.Attachment{}, false
	}

	ext := ".png"
	switch {
	case strings.Contains(mimeType, "jpeg") || strings.Contains(mimeType, "jpg"):
		ext = ".jpg"
	case strings.Contains(mimeType, "gif"):
		ext = ".gif"
	case strings.Contains(mimeType, "webp"):
		ext = ".webp"
	}
	name := fmt.Sprintf("paste_%d%s", idx, ext)

	return message.Attachment{
		FilePath: name,
		FileName: name,
		MimeType: mimeType,
		Content:  imageData,
	}, true
}

var pasteRE = regexp.MustCompile(`paste_(\d+)\.\w+`)

func (m *UI) pasteIdx() int {
	result := 0
	for _, at := range m.attachments.List() {
		found := pasteRE.FindStringSubmatch(at.FileName)
		if len(found) == 0 {
			continue
		}
		idx, err := strconv.Atoi(found[1])
		if err == nil {
			result = max(result, idx)
		}
	}
	return result + 1
}

// drawSessionDetails draws the session details in compact mode.
func (m *UI) drawSessionDetails(scr uv.Screen, area uv.Rectangle) {
	if m.session == nil {
		return
	}

	s := m.com.Styles

	width := area.Dx() - s.CompactDetails.View.GetHorizontalFrameSize()
	height := area.Dy() - s.CompactDetails.View.GetVerticalFrameSize()

	title := s.CompactDetails.Title.Width(width).MaxHeight(2).Render(m.session.Title)
	blocks := []string{
		title,
		"",
		m.modelInfo(width),
		"",
	}

	detailsHeader := lipgloss.JoinVertical(
		lipgloss.Left,
		blocks...,
	)

	version := s.CompactDetails.Version.Foreground(s.Border).Width(width).AlignHorizontal(lipgloss.Right).Render(version.Version)

	remainingHeight := height - lipgloss.Height(detailsHeader) - lipgloss.Height(version)

	const maxSectionWidth = 50
	sectionWidth := min(maxSectionWidth, width/3-2) // account for 2 spaces
	maxItemsPerSection := remainingHeight - 3       // Account for section title and spacing

	lspSection := m.lspInfo(sectionWidth, maxItemsPerSection, false)
	mcpSection := m.mcpInfo(sectionWidth, maxItemsPerSection, false)
	filesSection := m.filesInfo(m.com.Workspace.WorkingDir(), sectionWidth, maxItemsPerSection, false)
	sections := lipgloss.JoinHorizontal(lipgloss.Top, filesSection, " ", lspSection, " ", mcpSection)
	uv.NewStyledString(
		s.CompactDetails.View.
			Width(area.Dx()).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Left,
					detailsHeader,
					sections,
					version,
				),
			),
	).Draw(scr, area)
}

func (m *UI) runMCPPrompt(clientID, promptID string, arguments map[string]string) tea.Cmd {
	load := func() tea.Msg {
		prompt, err := m.com.Workspace.GetMCPPrompt(clientID, promptID, arguments)
		if err != nil {
			// TODO: make this better
			return util.ReportError(err)()
		}

		if prompt == "" {
			return nil
		}
		return sendMessageMsg{
			Content: prompt,
		}
	}

	var cmds []tea.Cmd
	if cmd := m.dialog.StartLoading(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, load, func() tea.Msg {
		return closeDialogMsg{}
	})

	return tea.Sequence(cmds...)
}

func (m *UI) handleStateChanged() tea.Cmd {
	return func() tea.Msg {
		if err := m.com.Workspace.UpdateAgentModel(context.Background()); err != nil {
			return util.ReportError(err)()
		}
		return mcpStateChangedMsg{
			states: m.com.Workspace.MCPGetStates(),
		}
	}
}

func handleMCPPromptsEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.MCPRefreshPrompts(context.Background(), name)
		return nil
	}
}

func handleMCPToolsEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.RefreshMCPTools(context.Background(), name)
		return nil
	}
}

func handleMCPResourcesEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.MCPRefreshResources(context.Background(), name)
		return nil
	}
}

func (m *UI) copyChatHighlight() tea.Cmd {
	text := m.chat.HighlightContent()
	return common.CopyToClipboardWithCallback(
		text,
		"Selected text copied to clipboard",
		func() tea.Msg {
			return copyChatHighlightDoneMsg{}
		},
	)
}

func (m *UI) enableDockerMCP() tea.Msg {
	ctx := context.Background()
	if err := m.com.Workspace.EnableDockerMCP(ctx); err != nil {
		return util.ReportError(err)()
	}

	return util.NewInfoMsg("Docker MCP enabled and started successfully")
}

func (m *UI) disableDockerMCP() tea.Msg {
	if err := m.com.Workspace.DisableDockerMCP(); err != nil {
		return util.ReportError(err)()
	}

	return util.NewInfoMsg("Docker MCP disabled successfully")
}

func (m *UI) refreshCopilotModels() tea.Cmd {
	return func() tea.Msg {
		store := m.com.App.Store()
		ctx := context.Background()
		if err := store.RefreshOAuthToken(ctx, config.ScopeGlobal, "copilot"); err != nil {
			return util.ReportError(err)()
		}
		if err := m.com.App.UpdateAgentModel(ctx); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg("Copilot token and models refreshed")
	}
}

func (m *UI) selfUpdate() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		info, err := update.Check(ctx, version.Version, version.BuildTime, update.Default)
		if err != nil {
			return util.ReportError(err)()
		}
		if !info.Available() {
			return util.NewInfoMsg(fmt.Sprintf("Already up to date (%s)", info.Current))
		}
		newVersion, err := update.SelfUpdate(ctx)
		if err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg(fmt.Sprintf("Updated to %s — restart crush to use the new version", newVersion))
	}
}

func (m *UI) toggleMCP(name string, disable bool) tea.Cmd {
	return func() tea.Msg {
		store := m.com.App.Store()
		if disable {
			if err := mcp.DisableSingle(store, name); err != nil {
				return util.ReportError(fmt.Errorf("failed to disable MCP %s: %w", name, err))()
			}
			if err := m.persistMCPDisabled(store, name, true); err != nil {
				return util.ReportError(err)()
			}
			return mcpToggledMsg{Name: name, Disabled: true, Info: fmt.Sprintf("MCP %s disabled", name)}
		}

		// Update in-memory config before InitializeSingle, which checks
		// the Disabled field to decide whether to proceed.
		if cfg, ok := store.Config().MCP[name]; ok {
			cfg.Disabled = false
			store.Config().MCP[name] = cfg
		}
		if err := m.persistMCPDisabled(store, name, false); err != nil {
			return util.ReportError(err)()
		}
		ctx := context.Background()
		if err := mcp.InitializeSingle(ctx, name, store); err != nil {
			if cfg, ok := store.Config().MCP[name]; ok {
				cfg.Disabled = true
				store.Config().MCP[name] = cfg
			}
			_ = m.persistMCPDisabled(store, name, true)
			return TextPreviewMsg{
				Title: fmt.Sprintf("Failed to enable MCP: %s", name),
				Text:  err.Error(),
			}
		}
		return mcpToggledMsg{Name: name, Disabled: false, Info: fmt.Sprintf("MCP %s enabled", name)}
	}
}

func (m *UI) persistMCPDisabled(store *config.ConfigStore, name string, disabled bool) error {
	scope := config.ScopeGlobal
	if store.HasConfigField(config.ScopeWorkspace, "mcp."+name) {
		scope = config.ScopeWorkspace
	}
	key := "mcp." + name + ".disabled"
	if disabled {
		return store.SetConfigField(scope, key, true)
	}
	return store.RemoveConfigField(scope, key)
}

// renderLogo renders the Crush logo with the given styles and dimensions.
func renderLogo(t *styles.Styles, compact bool, width int) string {
	return logo.Render(t, version.Version, compact, logo.Opts{
		FieldColor:   t.LogoFieldColor,
		TitleColorA:  t.LogoTitleColorA,
		TitleColorB:  t.LogoTitleColorB,
		CharmColor:   t.LogoCharmColor,
		VersionColor: t.LogoVersionColor,
		Width:        width,
	})
}
