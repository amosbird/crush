package dialog

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/dustin/go-humanize"
	"github.com/sahilm/fuzzy"
)

// DirectoryItem represents a directory entry in the Open Directory dialog.
type DirectoryItem struct {
	Path     string
	Info     string // e.g. "3 hours ago" or "directory"
	t        *styles.Styles
	m        fuzzy.Match
	cache    map[int]string
	focused  bool
}

var _ ListItem = &DirectoryItem{}

func (d *DirectoryItem) Filter() string { return d.Path }
func (d *DirectoryItem) ID() string     { return d.Path }

func (d *DirectoryItem) SetMatch(m fuzzy.Match) {
	d.cache = nil
	d.m = m
}

func (d *DirectoryItem) SetFocused(focused bool) {
	if d.focused != focused {
		d.cache = nil
	}
	d.focused = focused
}

func (d *DirectoryItem) Render(width int) string {
	st := ListItemStyles{
		ItemBlurred:     d.t.Dialog.NormalItem,
		ItemFocused:     d.t.Dialog.SelectedItem,
		InfoTextBlurred: d.t.Subtle,
		InfoTextFocused: d.t.Base,
	}
	return renderItem(st, d.Path, d.Info, d.focused, width, d.cache, &d.m)
}

// projectItems converts known projects to list items.
func projectItems(t *styles.Styles, projs []projects.Project) []list.FilterableItem {
	items := make([]list.FilterableItem, len(projs))
	for i, p := range projs {
		home, _ := os.UserHomeDir()
		path := p.Path
		if home != "" && strings.HasPrefix(path, home) {
			path = "~" + path[len(home):]
		}
		items[i] = &DirectoryItem{
			Path: path,
			Info: humanize.Time(p.LastAccessed),
			t:    t,
		}
	}
	return items
}

// directoryItems lists subdirectories of a given path for path-completion mode.
func directoryItems(t *styles.Styles, dir string) []list.FilterableItem {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var dirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e)
		}
	}

	// Sort alphabetically
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].Name() < dirs[j].Name()
	})

	items := make([]list.FilterableItem, 0, len(dirs))
	for _, d := range dirs {
		full := filepath.Join(dir, d.Name())
		items = append(items, &DirectoryItem{
			Path: full,
			t:    t,
		})
	}
	return items
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// isPathInput returns true if the input looks like a filesystem path.
func isPathInput(input string) bool {
	if input == "" {
		return false
	}
	if strings.HasPrefix(input, "~") {
		return true
	}
	return filepath.IsAbs(input)
}

// resolveDir resolves the input to a directory path for listing.
// Returns the parent directory and whether it exists.
func resolveDir(input string) (string, bool) {
	expanded := expandHome(input)
	info, err := os.Stat(expanded)
	if err == nil && info.IsDir() {
		return expanded, true
	}
	// Try parent directory
	parent := filepath.Dir(expanded)
	info, err = os.Stat(parent)
	if err == nil && info.IsDir() {
		return parent, true
	}
	return "", false
}

