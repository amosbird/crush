package dialog

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
)

// TextPreviewID is the unique identifier for the text preview dialog.
const TextPreviewID = "text-preview"

// TextPreview is a scrollable full-screen text preview dialog.
type TextPreview struct {
	com *common.Common

	title    string
	content  string
	viewport viewport.Model

	viewportDirty bool

	km struct {
		Close      key.Binding
		ScrollUp   key.Binding
		ScrollDown key.Binding
	}
}

var _ Dialog = (*TextPreview)(nil)

// NewTextPreview creates a new TextPreview dialog.
func NewTextPreview(com *common.Common, title, content string) *TextPreview {
	vp := viewport.New()
	vp.KeyMap = viewport.KeyMap{
		Up:           key.NewBinding(key.WithKeys("up", "k")),
		Down:         key.NewBinding(key.WithKeys("down", "j")),
		PageUp:       key.NewBinding(key.WithKeys("pgup")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown")),
		HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u")),
		HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d")),
		Left:         key.NewBinding(key.WithDisabled()),
		Right:        key.NewBinding(key.WithDisabled()),
	}

	d := &TextPreview{
		com:           com,
		title:         title,
		content:       content,
		viewport:      vp,
		viewportDirty: true,
	}
	d.km.Close = key.NewBinding(
		key.WithKeys("esc", "alt+esc", "q"),
		key.WithHelp("esc/q", "close"),
	)
	d.km.ScrollUp = key.NewBinding(key.WithKeys("up", "k"))
	d.km.ScrollDown = key.NewBinding(key.WithKeys("down", "j"))
	return d
}

// ID implements [Dialog].
func (d *TextPreview) ID() string { return TextPreviewID }

// HandleMsg implements [Dialog].
func (d *TextPreview) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, d.km.Close) {
			return ActionClose{}
		}
		d.viewport, _ = d.viewport.Update(msg)
	case tea.MouseWheelMsg:
		d.viewport, _ = d.viewport.Update(msg)
	case tea.MouseClickMsg:
		return ActionClose{}
	}
	return nil
}

// Draw implements [Dialog].
func (d *TextPreview) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	maxWidth := min(120, area.Dx())
	maxHeight := area.Dy()

	dialogStyle := t.Dialog.View.Width(maxWidth).Padding(0, 1)
	contentWidth := maxWidth - dialogStyle.GetHorizontalFrameSize()

	title := common.DialogTitle(t, d.title, contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	titleRendered := t.Dialog.Title.Render(title)
	titleHeight := lipgloss.Height(titleRendered)

	helpView := t.Dialog.HelpView.Width(contentWidth).Render("esc/q: close · j/k: scroll · pgup/pgdn: page")
	helpHeight := lipgloss.Height(helpView)

	frameHeight := dialogStyle.GetVerticalFrameSize() + 2
	availableHeight := max(3, maxHeight-titleHeight-helpHeight-frameHeight)

	if d.viewportDirty || d.viewport.Width() != contentWidth-1 {
		rendered := d.renderContent(contentWidth - 1)
		d.viewport.SetWidth(contentWidth - 1)
		d.viewport.SetHeight(availableHeight)
		d.viewport.SetContent(rendered)
		d.viewportDirty = false
	} else {
		d.viewport.SetHeight(availableHeight)
	}

	content := d.viewport.View()
	needsScrollbar := d.viewport.TotalLineCount() > availableHeight
	if needsScrollbar {
		scrollbar := common.Scrollbar(t, availableHeight, d.viewport.TotalLineCount(), availableHeight, d.viewport.YOffset())
		content = lipgloss.JoinHorizontal(lipgloss.Top, content, scrollbar)
	}

	parts := []string{titleRendered, "", content, "", helpView}
	innerContent := lipgloss.JoinVertical(lipgloss.Left, parts...)
	DrawCenter(scr, area, dialogStyle.Render(innerContent))
	return nil
}

func (d *TextPreview) renderContent(width int) string {
	renderer := common.MarkdownRenderer(d.com.Styles, width)
	result, err := renderer.Render(d.content)
	if err != nil {
		return d.content
	}
	return strings.TrimSuffix(result, "\n")
}
