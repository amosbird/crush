package list

import (
	"strings"
)

// List represents a list of items that can be lazily rendered. A list is
// always rendered like a chat conversation where items are stacked vertically
// from top to bottom.
type List struct {
	// Viewport size
	width, height int

	// Items in the list
	items []Item

	// Gap between items (0 or less means no gap)
	gap int

	// show list in reverse order
	reverse bool

	// Focus and selection state
	focused     bool
	selectedIdx int // The current selected index -1 means no selection

	// offsetIdx is the index of the first visible item in the viewport.
	offsetIdx int
	// offsetLine is the number of lines of the item at offsetIdx that are
	// scrolled out of view (above the viewport).
	// It must always be >= 0.
	offsetLine int

	// renderCallbacks is a list of callbacks to apply when rendering items.
	renderCallbacks []func(idx, selectedIdx int, item Item) Item

	// heightCache caches the rendered height per item index. A value of -1
	// means the entry is invalid and must be recomputed.
	heightCache []int
	// heightCacheWidth is the width the height cache was computed at.
	heightCacheWidth int
	// totalHeightCache caches the total content height (all item heights
	// plus gaps). A value of -1 means invalid.
	totalHeightCache int
}

// renderedItem holds the rendered content and height of an item.
type renderedItem struct {
	content string
	height  int
}

// NewList creates a new lazy-loaded list.
func NewList(items ...Item) *List {
	l := new(List)
	l.items = items
	l.selectedIdx = -1
	l.totalHeightCache = -1
	return l
}

// RenderCallback defines a function that can modify an item before it is
// rendered.
type RenderCallback func(idx, selectedIdx int, item Item) Item

// RegisterRenderCallback registers a callback to be called when rendering
// items. This can be used to modify items before they are rendered.
func (l *List) RegisterRenderCallback(cb RenderCallback) {
	l.renderCallbacks = append(l.renderCallbacks, cb)
}

// SetSize sets the size of the list viewport.
func (l *List) SetSize(width, height int) {
	if l.width != width {
		l.invalidateHeightCache()
	}
	l.width = width
	l.height = height
}

// SetGap sets the gap between items.
func (l *List) SetGap(gap int) {
	if l.gap != gap {
		l.totalHeightCache = -1
	}
	l.gap = gap
}

// Gap returns the gap between items.
func (l *List) Gap() int {
	return l.gap
}

// invalidateHeightCache resets all cached heights.
func (l *List) invalidateHeightCache() {
	l.heightCache = nil
	l.heightCacheWidth = 0
	l.totalHeightCache = -1
}

// InvalidateItemHeight invalidates the cached height for a single item and
// the total height cache. Call this when an item's content has changed.
func (l *List) InvalidateItemHeight(idx int) {
	if idx >= 0 && idx < len(l.heightCache) {
		l.heightCache[idx] = -1
	}
	l.totalHeightCache = -1
}

// ensureHeightCache allocates the height cache slice if needed and
// validates it matches the current width and item count.
func (l *List) ensureHeightCache() {
	if l.heightCacheWidth != l.width || len(l.heightCache) != len(l.items) {
		l.heightCache = make([]int, len(l.items))
		for i := range l.heightCache {
			l.heightCache[i] = -1
		}
		l.heightCacheWidth = l.width
		l.totalHeightCache = -1
	}
}

// getItemHeight returns the cached height for the item at idx, computing
// and caching it if necessary.
func (l *List) getItemHeight(idx int) int {
	if idx < 0 || idx >= len(l.items) {
		return 0
	}

	l.ensureHeightCache()

	if l.heightCache[idx] >= 0 {
		return l.heightCache[idx]
	}

	item := l.items[idx]
	for _, cb := range l.renderCallbacks {
		if it := cb(idx, l.selectedIdx, item); it != nil {
			item = it
		}
	}
	rendered := item.Render(l.width)
	rendered = strings.TrimRight(rendered, "\n")
	var h int
	if rendered != "" {
		h = strings.Count(rendered, "\n") + 1
	}
	l.heightCache[idx] = h
	return h
}

// AtBottom returns whether the list is showing the last item at the bottom.
func (l *List) AtBottom() bool {
	if len(l.items) == 0 {
		return true
	}

	// Calculate the height from offsetIdx to the end.
	var totalHeight int
	for idx := l.offsetIdx; idx < len(l.items); idx++ {
		if totalHeight > l.height {
			return false
		}
		itemHeight := l.getItemHeight(idx)
		if l.gap > 0 && idx > l.offsetIdx {
			itemHeight += l.gap
		}
		totalHeight += itemHeight
	}

	return totalHeight-l.offsetLine <= l.height
}

// NearBottom returns whether the list is within the given number of lines
// from the bottom. This is useful for re-engaging follow mode when the user
// scrolls close to the bottom during streaming.
func (l *List) NearBottom(threshold int) bool {
	if len(l.items) == 0 {
		return true
	}

	var totalHeight int
	for idx := l.offsetIdx; idx < len(l.items); idx++ {
		if totalHeight > l.height+threshold {
			return false
		}
		itemHeight := l.getItemHeight(idx)
		if l.gap > 0 && idx > l.offsetIdx {
			itemHeight += l.gap
		}
		totalHeight += itemHeight
	}

	return totalHeight-l.offsetLine <= l.height+threshold
}

// ScrollInfo returns the total content height in lines and the current scroll
// offset from the top. These values are suitable for rendering a scrollbar.
func (l *List) ScrollInfo() (totalHeight, offset int) {
	if len(l.items) == 0 {
		return 0, 0
	}

	totalHeight = l.computeTotalHeight()

	// Compute offset: sum heights of all items before offsetIdx, plus
	// offsetLine within the current item, plus gaps.
	for idx := range l.offsetIdx {
		offset += l.getItemHeight(idx)
		if l.gap > 0 {
			offset += l.gap
		}
	}
	offset += l.offsetLine

	return totalHeight, offset
}

// computeTotalHeight returns the total content height, using the cache when
// available.
func (l *List) computeTotalHeight() int {
	if l.totalHeightCache >= 0 {
		return l.totalHeightCache
	}

	var total int
	var prevHadHeight bool
	for idx := range l.items {
		h := l.getItemHeight(idx)
		if h == 0 {
			continue
		}
		if l.gap > 0 && prevHadHeight {
			total += l.gap
		}
		total += h
		prevHadHeight = true
	}
	l.totalHeightCache = total
	return total
}

// SetReverse shows the list in reverse order.
func (l *List) SetReverse(reverse bool) {
	l.reverse = reverse
}

// Width returns the width of the list viewport.
func (l *List) Width() int {
	return l.width
}

// Height returns the height of the list viewport.
func (l *List) Height() int {
	return l.height
}

// Len returns the number of items in the list.
func (l *List) Len() int {
	return len(l.items)
}

// lastOffsetItem returns the index and line offsets of the last item that can
// be partially visible in the viewport.
func (l *List) lastOffsetItem() (int, int, int) {
	var totalHeight int
	var idx int
	for idx = len(l.items) - 1; idx >= 0; idx-- {
		itemHeight := l.getItemHeight(idx)
		if l.gap > 0 && idx < len(l.items)-1 {
			itemHeight += l.gap
		}
		totalHeight += itemHeight
		if totalHeight > l.height {
			break
		}
	}

	// Calculate line offset within the item.
	lineOffset := max(totalHeight-l.height, 0)
	idx = max(idx, 0)

	return idx, lineOffset, totalHeight
}

// getItem renders (if needed) and returns the item at the given index.
func (l *List) getItem(idx int) renderedItem {
	if idx < 0 || idx >= len(l.items) {
		return renderedItem{}
	}

	item := l.items[idx]
	if len(l.renderCallbacks) > 0 {
		for _, cb := range l.renderCallbacks {
			if it := cb(idx, l.selectedIdx, item); it != nil {
				item = it
			}
		}
	}

	rendered := item.Render(l.width)
	rendered = strings.TrimRight(rendered, "\n")
	var height int
	if rendered == "" {
		height = 0
	} else {
		height = strings.Count(rendered, "\n") + 1
	}
	ri := renderedItem{
		content: rendered,
		height:  height,
	}

	// Update height cache as a side effect.
	l.ensureHeightCache()
	if idx < len(l.heightCache) {
		l.heightCache[idx] = height
	}

	return ri
}

// ScrollToIndex scrolls the list to the given item index.
func (l *List) ScrollToIndex(index int) {
	if index < 0 {
		index = 0
	}
	if index >= len(l.items) {
		index = len(l.items) - 1
	}
	l.offsetIdx = index
	l.offsetLine = 0
}

// ScrollBy scrolls the list by the given number of lines.
func (l *List) ScrollBy(lines int) {
	if len(l.items) == 0 || lines == 0 {
		return
	}

	if l.reverse {
		lines = -lines
	}

	if lines > 0 {
		if l.AtBottom() {
			return
		}

		// Scroll down.
		l.offsetLine += lines
		currentHeight := l.getItemHeight(l.offsetIdx)
		for l.offsetLine >= currentHeight {
			l.offsetLine -= currentHeight
			if l.gap > 0 {
				l.offsetLine = max(0, l.offsetLine-l.gap)
			}

			l.offsetIdx++
			if l.offsetIdx > len(l.items)-1 {
				l.ScrollToBottom()
				return
			}
			currentHeight = l.getItemHeight(l.offsetIdx)
		}

		lastOffsetIdx, lastOffsetLine, _ := l.lastOffsetItem()
		if l.offsetIdx > lastOffsetIdx || (l.offsetIdx == lastOffsetIdx && l.offsetLine > lastOffsetLine) {
			l.offsetIdx = lastOffsetIdx
			l.offsetLine = lastOffsetLine
		}
	} else if lines < 0 {
		// Scroll up.
		l.offsetLine += lines // lines is negative
		for l.offsetLine < 0 {
			l.offsetIdx--
			if l.offsetIdx < 0 {
				l.ScrollToTop()
				break
			}
			prevHeight := l.getItemHeight(l.offsetIdx)
			totalHeight := prevHeight
			if l.gap > 0 {
				totalHeight += l.gap
			}
			l.offsetLine += totalHeight
		}
	}
}

// VisibleItemIndices finds the range of items that are visible in the viewport.
// This is used for checking if selected item is in view.
func (l *List) VisibleItemIndices() (startIdx, endIdx int) {
	if len(l.items) == 0 {
		return 0, 0
	}

	startIdx = l.offsetIdx
	currentIdx := startIdx
	visibleHeight := -l.offsetLine

	for currentIdx < len(l.items) {
		visibleHeight += l.getItemHeight(currentIdx)
		if l.gap > 0 {
			visibleHeight += l.gap
		}

		if visibleHeight >= l.height {
			break
		}
		currentIdx++
	}

	endIdx = currentIdx
	if endIdx >= len(l.items) {
		endIdx = len(l.items) - 1
	}

	return startIdx, endIdx
}

// Render renders the list and returns the visible lines.
func (l *List) Render() string {
	if len(l.items) == 0 {
		return ""
	}

	var lines []string
	currentIdx := l.offsetIdx
	currentOffset := l.offsetLine

	linesNeeded := l.height

	for linesNeeded > 0 && currentIdx < len(l.items) {
		item := l.getItem(currentIdx)
		if item.height == 0 {
			currentIdx++
			currentOffset = 0
			continue
		}
		itemLines := strings.Split(item.content, "\n")
		itemHeight := len(itemLines)

		if currentOffset >= 0 && currentOffset < itemHeight {
			lines = append(lines, itemLines[currentOffset:]...)

			if l.gap > 0 {
				for i := 0; i < l.gap; i++ {
					lines = append(lines, "")
				}
			}
		} else {
			gapOffset := currentOffset - itemHeight
			gapRemaining := l.gap - gapOffset
			if gapRemaining > 0 {
				for range gapRemaining {
					lines = append(lines, "")
				}
			}
		}

		linesNeeded = l.height - len(lines)
		currentIdx++
		currentOffset = 0
	}

	l.height = max(l.height, 0)

	if len(lines) > l.height {
		lines = lines[:l.height]
	}

	if l.reverse {
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
	}

	return strings.Join(lines, "\n")
}

// PrependItems prepends items to the list.
func (l *List) PrependItems(items ...Item) {
	l.items = append(items, l.items...)

	// Keep view position relative to the content that was visible
	l.offsetIdx += len(items)

	// Update selection index if valid
	if l.selectedIdx != -1 {
		l.selectedIdx += len(items)
	}

	l.invalidateHeightCache()
}

// SetItems sets the items in the list.
func (l *List) SetItems(items ...Item) {
	l.setItems(true, items...)
}

// setItems sets the items in the list. If evict is true, it clears the
// rendered item cache.
func (l *List) setItems(evict bool, items ...Item) {
	l.items = items
	l.selectedIdx = min(l.selectedIdx, len(l.items)-1)
	l.offsetIdx = min(l.offsetIdx, len(l.items)-1)
	l.offsetLine = 0
	if evict {
		l.invalidateHeightCache()
	}
}

// AppendItems appends items to the list.
func (l *List) AppendItems(items ...Item) {
	l.items = append(l.items, items...)
	l.totalHeightCache = -1
}

// RemoveItem removes the item at the given index from the list.
func (l *List) RemoveItem(idx int) {
	if idx < 0 || idx >= len(l.items) {
		return
	}

	// Remove the item
	l.items = append(l.items[:idx], l.items[idx+1:]...)

	// Adjust selection if needed
	if l.selectedIdx == idx {
		l.selectedIdx = -1
	} else if l.selectedIdx > idx {
		l.selectedIdx--
	}

	// Adjust offset if needed
	if l.offsetIdx > idx {
		l.offsetIdx--
	} else if l.offsetIdx == idx && l.offsetIdx >= len(l.items) {
		l.offsetIdx = max(0, len(l.items)-1)
		l.offsetLine = 0
	}

	l.invalidateHeightCache()
}

// Focused returns whether the list is focused.
func (l *List) Focused() bool {
	return l.focused
}

// Focus sets the focus state of the list.
func (l *List) Focus() {
	l.focused = true
}

// Blur removes the focus state from the list.
func (l *List) Blur() {
	l.focused = false
}

// AtTop returns whether the list is scrolled to the very top.
func (l *List) AtTop() bool {
	return l.offsetIdx == 0 && l.offsetLine == 0
}

// ScrollToTop scrolls the list to the top.
func (l *List) ScrollToTop() {
	l.offsetIdx = 0
	l.offsetLine = 0
}

// ScrollToBottom scrolls the list to the bottom.
func (l *List) ScrollToBottom() {
	if len(l.items) == 0 {
		return
	}

	lastOffsetIdx, lastOffsetLine, _ := l.lastOffsetItem()
	l.offsetIdx = lastOffsetIdx
	l.offsetLine = lastOffsetLine
}

// ScrollToSelected scrolls the list to the selected item.
func (l *List) ScrollToSelected() {
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return
	}

	startIdx, endIdx := l.VisibleItemIndices()
	if l.selectedIdx < startIdx {
		l.offsetIdx = l.selectedIdx
		l.offsetLine = 0
	} else if l.selectedIdx > endIdx {
		var totalHeight int
		for i := l.selectedIdx; i >= 0; i-- {
			totalHeight += l.getItemHeight(i)
			if l.gap > 0 && i < l.selectedIdx {
				totalHeight += l.gap
			}
			if totalHeight >= l.height {
				l.offsetIdx = i
				l.offsetLine = totalHeight - l.height
				break
			}
		}
		if totalHeight < l.height {
			l.ScrollToTop()
		}
	}
}

// SelectedItemInView returns whether the selected item is currently in view.
func (l *List) SelectedItemInView() bool {
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return false
	}
	startIdx, endIdx := l.VisibleItemIndices()
	return l.selectedIdx >= startIdx && l.selectedIdx <= endIdx
}

// SetSelected sets the selected item index in the list.
// It returns -1 if the index is out of bounds.
func (l *List) SetSelected(index int) {
	if index < 0 || index >= len(l.items) {
		l.selectedIdx = -1
	} else {
		l.selectedIdx = index
	}
}

// Selected returns the index of the currently selected item. It returns -1 if
// no item is selected.
func (l *List) Selected() int {
	return l.selectedIdx
}

// IsSelectedFirst returns whether the first item is selected.
func (l *List) IsSelectedFirst() bool {
	return l.selectedIdx == 0
}

// IsSelectedLast returns whether the last item is selected.
func (l *List) IsSelectedLast() bool {
	return l.selectedIdx == len(l.items)-1
}

// SelectPrev selects the visually previous item (moves toward visual top).
// It returns whether the selection changed.
func (l *List) SelectPrev() bool {
	if l.reverse {
		if l.selectedIdx < len(l.items)-1 {
			l.selectedIdx++
			return true
		}
	} else {
		if l.selectedIdx > 0 {
			l.selectedIdx--
			return true
		}
	}
	return false
}

// SelectNext selects the next item in the list.
// It returns whether the selection changed.
func (l *List) SelectNext() bool {
	if l.reverse {
		if l.selectedIdx > 0 {
			l.selectedIdx--
			return true
		}
	} else {
		if l.selectedIdx < len(l.items)-1 {
			l.selectedIdx++
			return true
		}
	}
	return false
}

// SelectFirst selects the first item in the list.
// It returns whether the selection changed.
func (l *List) SelectFirst() bool {
	if len(l.items) == 0 {
		return false
	}
	l.selectedIdx = 0
	return true
}

// SelectLast selects the last item in the list (highest index).
// It returns whether the selection changed.
func (l *List) SelectLast() bool {
	if len(l.items) == 0 {
		return false
	}
	l.selectedIdx = len(l.items) - 1
	return true
}

// WrapToStart wraps selection to the visual start (for circular navigation).
// In normal mode, this is index 0. In reverse mode, this is the highest index.
func (l *List) WrapToStart() bool {
	if len(l.items) == 0 {
		return false
	}
	if l.reverse {
		l.selectedIdx = len(l.items) - 1
	} else {
		l.selectedIdx = 0
	}
	return true
}

// WrapToEnd wraps selection to the visual end (for circular navigation).
// In normal mode, this is the highest index. In reverse mode, this is index 0.
func (l *List) WrapToEnd() bool {
	if len(l.items) == 0 {
		return false
	}
	if l.reverse {
		l.selectedIdx = 0
	} else {
		l.selectedIdx = len(l.items) - 1
	}
	return true
}

// SelectedItem returns the currently selected item. It may be nil if no item
// is selected.
func (l *List) SelectedItem() Item {
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return nil
	}
	return l.items[l.selectedIdx]
}

// SelectFirstInView selects the first item currently in view.
func (l *List) SelectFirstInView() {
	startIdx, _ := l.VisibleItemIndices()
	l.selectedIdx = startIdx
}

// SelectLastInView selects the last item currently in view.
func (l *List) SelectLastInView() {
	_, endIdx := l.VisibleItemIndices()
	l.selectedIdx = endIdx
}

// ItemAt returns the item at the given index.
func (l *List) ItemAt(index int) Item {
	if index < 0 || index >= len(l.items) {
		return nil
	}
	return l.items[index]
}

// ItemIndexAtPosition returns the item at the given viewport-relative y
// coordinate. Returns the item index and the y offset within that item. It
// returns -1, -1 if no item is found.
func (l *List) ItemIndexAtPosition(x, y int) (itemIdx int, itemY int) {
	return l.findItemAtY(x, y)
}

// findItemAtY finds the item at the given viewport y coordinate.
// Returns the item index and the y offset within that item. It returns -1, -1
// if no item is found.
func (l *List) findItemAtY(_, y int) (itemIdx int, itemY int) {
	if y < 0 || y >= l.height {
		return -1, -1
	}

	currentIdx := l.offsetIdx
	currentLine := -l.offsetLine

	for currentIdx < len(l.items) && currentLine < l.height {
		h := l.getItemHeight(currentIdx)
		itemEndLine := currentLine + h

		if y >= currentLine && y < itemEndLine {
			itemY = y - currentLine
			return currentIdx, itemY
		}

		currentLine = itemEndLine
		if l.gap > 0 {
			currentLine += l.gap
		}
		currentIdx++
	}

	return -1, -1
}
