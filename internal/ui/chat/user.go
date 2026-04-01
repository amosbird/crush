package chat

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// UserMessageItem represents a user message in the chat UI.
type UserMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	attachments        *attachments.Renderer
	message            *message.Message
	sty                *styles.Styles
	pendingPreview     *message.Attachment
	pendingTextPreview *TextPreviewContent
}

// NewUserMessageItem creates a new UserMessageItem.
func NewUserMessageItem(sty *styles.Styles, message *message.Message, attachments *attachments.Renderer) MessageItem {
	return &UserMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		attachments:              attachments,
		message:                  message,
		sty:                      sty,
	}
}

// RawRender implements [MessageItem].
func (m *UserMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)

	content, height, ok := m.getCachedRender(cappedWidth)
	// cache hit
	if ok {
		return m.renderHighlighted(content, cappedWidth, height)
	}

	renderer := common.MarkdownRenderer(m.sty, cappedWidth)

	msgContent := strings.TrimSpace(m.message.Content().Text)
	result, err := renderer.Render(msgContent)
	if err != nil {
		content = msgContent
	} else {
		content = strings.TrimSuffix(result, "\n")
	}

	if len(m.message.BinaryContent()) > 0 {
		attachmentsStr := m.renderAttachments(cappedWidth)
		if content == "" {
			content = attachmentsStr
		} else {
			content = strings.Join([]string{content, "", attachmentsStr}, "\n")
		}
	}

	height = lipgloss.Height(content)
	m.setCachedRender(content, cappedWidth, height)
	return m.renderHighlighted(content, cappedWidth, height)
}

// Render implements MessageItem.
func (m *UserMessageItem) Render(width int) string {
	var prefix string
	if m.focused {
		prefix = m.sty.Chat.Message.UserFocused.Render()
	} else {
		prefix = m.sty.Chat.Message.UserBlurred.Render()
	}
	lines := strings.Split(m.RawRender(width), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// ID implements MessageItem.
func (m *UserMessageItem) ID() string {
	return m.message.ID
}

// renderAttachments renders attachments.
func (m *UserMessageItem) renderAttachments(width int) string {
	var attachments []message.Attachment
	for _, at := range m.message.BinaryContent() {
		attachments = append(attachments, message.Attachment{
			FileName: at.Path,
			MimeType: at.MIMEType,
		})
	}
	return m.attachments.Render(attachments, false, width)
}

// HandleKeyEvent implements KeyEventHandler.
func (m *UserMessageItem) HandleKeyEvent(key tea.KeyMsg) (bool, tea.Cmd) {
	if k := key.String(); k == "c" || k == "y" {
		text := m.message.Content().Text
		return true, common.CopyToClipboard(text, "Message copied to clipboard")
	}
	return false, nil
}

// HandleMouseClick implements [list.MouseClickable].
func (m *UserMessageItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	if btn != ansi.MouseLeft {
		return false
	}
	m.pendingPreview = nil
	m.pendingTextPreview = nil

	binaries := m.message.BinaryContent()
	if len(binaries) == 0 {
		return false
	}

	// Use cached render height to determine where attachments start.
	// The attachment line is always at the bottom of the rendered item.
	_, cachedHeight, hasCached := m.getCachedRender(m.width)
	if hasCached {
		attachmentHeight := lipgloss.Height(m.renderAttachments(m.width))
		textHeight := cachedHeight - attachmentHeight
		if y < textHeight {
			return false
		}
	}

	for _, bc := range binaries {
		if strings.HasPrefix(bc.MIMEType, "image/") {
			att := message.Attachment{
				FilePath: bc.Path,
				FileName: filepath.Base(bc.Path),
				MimeType: bc.MIMEType,
				Content:  bc.Data,
			}
			m.pendingPreview = &att
			return true
		}
		if strings.HasPrefix(bc.MIMEType, "text/") || strings.HasSuffix(bc.Path, ".txt") {
			m.pendingTextPreview = &TextPreviewContent{
				Title: filepath.Base(bc.Path),
				Text:  string(bc.Data),
			}
			return true
		}
	}
	return false
}

// PendingImagePreview implements [ImagePreviewable].
func (m *UserMessageItem) PendingImagePreview() *message.Attachment {
	att := m.pendingPreview
	m.pendingPreview = nil
	return att
}

// PendingTextPreview implements [TextPreviewable].
func (m *UserMessageItem) PendingTextPreview() *TextPreviewContent {
	tp := m.pendingTextPreview
	m.pendingTextPreview = nil
	return tp
}
