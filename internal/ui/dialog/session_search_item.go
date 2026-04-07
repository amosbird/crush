package dialog

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/search"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

// SearchResultItem wraps a [search.SearchResult] to implement the [ListItem]
// interface for display in the session search dialog.
type SearchResultItem struct {
	search.SearchResult
	t       *styles.Styles
	m       fuzzy.Match
	cache   map[int]string
	focused bool
}

var _ ListItem = &SearchResultItem{}

// Filter returns the filterable value of the search result.
func (s *SearchResultItem) Filter() string {
	return s.Title + " " + s.ProjectPath
}

// ID returns the unique identifier of the search result session.
func (s *SearchResultItem) ID() string {
	return s.SessionID
}

// SetMatch sets the fuzzy match for the search result item.
func (s *SearchResultItem) SetMatch(m fuzzy.Match) {
	s.cache = nil
	s.m = m
}

// SetFocused sets the focus state of the search result item.
func (s *SearchResultItem) SetFocused(focused bool) {
	if s.focused != focused {
		s.cache = nil
	}
	s.focused = focused
}

// Render returns the string representation of the search result item.
// Line 1: title + info (via renderItem), Line 2: project path (dimmed).
func (s *SearchResultItem) Render(width int) string {
	title := s.Title
	if s.Active {
		title = "● " + title
	}

	info := s.Date
	if s.MessageCount != "" {
		info = s.MessageCount + " · " + info
	}

	st := ListItemStyles{
		ItemBlurred:     s.t.Dialog.NormalItem,
		ItemFocused:     s.t.Dialog.SelectedItem,
		InfoTextBlurred: s.t.Subtle,
		InfoTextFocused: s.t.Base.Foreground(s.t.Dialog.SelectedItem.GetForeground()),
	}

	line1 := renderItem(st, title, info, s.focused, width, s.cache, &s.m)

	if s.ProjectPath == "" {
		return line1
	}

	// Line 2: project path, truncated, with same padding as itemStyle
	itemStyle := st.ItemBlurred
	if s.focused {
		itemStyle = st.ItemFocused
	}
	innerWidth := max(0, width-itemStyle.GetHorizontalFrameSize())
	path := ansi.Truncate(s.ProjectPath, innerWidth, "…")
	// Use the item style but with subtle foreground for the path line
	pathStyle := lipgloss.NewStyle().
		Foreground(s.t.Subtle.GetForeground()).
		PaddingLeft(itemStyle.GetPaddingLeft()).
		PaddingRight(itemStyle.GetPaddingRight())
	line2 := pathStyle.Render(path)

	return line1 + "\n" + line2
}

// searchResultItems converts a slice of [search.SearchResult] into a slice
// of [list.FilterableItem].
func searchResultItems(t *styles.Styles, results ...search.SearchResult) []list.FilterableItem {
	items := make([]list.FilterableItem, len(results))
	for i, r := range results {
		items[i] = &SearchResultItem{SearchResult: r, t: t}
	}
	return items
}
