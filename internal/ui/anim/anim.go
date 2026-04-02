// Package anim provides an animated spinner.
package anim

import (
	"fmt"
	"image/color"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/crush/internal/csync"
)

const (
	fps           = 10
	labelGap      = " "
	labelGapWidth = 1

	// Periods of ellipsis animation speed in steps.
	ellipsisAnimSpeed = 4
)

var (
	defaultLabelColor = color.RGBA{R: 0xcc, G: 0xcc, B: 0xcc, A: 0xff}

	braileFrames   = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
	ellipsisFrames = []string{".", "..", "...", ""}
)

// Internal ID management.
var lastID int64

func nextID() int {
	return int(atomic.AddInt64(&lastID, 1))
}

// StepMsg is a message type used to trigger the next step in the animation.
type StepMsg struct{ ID string }

// Settings defines settings for the animation.
type Settings struct {
	ID          string
	Size        int
	Label       string
	LabelColor  color.Color
	GradColorA  color.Color
	GradColorB  color.Color
	CycleColors bool
}

// Anim is a Bubble for an animated braille spinner.
type Anim struct {
	width          int
	label          *csync.Slice[string]
	labelWidth     int
	labelColor     color.Color
	spinnerColor   color.Color
	ellipsisFrames *csync.Slice[string]
	step           atomic.Int64
	ellipsisStep   atomic.Int64
	id             string
}

// New creates a new Anim instance.
func New(opts Settings) *Anim {
	a := &Anim{}

	if colorIsUnset(opts.LabelColor) {
		opts.LabelColor = defaultLabelColor
	}
	if colorIsUnset(opts.GradColorA) {
		opts.GradColorA = defaultLabelColor
	}

	if opts.ID != "" {
		a.id = opts.ID
	} else {
		a.id = fmt.Sprintf("%d", nextID())
	}

	a.labelColor = opts.LabelColor
	a.spinnerColor = opts.GradColorA
	a.labelWidth = lipgloss.Width(opts.Label)

	// Total width: 1 (spinner) + gap + label
	a.width = 1
	if opts.Label != "" {
		a.width += labelGapWidth + a.labelWidth
	}

	a.renderLabel(opts.Label)
	return a
}

// SetSpinnerColor updates the spinner color.
func (a *Anim) SetSpinnerColor(c color.Color) {
	a.spinnerColor = c
}

// SetLabel updates the label text and re-renders it.
func (a *Anim) SetLabel(newLabel string) {
	a.labelWidth = lipgloss.Width(newLabel)
	a.width = 1
	if newLabel != "" {
		a.width += labelGapWidth + a.labelWidth
	}
	a.renderLabel(newLabel)
}

// renderLabel renders the label with the current label color.
func (a *Anim) renderLabel(label string) {
	if a.labelWidth > 0 {
		labelRunes := []rune(label)
		a.label = csync.NewSlice[string]()
		for i := range labelRunes {
			rendered := lipgloss.NewStyle().
				Foreground(a.labelColor).
				Render(string(labelRunes[i]))
			a.label.Append(rendered)
		}

		a.ellipsisFrames = csync.NewSlice[string]()
		for _, frame := range ellipsisFrames {
			rendered := lipgloss.NewStyle().
				Foreground(a.labelColor).
				Render(frame)
			a.ellipsisFrames.Append(rendered)
		}
	} else {
		a.label = csync.NewSlice[string]()
		a.ellipsisFrames = csync.NewSlice[string]()
	}
}

// Width returns the total width of the animation.
func (a *Anim) Width() (w int) {
	w = a.width
	if a.labelWidth > 0 {
		var widestEllipsisFrame int
		for _, f := range ellipsisFrames {
			fw := lipgloss.Width(f)
			if fw > widestEllipsisFrame {
				widestEllipsisFrame = fw
			}
		}
		w += widestEllipsisFrame
	}
	return w
}

// Start starts the animation.
func (a *Anim) Start() tea.Cmd {
	return a.Step()
}

// Animate advances the animation to the next step.
func (a *Anim) Animate(msg StepMsg) tea.Cmd {
	if msg.ID != a.id {
		return nil
	}

	step := a.step.Add(1)
	if int(step) >= len(braileFrames) {
		a.step.Store(0)
	}

	if a.labelWidth > 0 {
		ellipsisStep := a.ellipsisStep.Add(1)
		if int(ellipsisStep) >= ellipsisAnimSpeed*len(ellipsisFrames) {
			a.ellipsisStep.Store(0)
		}
	}

	return a.Step()
}

// Render renders the current state of the animation.
func (a *Anim) Render() string {
	var b strings.Builder
	step := int(a.step.Load())

	// Render braille spinner character
	frame := braileFrames[step%len(braileFrames)]
	b.WriteString(lipgloss.NewStyle().
		Foreground(a.spinnerColor).
		Render(string(frame)))

	// Render label
	if a.labelWidth > 0 {
		b.WriteString(labelGap)
		for i := range a.labelWidth {
			if labelChar, ok := a.label.Get(i); ok {
				b.WriteString(labelChar)
			}
		}

		// Render animated ellipsis
		ellipsisStep := int(a.ellipsisStep.Load())
		if ellipsisFrame, ok := a.ellipsisFrames.Get(ellipsisStep / ellipsisAnimSpeed); ok {
			b.WriteString(ellipsisFrame)
		}
	}

	return b.String()
}

// Step is a command that triggers the next step in the animation.
func (a *Anim) Step() tea.Cmd {
	return tea.Tick(time.Second/time.Duration(fps), func(t time.Time) tea.Msg {
		return StepMsg{ID: a.id}
	})
}

func colorIsUnset(c color.Color) bool {
	if c == nil {
		return true
	}
	_, _, _, a := c.RGBA()
	return a == 0
}
