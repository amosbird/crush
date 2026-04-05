package chat

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

const PlaceholderID = "__placeholder_spinner__"

// PlaceholderItem is a temporary spinner shown immediately after the user sends
// a message, before the backend creates the real assistant message. It is
// removed as soon as the first assistant CreatedEvent arrives.
type PlaceholderItem struct {
	anim *anim.Anim
}

var _ MessageItem = (*PlaceholderItem)(nil)

func NewPlaceholderItem(sty *styles.Styles) *PlaceholderItem {
	return &PlaceholderItem{
		anim: anim.New(anim.Settings{
			ID:          PlaceholderID,
			Size:        15,
			GradColorA:  sty.Primary,
			GradColorB:  sty.Secondary,
			LabelColor:  sty.FgBase,
			CycleColors: true,
		}),
	}
}

func (p *PlaceholderItem) ID() string { return PlaceholderID }

func (p *PlaceholderItem) StartAnimation() tea.Cmd {
	return p.anim.Start()
}

func (p *PlaceholderItem) Animate(msg anim.StepMsg) tea.Cmd {
	return p.anim.Animate(msg)
}

func (p *PlaceholderItem) Render(width int) string {
	return p.anim.Render()
}

func (p *PlaceholderItem) RawRender(width int) string {
	return p.anim.Render()
}
