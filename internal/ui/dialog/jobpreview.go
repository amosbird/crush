package dialog

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
)

// JobPreviewID is the unique identifier for the job preview dialog.
const JobPreviewID = "job-preview"

const jobPreviewRefreshInterval = 500 * time.Millisecond

// jobPreviewTickMsg triggers a content refresh in the job preview dialog.
type jobPreviewTickMsg struct{}

// JobPreviewContentResult holds the content and state returned by a
// content provider function.
type JobPreviewContentResult struct {
	Content string
	Done    bool
}

// JobPreviewContentFunc is called on each refresh to get the current
// content and done state for the preview.
type JobPreviewContentFunc func() JobPreviewContentResult

// JobPreviewKillFunc is called when the user presses ctrl+c to kill.
// It should return the final content to display.
type JobPreviewKillFunc func() string

// JobPreview is a live-updating preview dialog.
type JobPreview struct {
	com *common.Common

	title       string
	viewport    viewport.Model
	follow      bool
	done        bool
	killed      bool
	contentFunc JobPreviewContentFunc
	killFunc    JobPreviewKillFunc

	km struct {
		Close key.Binding
		Kill  key.Binding
	}
}

var _ Dialog = (*JobPreview)(nil)

// NewJobPreview creates a new live-updating preview dialog.
func NewJobPreview(com *common.Common, title string, contentFunc JobPreviewContentFunc, killFunc JobPreviewKillFunc) (*JobPreview, tea.Cmd) {
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

	d := &JobPreview{
		com:         com,
		title:       title,
		viewport:    vp,
		follow:      true,
		contentFunc: contentFunc,
		killFunc:    killFunc,
	}
	d.km.Close = key.NewBinding(
		key.WithKeys("ctrl+g", "q", "esc"),
	)
	d.km.Kill = key.NewBinding(
		key.WithKeys("ctrl+c"),
	)

	d.refreshContent()
	return d, d.tickCmd()
}

// ID implements [Dialog].
func (d *JobPreview) ID() string { return JobPreviewID }

// HandleMsg implements [Dialog].
func (d *JobPreview) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, d.km.Close) {
			return ActionClose{}
		}
		if key.Matches(msg, d.km.Kill) && !d.done && d.killFunc != nil {
			content := d.killFunc()
			d.killed = true
			d.done = true
			d.viewport.SetContent(content)
			if d.follow {
				d.viewport.GotoBottom()
			}
			return nil
		}
		prevOffset := d.viewport.YOffset()
		d.viewport, _ = d.viewport.Update(msg)
		if d.viewport.YOffset() != prevOffset {
			d.follow = d.viewport.AtBottom()
		}
	case tea.MouseWheelMsg:
		prevOffset := d.viewport.YOffset()
		d.viewport, _ = d.viewport.Update(msg)
		if d.viewport.YOffset() != prevOffset {
			d.follow = d.viewport.AtBottom()
		}
	case jobPreviewTickMsg:
		d.refreshContent()
		if !d.done {
			return ActionCmd{d.tickCmd()}
		}
	}
	return nil
}

func (d *JobPreview) refreshContent() {
	if d.killed {
		return
	}
	result := d.contentFunc()
	d.done = result.Done
	d.viewport.SetContent(result.Content)
	if d.follow {
		d.viewport.GotoBottom()
	}
}

func (d *JobPreview) tickCmd() tea.Cmd {
	return tea.Tick(jobPreviewRefreshInterval, func(time.Time) tea.Msg {
		return jobPreviewTickMsg{}
	})
}

// Draw implements [Dialog].
func (d *JobPreview) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles

	maxWidth := min(140, area.Dx())
	maxHeight := area.Dy()

	dialogStyle := t.Dialog.View.Width(maxWidth).Padding(0, 1)
	contentWidth := maxWidth - dialogStyle.GetHorizontalFrameSize()

	statusIcon := "●"
	if d.done {
		statusIcon = "✓"
		if d.killed {
			statusIcon = "✕"
		}
	}
	titleText := statusIcon + " " + d.title
	title := common.DialogTitle(t, titleText, contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	titleRendered := t.Dialog.Title.Render(title)
	titleHeight := lipgloss.Height(titleRendered)

	helpParts := []string{"esc/q: close", "j/k: scroll"}
	if !d.done && d.killFunc != nil {
		helpParts = append(helpParts, "ctrl+c: kill")
	}
	helpView := t.Dialog.HelpView.Width(contentWidth).Render(strings.Join(helpParts, " · "))
	helpHeight := lipgloss.Height(helpView)

	frameHeight := dialogStyle.GetVerticalFrameSize() + 2
	availableHeight := max(3, maxHeight-titleHeight-helpHeight-frameHeight)

	d.viewport.SetWidth(contentWidth - 1)
	d.viewport.SetHeight(availableHeight)

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
