package dialog

import (
	"image"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
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
	contentArea   image.Rectangle

	// Mouse selection state.
	mouseDown bool
	startLine int
	startCol  int
	endLine   int
	endCol    int
	hasSelect bool

	km struct {
		Close      key.Binding
		ScrollUp   key.Binding
		ScrollDown key.Binding
		Copy       key.Binding
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
		startLine:     -1,
	}
	d.km.Close = key.NewBinding(
		key.WithKeys("ctrl+g", "q", "esc"),
		key.WithHelp("ctrl+g/q", "close"),
	)
	d.km.ScrollUp = key.NewBinding(key.WithKeys("up", "k"))
	d.km.ScrollDown = key.NewBinding(key.WithKeys("down", "j"))
	d.km.Copy = key.NewBinding(key.WithKeys("c", "y"))
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
		if key.Matches(msg, d.km.Copy) {
			return ActionCmd{common.CopyToClipboard(d.content, "Copied to clipboard")}
		}
		d.viewport, _ = d.viewport.Update(msg)
	case tea.MouseWheelMsg:
		d.viewport, _ = d.viewport.Update(msg)
	case tea.MouseClickMsg:
		pt := image.Pt(msg.X, msg.Y)
		if !pt.In(d.contentArea) {
			return ActionClose{}
		}
		col := msg.X - d.contentArea.Min.X
		line := msg.Y - d.contentArea.Min.Y + d.viewport.YOffset()
		d.mouseDown = true
		d.startLine = line
		d.startCol = col
		d.endLine = line
		d.endCol = col
		d.hasSelect = false
	case tea.MouseMotionMsg:
		if !d.mouseDown {
			return nil
		}
		col := msg.X - d.contentArea.Min.X
		line := msg.Y - d.contentArea.Min.Y + d.viewport.YOffset()
		d.endLine = line
		d.endCol = col
		d.hasSelect = d.startLine != d.endLine || d.startCol != d.endCol
	case tea.MouseReleaseMsg:
		if !d.mouseDown {
			return nil
		}
		d.mouseDown = false
		if d.hasSelect {
			if text := d.selectedText(); text != "" {
				d.hasSelect = false
				return ActionCmd{common.CopyToClipboard(text, "Selected text copied to clipboard")}
			}
		}
	}
	return nil
}

func (d *TextPreview) normalizedSelection() (sLine, sCol, eLine, eCol int) {
	sLine, sCol = d.startLine, d.startCol
	eLine, eCol = d.endLine, d.endCol
	if sLine > eLine || (sLine == eLine && sCol > eCol) {
		sLine, sCol, eLine, eCol = eLine, eCol, sLine, sCol
	}
	return
}

func (d *TextPreview) selectedText() string {
	sLine, sCol, eLine, eCol := d.normalizedSelection()
	rendered := d.viewport.View()
	w := d.contentArea.Dx()
	h := d.contentArea.Dy()
	area := image.Rect(0, 0, w, h)
	visStartLine := sLine - d.viewport.YOffset()
	visEndLine := eLine - d.viewport.YOffset()
	return strings.TrimRight(list.HighlightContent(rendered, area, visStartLine, sCol, visEndLine, eCol), "\n")
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

	helpView := t.Dialog.HelpView.Width(contentWidth).Render("esc/q: close · j/k: scroll · c: copy · drag: select")
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

	if d.hasSelect {
		sLine, sCol, eLine, eCol := d.normalizedSelection()
		visStartLine := sLine - d.viewport.YOffset()
		visEndLine := eLine - d.viewport.YOffset()
		w := contentWidth - 1
		h := availableHeight
		hlArea := image.Rect(0, 0, w, h)
		content = list.Highlight(content, hlArea, visStartLine, sCol, visEndLine, eCol, list.ToHighlighter(t.TextSelection))
	}

	needsScrollbar := d.viewport.TotalLineCount() > availableHeight
	if needsScrollbar {
		scrollbar := common.Scrollbar(t, availableHeight, d.viewport.TotalLineCount(), availableHeight, d.viewport.YOffset())
		content = lipgloss.JoinHorizontal(lipgloss.Top, content, scrollbar)
	}

	parts := []string{titleRendered, "", content, "", helpView}
	innerContent := lipgloss.JoinVertical(lipgloss.Left, parts...)
	rendered := dialogStyle.Render(innerContent)

	width, height := lipgloss.Size(rendered)
	center := common.CenterRect(area, width, height)

	pad := dialogStyle.GetHorizontalPadding()
	contentTopY := center.Min.Y + titleHeight + 2
	d.contentArea = image.Rect(
		center.Min.X+pad,
		contentTopY,
		center.Min.X+pad+contentWidth-1,
		contentTopY+availableHeight,
	)

	uv.NewStyledString(rendered).Draw(scr, center)
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
