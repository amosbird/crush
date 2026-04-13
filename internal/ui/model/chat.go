package model

import (
	"image"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/words"
)

// Constants for multi-click detection.
const (
	doubleClickThreshold = 400 * time.Millisecond // 0.4s is typical double-click threshold
	clickTolerance       = 2                      // x,y tolerance for double/tripple click
)

// DelayedClickMsg is sent after the double-click threshold to trigger a
// single-click action (like expansion) if no double-click occurred.
type DelayedClickMsg struct {
	ClickID int
	ItemIdx int
	X, Y    int
}

// ImagePreviewMsg is sent when a user clicks on an image attachment to
// request an image preview dialog.
type ImagePreviewMsg struct {
	Attachment message.Attachment
}

// TextPreviewMsg is sent when a user clicks on a text attachment to request
// a text preview dialog.
type TextPreviewMsg struct {
	Title string
	Text  string
}

// DiffPreviewMsg is sent when a user clicks on a diff tool output to request
// a diff preview dialog.
type DiffPreviewMsg struct {
	FilePath   string
	OldContent string
	NewContent string
}

// JobPreviewMsg is sent when a user clicks on a background job or agent
// tool to request a live output preview dialog.
type JobPreviewMsg struct {
	Title       string
	ContentFunc func() chat.JobPreviewResult
	KillFunc    func() string
}

// Chat represents the chat UI model that handles chat interactions and
// messages.
type Chat struct {
	com      *common.Common
	list     *list.List
	idInxMap map[string]int // Map of message IDs to their indices in the list

	// Animation visibility optimization: track animations paused due to items
	// being scrolled out of view. When items become visible again, their
	// animations are restarted.
	pausedAnimations map[string]struct{}

	// Mouse state
	mouseDown     bool
	mouseDownItem int // Item index where mouse was pressed
	mouseDownX    int // X position in item content (character offset)
	mouseDownY    int // Y position in item (line offset)
	mouseDragItem int // Current item index being dragged over
	mouseDragX    int // Current X in item content
	mouseDragY    int // Current Y in item

	// Click tracking for double/triple clicks
	lastClickTime time.Time
	lastClickX    int
	lastClickY    int
	clickCount    int

	// Pending single click action (delayed to detect double-click)
	pendingClickID int // Incremented on each click to invalidate old pending clicks

	// follow is a flag to indicate whether the view should auto-scroll to
	// bottom on new messages.
	follow bool
}

// NewChat creates a new instance of [Chat] that handles chat interactions and
// messages.
func NewChat(com *common.Common) *Chat {
	c := &Chat{
		com:              com,
		idInxMap:         make(map[string]int),
		pausedAnimations: make(map[string]struct{}),
	}
	l := list.NewList()
	l.SetGap(1)
	l.RegisterRenderCallback(c.applyHighlightRange)
	l.RegisterRenderCallback(list.FocusedRenderCallback(l))
	c.list = l
	c.mouseDownItem = -1
	c.mouseDragItem = -1
	return c
}

// Height returns the height of the chat view port.
func (m *Chat) Height() int {
	return m.list.Height()
}

// scrollbarWidth is the width of the scrollbar column.
const scrollbarWidth = 1

// Draw renders the chat UI component to the screen and the given area.
func (m *Chat) Draw(scr uv.Screen, area uv.Rectangle) {
	// Draw the list content in the left portion, leaving room for the scrollbar.
	contentArea := image.Rect(area.Min.X, area.Min.Y, area.Max.X-scrollbarWidth, area.Max.Y)
	uv.NewStyledString(m.list.Render()).Draw(scr, contentArea)

	// Draw scrollbar on the right edge using chat-specific styles.
	totalHeight, offset := m.list.ScrollInfo()
	viewportHeight := area.Dy()
	s := m.com.Styles
	scrollbar := common.ScrollbarStyled(
		s.Chat.ScrollbarThumb, s.Chat.ScrollbarTrack,
		viewportHeight, totalHeight, viewportHeight, offset,
	)
	sbArea := image.Rect(area.Max.X-scrollbarWidth, area.Min.Y, area.Max.X, area.Max.Y)
	if scrollbar != "" {
		uv.NewStyledString(scrollbar).Draw(scr, sbArea)
	} else {
		// Always draw the track line so the right edge stays clean.
		m.drawScrollbarTrack(scr, sbArea)
	}

	// Show a follow-mode indicator at the bottom-right when following.
	if m.follow && m.list.Len() > 0 {
		indicator := s.Chat.ScrollbarThumb.Render("▼")
		indicatorArea := image.Rect(area.Max.X-scrollbarWidth, area.Max.Y-1, area.Max.X, area.Max.Y)
		uv.NewStyledString(indicator).Draw(scr, indicatorArea)
	}
}

// drawScrollbarTrack draws a faint track line when there is nothing to scroll.
func (m *Chat) drawScrollbarTrack(scr uv.Screen, area uv.Rectangle) {
	fg := m.com.Styles.Border
	bg := m.com.Styles.Background
	x := area.Min.X
	for y := area.Min.Y; y < area.Max.Y; y++ {
		if c := scr.CellAt(x, y); c != nil {
			c.Content = styles.ScrollbarTrack
			c.Width = 1
			c.Style.Fg = fg
			c.Style.Bg = bg
		}
	}
}

// SetSize sets the size of the chat view port.
func (m *Chat) SetSize(width, height int) {
	// Reserve space for the scrollbar column.
	m.list.SetSize(width-scrollbarWidth, height)
	// Anchor to bottom if we were at the bottom.
	if m.AtBottom() {
		m.ScrollToBottom()
	}
}

// Len returns the number of items in the chat list.
func (m *Chat) Len() int {
	return m.list.Len()
}

// SetMessages sets the chat messages to the provided list of message items.
func (m *Chat) SetMessages(msgs ...chat.MessageItem) {
	m.idInxMap = make(map[string]int)
	m.pausedAnimations = make(map[string]struct{})

	items := make([]list.Item, len(msgs))
	for i, msg := range msgs {
		m.idInxMap[msg.ID()] = i
		// Register nested tool IDs for tools that contain nested tools.
		if container, ok := msg.(chat.NestedToolContainer); ok {
			for _, nested := range container.NestedTools() {
				m.idInxMap[nested.ID()] = i
			}
		}
		items[i] = msg
	}
	m.list.SetItems(items...)
	m.ScrollToBottom()
}

// AppendMessages appends a new message item to the chat list.
func (m *Chat) AppendMessages(msgs ...chat.MessageItem) {
	items := make([]list.Item, len(msgs))
	indexOffset := m.list.Len()
	for i, msg := range msgs {
		m.idInxMap[msg.ID()] = indexOffset + i
		// Register nested tool IDs for tools that contain nested tools.
		if container, ok := msg.(chat.NestedToolContainer); ok {
			for _, nested := range container.NestedTools() {
				m.idInxMap[nested.ID()] = indexOffset + i
			}
		}
		items[i] = msg
	}
	m.list.AppendItems(items...)
}

// PrependMessages prepends message items to the beginning of the chat list.
// The viewport offset is adjusted so the currently visible content stays in
// place (no visual jump).
func (m *Chat) PrependMessages(msgs ...chat.MessageItem) {
	if len(msgs) == 0 {
		return
	}

	// Shift existing indices in the ID map.
	shift := len(msgs)
	for id, idx := range m.idInxMap {
		m.idInxMap[id] = idx + shift
	}

	// Register new items.
	items := make([]list.Item, len(msgs))
	for i, msg := range msgs {
		m.idInxMap[msg.ID()] = i
		if container, ok := msg.(chat.NestedToolContainer); ok {
			for _, nested := range container.NestedTools() {
				m.idInxMap[nested.ID()] = i
			}
		}
		items[i] = msg
	}
	m.list.PrependItems(items...)
}

// AtTop returns whether the chat list is currently scrolled to the very top.
func (m *Chat) AtTop() bool {
	return m.list.AtTop()
}

// UpdateNestedToolIDs updates the ID map for nested tools within a container.
// Call this after modifying nested tools to ensure animations work correctly.
func (m *Chat) UpdateNestedToolIDs(containerID string) {
	idx, ok := m.idInxMap[containerID]
	if !ok {
		return
	}

	item, ok := m.list.ItemAt(idx).(chat.MessageItem)
	if !ok {
		return
	}

	container, ok := item.(chat.NestedToolContainer)
	if !ok {
		return
	}

	// Register all nested tool IDs to point to the container's index.
	for _, nested := range container.NestedTools() {
		m.idInxMap[nested.ID()] = idx
	}
}

// Animate animates items in the chat list. Only propagates animation messages
// to visible items to save CPU. When items are not visible, their animation ID
// is tracked so it can be restarted when they become visible again.
func (m *Chat) Animate(msg anim.StepMsg) tea.Cmd {
	idx, ok := m.idInxMap[msg.ID]
	if !ok {
		return nil
	}

	animatable, ok := m.list.ItemAt(idx).(chat.Animatable)
	if !ok {
		return nil
	}

	// Check if item is currently visible.
	startIdx, endIdx := m.list.VisibleItemIndices()
	isVisible := idx >= startIdx && idx <= endIdx

	if !isVisible {
		// Item not visible - pause animation by not propagating.
		// Track it so we can restart when it becomes visible.
		m.pausedAnimations[msg.ID] = struct{}{}
		return nil
	}

	// Item is visible - remove from paused set and animate.
	delete(m.pausedAnimations, msg.ID)
	return animatable.Animate(msg)
}

// RestartPausedVisibleAnimations restarts animations for items that were paused
// due to being scrolled out of view but are now visible again.
func (m *Chat) RestartPausedVisibleAnimations() tea.Cmd {
	if len(m.pausedAnimations) == 0 {
		return nil
	}

	startIdx, endIdx := m.list.VisibleItemIndices()
	var cmds []tea.Cmd
	restarted := make(map[int]struct{})

	for id := range m.pausedAnimations {
		idx, ok := m.idInxMap[id]
		if !ok {
			delete(m.pausedAnimations, id)
			continue
		}

		if idx >= startIdx && idx <= endIdx {
			if _, done := restarted[idx]; !done {
				if animatable, ok := m.list.ItemAt(idx).(chat.Animatable); ok {
					if cmd := animatable.StartAnimation(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
				restarted[idx] = struct{}{}
			}
			delete(m.pausedAnimations, id)
		}
	}

	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// Focus sets the focus state of the chat component.
func (m *Chat) Focus() {
	m.list.Focus()
}

// Blur removes the focus state from the chat component.
func (m *Chat) Blur() {
	m.list.Blur()
}

// nearBottomThreshold is the number of lines from the bottom within which
// follow mode re-engages when scrolling down.
const nearBottomThreshold = 5

// AtBottom returns whether the chat list is currently scrolled to the bottom.
func (m *Chat) AtBottom() bool {
	return m.list.AtBottom()
}

// NearBottom returns whether the chat list is within a few lines of the
// bottom. This is more forgiving than AtBottom and is used to re-engage
// follow mode when the user scrolls close to the bottom during streaming.
func (m *Chat) NearBottom() bool {
	return m.list.NearBottom(nearBottomThreshold)
}

// Follow returns whether the chat view is in follow mode (auto-scroll to
// bottom on new messages).
func (m *Chat) Follow() bool {
	return m.follow
}

// ScrollToBottom scrolls the chat view to the bottom.
func (m *Chat) ScrollToBottom() {
	m.list.ScrollToBottom()
	m.follow = true // Enable follow mode when user scrolls to bottom
}

// ScrollToTop scrolls the chat view to the top.
func (m *Chat) ScrollToTop() {
	m.list.ScrollToTop()
	m.follow = false // Disable follow mode when user scrolls up
}

// ScrollBy scrolls the chat view by the given number of line deltas.
func (m *Chat) ScrollBy(lines int) {
	m.list.ScrollBy(lines)
	// Re-engage follow mode when scrolling down and near the bottom.
	// Using NearBottom instead of AtBottom so that follow re-engages
	// even when streaming content grows faster than the user scrolls.
	m.follow = lines > 0 && m.NearBottom()
}

// ScrollToSelected scrolls the chat view to the selected item.
func (m *Chat) ScrollToSelected() {
	m.list.ScrollToSelected()
	m.follow = m.AtBottom() // Disable follow mode if user scrolls up
}

// ScrollToIndex scrolls the chat view to the item at the given index.
func (m *Chat) ScrollToIndex(index int) {
	m.list.ScrollToIndex(index)
	m.follow = m.AtBottom() // Disable follow mode if user scrolls up
}

// ScrollToTopAndAnimate scrolls the chat view to the top and returns a command to restart
// any paused animations that are now visible.
func (m *Chat) ScrollToTopAndAnimate() tea.Cmd {
	m.ScrollToTop()
	return m.RestartPausedVisibleAnimations()
}

// ScrollToBottomAndAnimate scrolls the chat view to the bottom and returns a command to
// restart any paused animations that are now visible.
func (m *Chat) ScrollToBottomAndAnimate() tea.Cmd {
	m.ScrollToBottom()
	return m.RestartPausedVisibleAnimations()
}

// ScrollByAndAnimate scrolls the chat view by the given number of line deltas and returns
// a command to restart any paused animations that are now visible.
func (m *Chat) ScrollByAndAnimate(lines int) tea.Cmd {
	m.ScrollBy(lines)
	return m.RestartPausedVisibleAnimations()
}

// ScrollToSelectedAndAnimate scrolls the chat view to the selected item and returns a
// command to restart any paused animations that are now visible.
func (m *Chat) ScrollToSelectedAndAnimate() tea.Cmd {
	m.ScrollToSelected()
	return m.RestartPausedVisibleAnimations()
}

// SelectedItemInView returns whether the selected item is currently in view.
func (m *Chat) SelectedItemInView() bool {
	return m.list.SelectedItemInView()
}

func (m *Chat) isSelectable(index int) bool {
	item := m.list.ItemAt(index)
	if item == nil {
		return false
	}
	_, ok := item.(list.Focusable)
	return ok
}

// SetSelected sets the selected message index in the chat list.
func (m *Chat) SetSelected(index int) {
	m.list.SetSelected(index)
	if index < 0 || index >= m.list.Len() {
		return
	}
	for {
		if m.isSelectable(m.list.Selected()) {
			return
		}
		if m.list.SelectNext() {
			continue
		}
		// If we're at the end and the last item isn't selectable, walk backwards
		// to find the nearest selectable item.
		for {
			if !m.list.SelectPrev() {
				return
			}
			if m.isSelectable(m.list.Selected()) {
				return
			}
		}
	}
}

// SelectPrev selects the previous message in the chat list.
func (m *Chat) SelectPrev() {
	for {
		if !m.list.SelectPrev() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectNext selects the next message in the chat list.
func (m *Chat) SelectNext() {
	for {
		if !m.list.SelectNext() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectPrevUserMessage selects the previous user message in the chat list.
func (m *Chat) SelectPrevUserMessage() bool {
	cur := m.list.Selected()
	for {
		if !m.list.SelectPrev() {
			m.list.SetSelected(cur)
			return false
		}
		item := m.list.ItemAt(m.list.Selected())
		if _, ok := item.(*chat.UserMessageItem); ok {
			return true
		}
	}
}

// SelectNextUserMessage selects the next user message in the chat list.
func (m *Chat) SelectNextUserMessage() bool {
	cur := m.list.Selected()
	for {
		if !m.list.SelectNext() {
			m.list.SetSelected(cur)
			return false
		}
		item := m.list.ItemAt(m.list.Selected())
		if _, ok := item.(*chat.UserMessageItem); ok {
			return true
		}
	}
}

// SelectFirst selects the first message in the chat list.
func (m *Chat) SelectFirst() {
	if !m.list.SelectFirst() {
		return
	}
	if m.isSelectable(m.list.Selected()) {
		return
	}
	for {
		if !m.list.SelectNext() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectLast selects the last message in the chat list.
func (m *Chat) SelectLast() {
	if !m.list.SelectLast() {
		return
	}
	if m.isSelectable(m.list.Selected()) {
		return
	}
	for {
		if !m.list.SelectPrev() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectFirstInView selects the first message currently in view.
func (m *Chat) SelectFirstInView() {
	startIdx, endIdx := m.list.VisibleItemIndices()
	for i := startIdx; i <= endIdx; i++ {
		if m.isSelectable(i) {
			m.list.SetSelected(i)
			return
		}
	}
}

// SelectLastInView selects the last message currently in view.
func (m *Chat) SelectLastInView() {
	startIdx, endIdx := m.list.VisibleItemIndices()
	for i := endIdx; i >= startIdx; i-- {
		if m.isSelectable(i) {
			m.list.SetSelected(i)
			return
		}
	}
}

// ClearMessages removes all messages from the chat list.
func (m *Chat) ClearMessages() {
	m.idInxMap = make(map[string]int)
	m.pausedAnimations = make(map[string]struct{})
	m.list.SetItems()
	m.ClearMouse()
}

// RemoveMessage removes a message from the chat list by its ID.
func (m *Chat) RemoveMessage(id string) {
	idx, ok := m.idInxMap[id]
	if !ok {
		return
	}

	// Collect all indices to remove: primary + related tool calls + info item.
	toRemove := map[int]string{idx: id}
	if _, ok := m.list.ItemAt(idx).(chat.MessageItem); ok {
		for i := 0; i < m.list.Len(); i++ {
			other, ok := m.list.ItemAt(i).(chat.MessageItem)
			if !ok || other.ID() == id {
				continue
			}
			if tool, ok := other.(chat.ToolMessageItem); ok && tool.MessageID() == id {
				toRemove[i] = tool.ID()
			}
		}
		infoID := chat.AssistantInfoID(id)
		if infoIdx, ok := m.idInxMap[infoID]; ok {
			toRemove[infoIdx] = infoID
		}
	}

	// Sort indices descending so removals don't shift earlier indices.
	indices := make([]int, 0, len(toRemove))
	for i := range toRemove {
		indices = append(indices, i)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(indices)))

	for _, i := range indices {
		rid := toRemove[i]
		m.list.RemoveItem(i)
		delete(m.idInxMap, rid)
		delete(m.pausedAnimations, rid)
	}

	// Rebuild index map.
	m.idInxMap = make(map[string]int, m.list.Len())
	for i := 0; i < m.list.Len(); i++ {
		if item, ok := m.list.ItemAt(i).(chat.MessageItem); ok {
			m.idInxMap[item.ID()] = i
		}
	}
}

// RemoveOrphanedTools removes tool items that belong to the given message ID
// but are not in the currentToolIDs set. This happens when a retry clears
// the assistant message's Parts and new tool calls get different IDs.
func (m *Chat) RemoveOrphanedTools(messageID string, currentToolIDs map[string]bool) {
	var orphans []string
	for i := 0; i < m.list.Len(); i++ {
		item, ok := m.list.ItemAt(i).(chat.MessageItem)
		if !ok {
			continue
		}
		tool, ok := item.(chat.ToolMessageItem)
		if !ok {
			continue
		}
		if tool.MessageID() == messageID && !currentToolIDs[tool.ID()] {
			orphans = append(orphans, tool.ID())
		}
	}
	for _, id := range orphans {
		m.RemoveMessage(id)
	}
}

// MessageItem returns the message item with the given ID, or nil if not found.
func (m *Chat) MessageItem(id string) chat.MessageItem {
	idx, ok := m.idInxMap[id]
	if !ok {
		return nil
	}
	item, ok := m.list.ItemAt(idx).(chat.MessageItem)
	if !ok {
		return nil
	}
	return item
}

// InvalidateItemHeight invalidates the cached height for the item with the
// given ID. Call this after mutating an item's content.
func (m *Chat) InvalidateItemHeight(id string) {
	if idx, ok := m.idInxMap[id]; ok {
		m.list.InvalidateItemHeight(idx)
	}
}

// ToggleExpandedSelectedItem expands the selected message item if it is expandable.
func (m *Chat) ToggleExpandedSelectedItem() {
	if expandable, ok := m.list.SelectedItem().(chat.Expandable); ok {
		if !expandable.ToggleExpanded() {
			m.ScrollToIndex(m.list.Selected())
		}
		m.list.InvalidateItemHeight(m.list.Selected())
		if m.AtBottom() {
			m.ScrollToBottom()
		}
	}
}

// HandleKeyMsg handles key events for the chat component.
func (m *Chat) HandleKeyMsg(key tea.KeyMsg) (bool, tea.Cmd) {
	if m.list.Focused() {
		if handler, ok := m.list.SelectedItem().(chat.KeyEventHandler); ok {
			return handler.HandleKeyEvent(key)
		}
	}
	return false, nil
}

// HandleMouseDown handles mouse down events for the chat component.
// It detects single, double, and triple clicks for text selection.
// Returns whether the click was handled and an optional command for delayed
// single-click actions.
func (m *Chat) HandleMouseDown(x, y int) (bool, tea.Cmd) {
	if m.list.Len() == 0 {
		return false, nil
	}

	itemIdx, itemY := m.list.ItemIndexAtPosition(x, y)
	if itemIdx < 0 {
		return false, nil
	}
	if !m.isSelectable(itemIdx) {
		return false, nil
	}

	// Increment pending click ID to invalidate any previous pending clicks.
	m.pendingClickID++
	clickID := m.pendingClickID

	// Detect multi-click (double/triple)
	now := time.Now()
	if now.Sub(m.lastClickTime) <= doubleClickThreshold &&
		abs(x-m.lastClickX) <= clickTolerance &&
		abs(y-m.lastClickY) <= clickTolerance {
		m.clickCount++
	} else {
		m.clickCount = 1
	}
	m.lastClickTime = now
	m.lastClickX = x
	m.lastClickY = y

	// Select the item that was clicked
	m.list.SetSelected(itemIdx)

	var cmd tea.Cmd

	switch m.clickCount {
	case 1:
		// Single click - start selection and schedule delayed click action.
		m.mouseDown = true
		m.mouseDownItem = itemIdx
		m.mouseDownX = x
		m.mouseDownY = itemY
		m.mouseDragItem = itemIdx
		m.mouseDragX = x
		m.mouseDragY = itemY

		// Schedule delayed click action (e.g., expansion) after a short delay.
		// If a double-click occurs, the clickID will be invalidated.
		cmd = tea.Tick(doubleClickThreshold, func(t time.Time) tea.Msg {
			return DelayedClickMsg{
				ClickID: clickID,
				ItemIdx: itemIdx,
				X:       x,
				Y:       itemY,
			}
		})
	case 2:
		// Double click - select word (no delayed action)
		m.selectWord(itemIdx, x, itemY)
	case 3:
		// Triple click - select line (no delayed action)
		m.selectLine(itemIdx, itemY)
		m.clickCount = 0 // Reset after triple click
	}

	return true, cmd
}

// HandleDelayedClick handles a delayed single-click action (like expansion).
// It only executes if the click ID matches (i.e., no double-click occurred)
// and no text selection was made (drag to select).
func (m *Chat) HandleDelayedClick(msg DelayedClickMsg) (bool, tea.Cmd) {
	// Ignore if this click was superseded by a newer click (double/triple).
	if msg.ClickID != m.pendingClickID {
		return false, nil
	}

	// Don't expand if user dragged to select text.
	if m.HasHighlight() {
		return false, nil
	}

	// Execute the click action (e.g., expansion).
	selectedItem := m.list.SelectedItem()
	if clickable, ok := selectedItem.(list.MouseClickable); ok {
		handled := clickable.HandleMouseClick(ansi.MouseButton1, msg.X, msg.Y)

		// Check if the item wants to open a preview.
		// If a preview is opened, skip the toggle expansion.
		if handled {
			if previewable, ok := selectedItem.(chat.ImagePreviewable); ok {
				if att := previewable.PendingImagePreview(); att != nil {
					return true, func() tea.Msg { return ImagePreviewMsg{Attachment: *att} }
				}
			}
			if previewable, ok := selectedItem.(chat.DiffPreviewable); ok {
				if dp := previewable.PendingDiffPreview(); dp != nil {
					return true, func() tea.Msg {
						return DiffPreviewMsg{FilePath: dp.FilePath, OldContent: dp.OldContent, NewContent: dp.NewContent}
					}
				}
			}
			if previewable, ok := selectedItem.(chat.JobPreviewable); ok {
				if jp := previewable.PendingJobPreview(); jp != nil {
					return true, func() tea.Msg {
						return JobPreviewMsg{
							Title:       jp.Title,
							ContentFunc: jp.ContentFunc,
							KillFunc:    jp.KillFunc,
						}
					}
				}
			}
			if previewable, ok := selectedItem.(chat.TextPreviewable); ok {
				if tp := previewable.PendingTextPreview(); tp != nil {
					return true, func() tea.Msg { return TextPreviewMsg{Title: tp.Title, Text: tp.Text} }
				}
			}
		}

		// Toggle expansion if applicable (only when no preview was opened).
		if expandable, ok := selectedItem.(chat.Expandable); ok {
			if !expandable.ToggleExpanded() {
				m.ScrollToIndex(m.list.Selected())
			}
		}
		m.list.InvalidateItemHeight(m.list.Selected())
		if m.AtBottom() {
			m.ScrollToBottom()
		}

		return handled, nil
	}

	return false, nil
}

// HandleMouseUp handles mouse up events for the chat component.
func (m *Chat) HandleMouseUp(x, y int) bool {
	if !m.mouseDown {
		return false
	}

	m.mouseDown = false
	return true
}

// HandleMouseDrag handles mouse drag events for the chat component.
func (m *Chat) HandleMouseDrag(x, y int) bool {
	if !m.mouseDown {
		return false
	}

	if m.list.Len() == 0 {
		return false
	}

	itemIdx, itemY := m.list.ItemIndexAtPosition(x, y)
	if itemIdx < 0 {
		return false
	}

	m.mouseDragItem = itemIdx
	m.mouseDragX = x
	m.mouseDragY = itemY

	return true
}

// HasHighlight returns whether there is currently highlighted content.
func (m *Chat) HasHighlight() bool {
	startItemIdx, startLine, startCol, endItemIdx, endLine, endCol := m.getHighlightRange()
	return startItemIdx >= 0 && endItemIdx >= 0 && (startLine != endLine || startCol != endCol)
}

// HighlightContent returns the currently highlighted content based on the mouse
// selection. It returns an empty string if no content is highlighted.
func (m *Chat) HighlightContent() string {
	startItemIdx, startLine, startCol, endItemIdx, endLine, endCol := m.getHighlightRange()
	if startItemIdx < 0 || endItemIdx < 0 || startLine == endLine && startCol == endCol {
		return ""
	}

	var sb strings.Builder
	for i := startItemIdx; i <= endItemIdx; i++ {
		item := m.list.ItemAt(i)
		if hi, ok := item.(list.Highlightable); ok {
			startLine, startCol, endLine, endCol := hi.Highlight()
			listWidth := m.list.Width()
			var rendered string
			if rr, ok := item.(list.RawRenderable); ok {
				rendered = rr.RawRender(listWidth)
			} else {
				rendered = item.Render(listWidth)
			}
			sb.WriteString(list.HighlightContent(
				rendered,
				uv.Rect(0, 0, listWidth, lipgloss.Height(rendered)),
				startLine,
				startCol,
				endLine,
				endCol,
			))
			sb.WriteString(strings.Repeat("\n", m.list.Gap()))
		}
	}

	return strings.TrimSpace(sb.String())
}

// ClearMouse clears the current mouse interaction state.
func (m *Chat) ClearMouse() {
	m.mouseDown = false
	m.mouseDownItem = -1
	m.mouseDragItem = -1
	m.lastClickTime = time.Time{}
	m.lastClickX = 0
	m.lastClickY = 0
	m.clickCount = 0
	m.pendingClickID++ // Invalidate any pending delayed click
}

// applyHighlightRange applies the current highlight range to the chat items.
func (m *Chat) applyHighlightRange(idx, selectedIdx int, item list.Item) list.Item {
	if hi, ok := item.(list.Highlightable); ok {
		// Apply highlight
		startItemIdx, startLine, startCol, endItemIdx, endLine, endCol := m.getHighlightRange()
		sLine, sCol, eLine, eCol := -1, -1, -1, -1
		if idx >= startItemIdx && idx <= endItemIdx {
			if idx == startItemIdx && idx == endItemIdx {
				// Single item selection
				sLine = startLine
				sCol = startCol
				eLine = endLine
				eCol = endCol
			} else if idx == startItemIdx {
				// First item - from start position to end of item
				sLine = startLine
				sCol = startCol
				eLine = -1
				eCol = -1
			} else if idx == endItemIdx {
				// Last item - from start of item to end position
				sLine = 0
				sCol = 0
				eLine = endLine
				eCol = endCol
			} else {
				// Middle item - fully highlighted
				sLine = 0
				sCol = 0
				eLine = -1
				eCol = -1
			}
		}

		hi.SetHighlight(sLine, sCol, eLine, eCol)
		return hi.(list.Item)
	}

	return item
}

// getHighlightRange returns the current highlight range.
func (m *Chat) getHighlightRange() (startItemIdx, startLine, startCol, endItemIdx, endLine, endCol int) {
	if m.mouseDownItem < 0 {
		return -1, -1, -1, -1, -1, -1
	}

	downItemIdx := m.mouseDownItem
	dragItemIdx := m.mouseDragItem

	// Determine selection direction
	draggingDown := dragItemIdx > downItemIdx ||
		(dragItemIdx == downItemIdx && m.mouseDragY > m.mouseDownY) ||
		(dragItemIdx == downItemIdx && m.mouseDragY == m.mouseDownY && m.mouseDragX >= m.mouseDownX)

	if draggingDown {
		// Normal forward selection
		startItemIdx = downItemIdx
		startLine = m.mouseDownY
		startCol = m.mouseDownX
		endItemIdx = dragItemIdx
		endLine = m.mouseDragY
		endCol = m.mouseDragX
	} else {
		// Backward selection (dragging up)
		startItemIdx = dragItemIdx
		startLine = m.mouseDragY
		startCol = m.mouseDragX
		endItemIdx = downItemIdx
		endLine = m.mouseDownY
		endCol = m.mouseDownX
	}

	return startItemIdx, startLine, startCol, endItemIdx, endLine, endCol
}

// selectWord selects the word at the given position within an item.
func (m *Chat) selectWord(itemIdx, x, itemY int) {
	item := m.list.ItemAt(itemIdx)
	if item == nil {
		return
	}

	// Get the rendered content for this item
	var rendered string
	if rr, ok := item.(list.RawRenderable); ok {
		rendered = rr.RawRender(m.list.Width())
	} else {
		rendered = item.Render(m.list.Width())
	}

	lines := strings.Split(rendered, "\n")
	if itemY < 0 || itemY >= len(lines) {
		return
	}

	// Adjust x for the item's left padding (border + padding) to get content column.
	// The mouse x is in viewport space, but we need content space for boundary detection.
	offset := chat.MessageLeftPaddingTotal
	contentX := max(x-offset, 0)

	line := ansi.Strip(lines[itemY])
	startCol, endCol := findWordBoundaries(line, contentX)
	if startCol == endCol {
		// No word found at position, fallback to single click behavior
		m.mouseDown = true
		m.mouseDownItem = itemIdx
		m.mouseDownX = x
		m.mouseDownY = itemY
		m.mouseDragItem = itemIdx
		m.mouseDragX = x
		m.mouseDragY = itemY
		return
	}

	// Set selection to the word boundaries (convert back to viewport space).
	// Keep mouseDown true so HandleMouseUp triggers the copy.
	m.mouseDown = true
	m.mouseDownItem = itemIdx
	m.mouseDownX = startCol + offset
	m.mouseDownY = itemY
	m.mouseDragItem = itemIdx
	m.mouseDragX = endCol + offset
	m.mouseDragY = itemY
}

// selectLine selects the entire line at the given position within an item.
func (m *Chat) selectLine(itemIdx, itemY int) {
	item := m.list.ItemAt(itemIdx)
	if item == nil {
		return
	}

	// Get the rendered content for this item
	var rendered string
	if rr, ok := item.(list.RawRenderable); ok {
		rendered = rr.RawRender(m.list.Width())
	} else {
		rendered = item.Render(m.list.Width())
	}

	lines := strings.Split(rendered, "\n")
	if itemY < 0 || itemY >= len(lines) {
		return
	}

	// Get line length (stripped of ANSI codes) and account for padding.
	// SetHighlight will subtract the offset, so we need to add it here.
	offset := chat.MessageLeftPaddingTotal
	lineLen := ansi.StringWidth(lines[itemY])

	// Set selection to the entire line.
	// Keep mouseDown true so HandleMouseUp triggers the copy.
	m.mouseDown = true
	m.mouseDownItem = itemIdx
	m.mouseDownX = 0
	m.mouseDownY = itemY
	m.mouseDragItem = itemIdx
	m.mouseDragX = lineLen + offset
	m.mouseDragY = itemY
}

// findWordBoundaries finds the start and end column of the word at the given column.
// Returns (startCol, endCol) where endCol is exclusive.
func findWordBoundaries(line string, col int) (startCol, endCol int) {
	if line == "" || col < 0 {
		return 0, 0
	}

	i := displaywidth.StringGraphemes(line)
	for i.Next() {
	}

	// Segment the line into words using UAX#29.
	lineCol := 0 // tracks the visited column widths
	lastCol := 0 // tracks the start of the current token
	iter := words.FromString(line)
	for iter.Next() {
		token := iter.Value()
		tokenWidth := displaywidth.String(token)

		graphemeStart := lineCol
		graphemeEnd := lineCol + tokenWidth
		lineCol += tokenWidth

		// If clicked before this token, return the previous token boundaries.
		if col < graphemeStart {
			return lastCol, lastCol
		}

		// Update lastCol to the end of this token for next iteration.
		lastCol = graphemeEnd

		// If clicked within this token, return its boundaries.
		if col >= graphemeStart && col < graphemeEnd {
			// If clicked on whitespace, return empty selection.
			if strings.TrimSpace(token) == "" {
				return col, col
			}
			return graphemeStart, graphemeEnd
		}
	}

	return col, col
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
