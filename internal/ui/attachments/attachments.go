package attachments

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/x/ansi"
)

const maxFilename = 15

type Keymap struct {
	DeleteMode,
	DeleteAll,
	Escape key.Binding
}

func New(renderer *Renderer, keyMap Keymap) *Attachments {
	return &Attachments{
		keyMap:   keyMap,
		renderer: renderer,
	}
}

type Attachments struct {
	renderer *Renderer
	keyMap   Keymap
	list     []message.Attachment
	deleting bool

	cachedRender string
	cachedWidth  int
	cacheDirty   bool
}

func (m *Attachments) List() []message.Attachment { return m.list }

func (m *Attachments) Reset() {
	m.list = nil
	m.cacheDirty = true
}

func (m *Attachments) Update(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case message.Attachment:
		m.list = append(m.list, msg)
		m.cacheDirty = true
		return true
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keyMap.DeleteMode):
			if len(m.list) > 0 {
				m.deleting = true
				m.cacheDirty = true
			}
			return true
		case m.deleting && key.Matches(msg, m.keyMap.Escape):
			m.deleting = false
			m.cacheDirty = true
			return true
		case m.deleting && key.Matches(msg, m.keyMap.DeleteAll):
			m.deleting = false
			m.list = nil
			m.cacheDirty = true
			return true
		case m.deleting:
			r := msg.Code
			if r >= '0' && r <= '9' {
				num := int(r - '0')
				if num < len(m.list) {
					m.list = slices.Delete(m.list, num, num+1)
					m.cacheDirty = true
				}
				m.deleting = false
			}
			return true
		}
	}
	return false
}

func (m *Attachments) HandleClick(x int) bool {
	if len(m.list) == 0 {
		return false
	}
	offset := 0
	for i, att := range m.list {
		iconWidth := lipgloss.Width(m.renderer.icon(att).String())
		if x >= offset && x < offset+iconWidth {
			m.list = slices.Delete(m.list, i, i+1)
			m.cacheDirty = true
			return true
		}
		offset += m.renderer.chipWidth(att)
	}
	return false
}

// AttachmentAt returns the attachment at the given x coordinate, or nil if
// no attachment occupies that position.
func (m *Attachments) AttachmentAt(x int) *message.Attachment {
	offset := 0
	for i, att := range m.list {
		w := m.renderer.chipWidth(att)
		if x >= offset && x < offset+w {
			return &m.list[i]
		}
		offset += w
	}
	return nil
}

func (m *Attachments) Render(width int) string {
	if !m.cacheDirty && m.cachedWidth == width && m.cachedRender != "" {
		return m.cachedRender
	}
	m.cachedRender = m.renderer.Render(m.list, m.deleting, width)
	m.cachedWidth = width
	m.cacheDirty = false
	return m.cachedRender
}

func NewRenderer(normalStyle, deletingStyle, imageStyle, textStyle lipgloss.Style) *Renderer {
	return &Renderer{
		normalStyle:   normalStyle,
		textStyle:     textStyle,
		imageStyle:    imageStyle,
		deletingStyle: deletingStyle,
	}
}

type Renderer struct {
	normalStyle, textStyle, imageStyle, deletingStyle lipgloss.Style
}

func (r *Renderer) Render(attachments []message.Attachment, deleting bool, width int) string {
	var chips []string

	maxItemWidth := lipgloss.Width(r.imageStyle.String() + r.normalStyle.Render(strings.Repeat("x", maxFilename)))
	fits := int(math.Floor(float64(width)/float64(maxItemWidth))) - 1

	for i, att := range attachments {
		filename := filepath.Base(att.FileName)
		// Truncate if needed.
		if ansi.StringWidth(filename) > maxFilename {
			filename = ansi.Truncate(filename, maxFilename, "…")
		}

		if deleting {
			chips = append(
				chips,
				r.deletingStyle.Render(fmt.Sprintf("%d", i)),
				r.normalStyle.Render(filename),
			)
		} else {
			chips = append(
				chips,
				r.icon(att).String(),
				r.normalStyle.Render(filename),
			)
		}

		if i == fits && len(attachments) > i {
			chips = append(chips, lipgloss.NewStyle().Width(maxItemWidth).Render(fmt.Sprintf("%d more…", len(attachments)-fits)))
			break
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, chips...)
}

func (r *Renderer) icon(a message.Attachment) lipgloss.Style {
	if a.IsImage() {
		return r.imageStyle
	}
	return r.textStyle
}

func (r *Renderer) chipWidth(att message.Attachment) int {
	filename := filepath.Base(att.FileName)
	if ansi.StringWidth(filename) > maxFilename {
		filename = ansi.Truncate(filename, maxFilename, "…")
	}
	return lipgloss.Width(r.icon(att).String()) + lipgloss.Width(r.normalStyle.Render(filename))
}
