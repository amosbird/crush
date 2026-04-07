package dialog

import (
	"os"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
)

const OpenDirectoryID = "open_directory"

// ActionOpenDirectory is returned when a directory is selected.
type ActionOpenDirectory struct {
	Path string // absolute path
}

// OpenDirectory is a dialog for selecting a directory to open crush in.
type OpenDirectory struct {
	com      *common.Common
	help     help.Model
	list     *list.FilterableList
	input    textinput.Model
	projects []projects.Project
	pathMode bool // true when input looks like a path

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*OpenDirectory)(nil)

func NewOpenDirectory(com *common.Common) *OpenDirectory {
	d := new(OpenDirectory)
	d.com = com

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.list = list.NewFilterableList()
	d.list.Focus()

	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.Placeholder = "Search projects or enter path…"
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Focus()

	d.keyMap.Select = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "open"),
	)
	d.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next"),
	)
	d.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous"),
	)
	d.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑↓", "navigate"),
	)
	d.keyMap.Close = CloseKey

	// Load projects
	projs, _ := projects.List()
	d.projects = projs
	d.list.SetItems(projectItems(com.Styles, projs)...)

	return d
}

func (d *OpenDirectory) ID() string { return OpenDirectoryID }

func (d *OpenDirectory) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Close):
			return ActionClose{}

		case key.Matches(msg, d.keyMap.Previous):
			d.list.Focus()
			if d.list.IsSelectedFirst() {
				d.list.SelectLast()
			} else {
				d.list.SelectPrev()
			}
			d.list.ScrollToSelected()

		case key.Matches(msg, d.keyMap.Next):
			d.list.Focus()
			if d.list.IsSelectedLast() {
				d.list.SelectFirst()
			} else {
				d.list.SelectNext()
			}
			d.list.ScrollToSelected()

		case key.Matches(msg, d.keyMap.Select):
			// If there's a selected item, use it
			if item := d.list.SelectedItem(); item != nil {
				dirItem := item.(*DirectoryItem)
				path := expandHome(dirItem.Path)
				return ActionOpenDirectory{Path: path}
			}
			// Otherwise try using the raw input as a path
			input := d.input.Value()
			if input != "" {
				path := expandHome(input)
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					return ActionOpenDirectory{Path: path}
				}
				return ActionCmd{util.ReportWarn("Not a valid directory")}
			}

		default:
			var cmd tea.Cmd
			d.input, cmd = d.input.Update(msg)
			value := d.input.Value()

			if isPathInput(value) {
				// Path mode: list subdirectories
				d.pathMode = true
				if dir, ok := resolveDir(value); ok {
					d.list.SetItems(directoryItems(d.com.Styles, dir)...)
				} else {
					d.list.SetItems()
				}
				// Filter by basename for partial input
				expanded := expandHome(value)
				if info, err := os.Stat(expanded); err != nil || !info.IsDir() {
					d.list.SetFilter(expanded[len(expandHome(value[:len(value)-len(baseName(value))])):])
				}
				d.list.SelectFirst()
				d.list.ScrollToTop()
			} else {
				// Fuzzy filter mode: search known projects
				d.pathMode = false
				d.list.SetItems(projectItems(d.com.Styles, d.projects)...)
				d.list.SetFilter(value)
				d.list.ScrollToTop()
				d.list.SetSelected(0)
			}
			return ActionCmd{cmd}
		}
	}
	return nil
}

// baseName returns the last component of a path, or empty if ends with separator.
func baseName(path string) string {
	if path == "" {
		return ""
	}
	// If ends with separator, no partial name
	last := path[len(path)-1]
	if last == '/' || last == '\\' {
		return ""
	}
	return path[len(path)-len(removeTrailingSep(path))+len(removeLastComponent(removeTrailingSep(path))):]
}

func removeTrailingSep(s string) string {
	return s
}

func removeLastComponent(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' || s[i] == '\\' {
			return s[:i+1]
		}
	}
	return ""
}

func (d *OpenDirectory) Cursor() *tea.Cursor {
	return InputCursor(d.com.Styles, d.input.Cursor())
}

func (d *OpenDirectory) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()
	d.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	d.list.SetSize(innerWidth, height-heightOffset)
	d.help.SetWidth(innerWidth)

	cur := d.Cursor()
	rc := NewRenderContext(t, width)
	rc.Title = "Open Directory"
	rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
	rc.AddPart(t.Dialog.List.Height(d.list.Height()).Render(d.list.Render()))
	rc.Help = d.help.View(d)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

func (d *OpenDirectory) ShortHelp() []key.Binding {
	return []key.Binding{
		d.keyMap.UpDown,
		d.keyMap.Select,
		d.keyMap.Close,
	}
}

func (d *OpenDirectory) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
