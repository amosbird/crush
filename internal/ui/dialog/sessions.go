package dialog

import (
	"context"
	"image"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/search"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
)

// SessionsID is the identifier for the session selector dialog.
const SessionsID = "session"

type sessionsMode uint8

// Possible modes a session item can be in
const (
	sessionsModeNormal sessionsMode = iota
	sessionsModeDeleting
	sessionsModeUpdating
)

// sessionPreviewLoadedMsg carries preview lines for the sessions dialog.
type sessionPreviewLoadedMsg struct {
	sessionID string
	lines     []string
}

// Session is a session selector dialog.
type Session struct {
	com                *common.Common
	help               help.Model
	list               *list.FilterableList
	input              textinput.Model
	selectedSessionInx int
	sessions           []session.Session
	activeIDs          []string

	sessionsMode sessionsMode
	alwaysDelete bool

	// preview state
	preview     []string
	previewSID  string
	previewRow  int
	previewRect image.Rectangle
	dbPath      string

	keyMap struct {
		Select        key.Binding
		Next          key.Binding
		Previous      key.Binding
		UpDown        key.Binding
		Delete        key.Binding
		Rename        key.Binding
		Fork          key.Binding
		ConfirmRename key.Binding
		CancelRename  key.Binding
		ConfirmDelete key.Binding
		AlwaysDelete  key.Binding
		CancelDelete  key.Binding
		Close         key.Binding
	}
}

var _ Dialog = (*Session)(nil)

// NewSessions creates a new Session dialog.
func NewSessions(com *common.Common, selectedSessionID string) (*Session, error) {
	s := new(Session)
	s.sessionsMode = sessionsModeNormal
	s.com = com
	sessions, err := com.Workspace.ListSessions(context.TODO())
	if err != nil {
		return nil, err
	}

	s.sessions = sessions
	s.activeIDs = com.Mux.ActiveCrushSessions()
	for i, sess := range sessions {
		if sess.ID == selectedSessionID {
			s.selectedSessionInx = i
			break
		}
	}

	s.dbPath = filepath.Join(com.Workspace.Config().Options.DataDirectory, "crush.db")

	help := help.New()
	help.Styles = com.Styles.DialogHelpStyles()

	s.help = help
	s.list = list.NewFilterableList(sessionItems(com.Styles, sessionsModeNormal, s.activeIDs, sessions...)...)
	s.list.Focus()
	s.list.SetSelected(s.selectedSessionInx)

	s.input = textinput.New()
	s.input.SetVirtualCursor(false)
	s.input.Placeholder = "Filter sessions…"
	s.input.SetStyles(com.Styles.TextInput)
	s.input.Focus()

	s.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "tab", "ctrl+y"),
		key.WithHelp("enter", "choose"),
	)
	s.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next item"),
	)
	s.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	s.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑↓", "choose"),
	)
	s.keyMap.Delete = key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "delete"),
	)
	s.keyMap.Rename = key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r", "rename"),
	)
	s.keyMap.ConfirmRename = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	)
	s.keyMap.CancelRename = key.NewBinding(
		key.WithKeys("ctrl+g"),
		key.WithHelp("ctrl+g", "cancel"),
	)
	s.keyMap.ConfirmDelete = key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "delete"),
	)
	s.keyMap.AlwaysDelete = key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "always"),
	)
	s.keyMap.CancelDelete = key.NewBinding(
		key.WithKeys("n", "ctrl+g", "esc"),
		key.WithHelp("n", "cancel"),
	)
	s.keyMap.Fork = key.NewBinding(
		key.WithKeys("alt+shift+f", "alt+F"),
		key.WithHelp("alt+F", "fork"),
	)
	s.keyMap.Close = CloseKey

	return s, nil
}

// InitialPreviewCmd returns a command that loads the preview for the initially
// selected session.
func (s *Session) InitialPreviewCmd() tea.Cmd {
	return s.loadPreviewCmd()
}

// ID implements Dialog.
func (s *Session) ID() string {
	return SessionsID
}

// HandleMsg implements Dialog.
func (s *Session) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case sessionPreviewLoadedMsg:
		if msg.sessionID == s.previewSID {
			s.preview = msg.lines
			s.previewRow = 0
		}
		return nil

	case tea.MouseWheelMsg:
		pt := image.Pt(msg.X, msg.Y)
		if pt.In(s.previewRect) {
			switch msg.Button {
			case tea.MouseWheelUp:
				s.previewRow = max(0, s.previewRow-3)
			case tea.MouseWheelDown:
				s.previewRow += 3
			}
		}
		return nil

	case tea.KeyPressMsg:
		switch s.sessionsMode {
		case sessionsModeDeleting:
			switch {
			case key.Matches(msg, s.keyMap.ConfirmDelete), key.Matches(msg, s.keyMap.AlwaysDelete):
				if key.Matches(msg, s.keyMap.AlwaysDelete) {
					s.alwaysDelete = true
				}
				return s.confirmDeleteSession()
			case key.Matches(msg, s.keyMap.CancelDelete):
				s.sessionsMode = sessionsModeNormal
				s.list.SetItems(sessionItems(s.com.Styles, sessionsModeNormal, s.activeIDs, s.sessions...)...)
			}
		case sessionsModeUpdating:
			switch {
			case key.Matches(msg, s.keyMap.ConfirmRename):
				action := s.confirmRenameSession()
				s.list.SetItems(sessionItems(s.com.Styles, sessionsModeNormal, s.activeIDs, s.sessions...)...)
				return action
			case key.Matches(msg, s.keyMap.CancelRename):
				s.sessionsMode = sessionsModeNormal
				s.list.SetItems(sessionItems(s.com.Styles, sessionsModeNormal, s.activeIDs, s.sessions...)...)
			default:
				item := s.list.SelectedItem()
				if item == nil {
					return nil
				}
				if sessionItem, ok := item.(*SessionItem); ok {
					return sessionItem.HandleInput(msg)
				}
			}
		default:
			switch {
			case key.Matches(msg, s.keyMap.Close):
				return ActionClose{}
			case key.Matches(msg, s.keyMap.Rename):
				s.sessionsMode = sessionsModeUpdating
				s.list.SetItems(sessionItems(s.com.Styles, sessionsModeUpdating, s.activeIDs, s.sessions...)...)
			case key.Matches(msg, s.keyMap.Delete):
				if s.isSelectedSessionActive() {
					return ActionCmd{util.ReportWarn("Cannot delete an active session")}
				}
				if s.isCurrentSessionBusy() {
					return ActionCmd{util.ReportWarn("Agent is busy, please wait...")}
				}
				if s.alwaysDelete {
					return s.confirmDeleteSession()
				}
				s.sessionsMode = sessionsModeDeleting
				s.list.SetItems(sessionItems(s.com.Styles, sessionsModeDeleting, s.activeIDs, s.sessions...)...)
			case key.Matches(msg, s.keyMap.Previous):
				s.list.Focus()
				if s.list.IsSelectedFirst() {
					s.list.SelectLast()
				} else {
					s.list.SelectPrev()
				}
				s.list.ScrollToSelected()
				return ActionCmd{s.loadPreviewCmd()}
			case key.Matches(msg, s.keyMap.Next):
				s.list.Focus()
				if s.list.IsSelectedLast() {
					s.list.SelectFirst()
				} else {
					s.list.SelectNext()
				}
				s.list.ScrollToSelected()
				return ActionCmd{s.loadPreviewCmd()}
			case key.Matches(msg, s.keyMap.Select):
				if item := s.list.SelectedItem(); item != nil {
					sessionItem := item.(*SessionItem)
					return ActionSelectSession{sessionItem.Session}
				}
			case key.Matches(msg, s.keyMap.Fork):
				if item := s.list.SelectedItem(); item != nil {
					sessionItem := item.(*SessionItem)
					return ActionForkSession{SessionID: sessionItem.ID()}
				}
			default:
				var cmd tea.Cmd
				s.input, cmd = s.input.Update(msg)
				value := s.input.Value()
				s.list.SetFilter(value)
				s.list.ScrollToTop()
				s.list.SetSelected(0)
				return ActionCmd{tea.Batch(cmd, s.loadPreviewCmd())}
			}
		}
	}
	return nil
}

// loadPreviewCmd loads the preview for the currently selected session.
func (s *Session) loadPreviewCmd() tea.Cmd {
	item := s.list.SelectedItem()
	if item == nil {
		s.preview = nil
		s.previewSID = ""
		return nil
	}
	sessionItem := item.(*SessionItem)
	sid := sessionItem.Session.ID
	if sid == s.previewSID {
		return nil
	}
	s.previewSID = sid
	dbPath := s.dbPath
	return func() tea.Msg {
		lines, _ := search.Preview(dbPath, sid)
		return sessionPreviewLoadedMsg{sessionID: sid, lines: lines}
	}
}

// Cursor returns the cursor position relative to the dialog.
func (s *Session) Cursor() *tea.Cursor {
	return InputCursor(s.com.Styles, s.input.Cursor())
}

// Draw implements [Dialog].
func (s *Session) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := s.com.Styles

	// Align left edge to chat content area.
	chatArea := s.com.ChatArea
	startX := chatArea.Min.X
	if startX <= area.Min.X {
		startX = area.Min.X + area.Dx()/4
	}
	availW := area.Max.X - startX - 1

	// Left panel: standard dialog layout.
	leftWidth := max(0, min(defaultDialogMaxWidth, availW-t.Dialog.View.GetHorizontalBorderSize()))
	dialogWidth := leftWidth
	innerWidth := dialogWidth - t.Dialog.View.GetHorizontalFrameSize()

	totalHeight := max(0, min(defaultDialogHeight, area.Dy()-4))
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	s.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	s.list.SetSize(innerWidth, totalHeight-heightOffset)
	s.help.SetWidth(innerWidth)

	// This makes it so we do not scroll the list if we don't have to
	start, end := s.list.VisibleItemIndices()

	// if selected index is outside visible range, scroll to it
	if s.selectedSessionInx < start || s.selectedSessionInx > end {
		s.list.ScrollToSelected()
	}

	var cur *tea.Cursor
	rc := NewRenderContext(t, dialogWidth)
	rc.Title = "Sessions"
	switch s.sessionsMode {
	case sessionsModeDeleting:
		rc.TitleStyle = t.Dialog.Sessions.DeletingTitle
		rc.TitleGradientFromColor = t.Dialog.Sessions.DeletingTitleGradientFromColor
		rc.TitleGradientToColor = t.Dialog.Sessions.DeletingTitleGradientToColor
		rc.ViewStyle = t.Dialog.Sessions.DeletingView
		rc.AddPart(t.Dialog.Sessions.DeletingMessage.Render("Delete this session?"))
	case sessionsModeUpdating:
		rc.TitleStyle = t.Dialog.Sessions.RenamingingTitle
		rc.TitleGradientFromColor = t.Dialog.Sessions.RenamingTitleGradientFromColor
		rc.TitleGradientToColor = t.Dialog.Sessions.RenamingTitleGradientToColor
		rc.ViewStyle = t.Dialog.Sessions.RenamingView
		message := t.Dialog.Sessions.RenamingingMessage.Render("Rename this session?")
		rc.AddPart(message)
		item := s.selectedSessionItem()
		if item == nil {
			return nil
		}
		cur = item.Cursor()
		if cur == nil {
			break
		}

		start, end := s.list.VisibleItemIndices()
		selectedIndex := s.list.Selected()

		titleStyle := t.Dialog.Sessions.RenamingingTitle
		dialogStyle := t.Dialog.Sessions.RenamingView
		inputStyle := t.Dialog.InputPrompt

		cur.X += inputStyle.GetBorderLeftSize() +
			inputStyle.GetMarginLeft() +
			inputStyle.GetPaddingLeft() +
			dialogStyle.GetBorderLeftSize() +
			dialogStyle.GetPaddingLeft() +
			dialogStyle.GetMarginLeft()
		cur.Y += titleStyle.GetVerticalFrameSize() +
			inputStyle.GetBorderTopSize() +
			inputStyle.GetMarginTop() +
			inputStyle.GetPaddingTop() +
			inputStyle.GetBorderBottomSize() +
			inputStyle.GetMarginBottom() +
			inputStyle.GetPaddingBottom() +
			dialogStyle.GetPaddingTop() +
			dialogStyle.GetBorderTopSize() +
			lipgloss.Height(message) - 1

		for ; start <= end && start != selectedIndex && selectedIndex > -1; start++ {
			cur.Y += 1
		}
	default:
		inputView := t.Dialog.InputPrompt.Render(s.input.View())
		cur = s.Cursor()
		rc.AddPart(inputView)
	}
	listView := t.Dialog.List.Height(s.list.Height()).Render(s.list.Render())
	rc.AddPart(listView)
	rc.Help = s.help.View(s)

	leftView := rc.Render()
	_, leftH := lipgloss.Size(leftView)

	// Preview: taller than left panel, fill vertical space with top margin.
	const previewTopMargin = 5
	previewH := max(leftH, area.Dy()-4-previewTopMargin)
	rightWidth := max(0, availW-leftWidth)
	previewView := s.buildPreview(rightWidth, previewH)

	previewStartY := area.Min.Y + previewTopMargin + max(0, (area.Dy()-previewTopMargin-previewH)/2)
	leftStartY := previewStartY + max(0, (previewH-leftH)/2)

	// Draw left.
	leftRect := image.Rect(startX, leftStartY, startX+leftWidth, leftStartY+leftH)
	uv.NewStyledString(leftView).Draw(scr, leftRect)

	// Draw right.
	s.previewRect = image.Rect(startX+leftWidth, previewStartY, startX+leftWidth+rightWidth, previewStartY+previewH)
	uv.NewStyledString(previewView).Draw(scr, s.previewRect)

	if cur != nil {
		cur.X += startX
		cur.Y += leftStartY
	}
	return cur
}

// buildPreview builds the preview panel with a rounded border.
func (s *Session) buildPreview(width, height int) string {
	borderW := max(0, width-2)
	borderH := max(0, height-2)

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.com.Styles.Subtle.GetForeground()).
		Width(borderW).
		Height(borderH)

	if len(s.preview) == 0 {
		return border.Render("")
	}

	tokens := search.TokenizeQuery(s.input.Value())
	innerW := max(1, borderW-2)

	maxRow := max(0, len(s.preview)-borderH)
	s.previewRow = max(0, min(s.previewRow, maxRow))

	endLine := min(len(s.preview), s.previewRow+borderH)
	visible := s.preview[s.previewRow:endLine]

	var lines []string
	for _, line := range visible {
		cut := centerTruncate(line, innerW, tokens)
		lines = append(lines, highlightLine(cut, tokens))
	}

	return border.Render(strings.Join(lines, "\n"))
}

func (s *Session) selectedSessionItem() *SessionItem {
	if item := s.list.SelectedItem(); item != nil {
		return item.(*SessionItem)
	}
	return nil
}

func (s *Session) confirmDeleteSession() Action {
	sessionItem := s.selectedSessionItem()
	idx := s.list.Selected()
	s.sessionsMode = sessionsModeNormal
	if sessionItem == nil {
		return nil
	}

	s.removeSession(sessionItem.ID())
	s.list.SetItems(sessionItems(s.com.Styles, sessionsModeNormal, s.activeIDs, s.sessions...)...)
	if s.list.Len() > 0 {
		s.list.SetSelected(min(idx, s.list.Len()-1))
	}
	s.previewSID = ""
	return ActionCmd{tea.Batch(s.deleteSessionCmd(sessionItem.ID()), s.loadPreviewCmd())}
}

func (s *Session) removeSession(id string) {
	var newSessions []session.Session
	for _, sess := range s.sessions {
		if sess.ID == id {
			continue
		}
		newSessions = append(newSessions, sess)
	}
	s.sessions = newSessions
}

func (s *Session) deleteSessionCmd(id string) tea.Cmd {
	return func() tea.Msg {
		err := s.com.Workspace.DeleteSession(context.TODO(), id)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		return nil
	}
}

func (s *Session) confirmRenameSession() Action {
	sessionItem := s.selectedSessionItem()
	s.sessionsMode = sessionsModeNormal
	if sessionItem == nil {
		return nil
	}

	newTitle := strings.TrimSpace(sessionItem.InputValue())
	if newTitle == "" {
		return nil
	}
	session := sessionItem.Session
	session.Title = newTitle
	s.updateSession(session)
	return ActionCmd{s.updateSessionCmd(session)}
}

func (s *Session) updateSession(session session.Session) {
	for existingID, sess := range s.sessions {
		if sess.ID == session.ID {
			s.sessions[existingID] = session
			break
		}
	}
}

func (s *Session) updateSessionCmd(session session.Session) tea.Cmd {
	return func() tea.Msg {
		_, err := s.com.Workspace.SaveSession(context.TODO(), session)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		return nil
	}
}

func (s *Session) isSelectedSessionActive() bool {
	sessionItem := s.selectedSessionItem()
	if sessionItem == nil {
		return false
	}
	return sessionItem.active
}

func (s *Session) isCurrentSessionBusy() bool {
	sessionItem := s.selectedSessionItem()
	if sessionItem == nil {
		return false
	}

	if !s.com.Workspace.AgentIsReady() {
		return false
	}

	return s.com.Workspace.AgentIsSessionBusy(sessionItem.ID())
}

// ShortHelp implements [help.KeyMap].
func (s *Session) ShortHelp() []key.Binding {
	switch s.sessionsMode {
	case sessionsModeDeleting:
		return []key.Binding{
			s.keyMap.ConfirmDelete,
			s.keyMap.AlwaysDelete,
			s.keyMap.CancelDelete,
		}
	case sessionsModeUpdating:
		return []key.Binding{
			s.keyMap.ConfirmRename,
			s.keyMap.CancelRename,
		}
	default:
		return []key.Binding{
			s.keyMap.UpDown,
			s.keyMap.Rename,
			s.keyMap.Delete,
			s.keyMap.Fork,
			s.keyMap.Select,
			s.keyMap.Close,
		}
	}
}

// FullHelp implements [help.KeyMap].
func (s *Session) FullHelp() [][]key.Binding {
	m := [][]key.Binding{}
	slice := []key.Binding{
		s.keyMap.UpDown,
		s.keyMap.Rename,
		s.keyMap.Delete,
		s.keyMap.Fork,
		s.keyMap.Select,
		s.keyMap.Close,
	}

	switch s.sessionsMode {
	case sessionsModeDeleting:
		slice = []key.Binding{
			s.keyMap.ConfirmDelete,
			s.keyMap.AlwaysDelete,
			s.keyMap.CancelDelete,
		}
	case sessionsModeUpdating:
		slice = []key.Binding{
			s.keyMap.ConfirmRename,
			s.keyMap.CancelRename,
		}
	}
	for i := 0; i < len(slice); i += 4 {
		end := min(i+4, len(slice))
		m = append(m, slice[i:end])
	}
	return m
}
