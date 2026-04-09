// Package mux provides a thin abstraction over terminal multiplexers
// (tmux, psmux) so that crush features like session-fork-to-new-window
// work identically regardless of which multiplexer is running.
package mux

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Mux represents a terminal multiplexer that can manage windows and
// pane-level options.  A nil *Mux means no multiplexer is available.
type Mux struct {
	bin string // absolute path or bare name of the mux binary
}

// Detect returns a *Mux for the current environment, or nil if crush
// is not running inside a supported multiplexer.
func Detect() *Mux {
	if os.Getenv("TMUX") == "" {
		return nil
	}
	// Both tmux and psmux set $TMUX and accept the same command syntax.
	for _, name := range []string{"tmux", "psmux"} {
		if p, err := exec.LookPath(name); err == nil {
			return &Mux{bin: p}
		}
	}
	return nil
}

// Available reports whether a multiplexer was detected.
func (m *Mux) Available() bool {
	return m != nil
}

// NewWindow opens a new multiplexer window running the given command
// in the specified working directory.
func (m *Mux) NewWindow(cwd string, args ...string) error {
	if m == nil {
		return nil
	}
	cmdArgs := []string{"new-window", "-c", cwd}
	if name := filepath.Base(filepath.Clean(cwd)); name != "." && name != string(filepath.Separator) && name != "" {
		cmdArgs = append(cmdArgs, "-n", name)
	}
	cmdArgs = append(cmdArgs, args...)
	return m.run(cmdArgs...)
}

// SetPaneOption sets a pane-level user option (e.g. @crush_session).
func (m *Mux) SetPaneOption(key, value string) {
	if m == nil {
		return
	}
	go func() {
		_ = m.run("set-option", "-p", key, value)
	}()
}

// GetPaneOption reads a pane-level user option.  Returns "" on any error.
func (m *Mux) GetPaneOption(key string) string {
	if m == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.bin, "show-option", "-pqv", key).Output()
	if err != nil {
		return ""
	}
	// trim trailing newline
	s := string(out)
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	return s
}

// PaneCwd returns the current working directory of the active pane.
func (m *Mux) PaneCwd() string {
	if m == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.bin, "display-message", "-p", "#{pane_current_path}").Output()
	if err != nil {
		return ""
	}
	s := string(out)
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	return s
}

// ActiveCrushSessions returns the @crush_session values from all panes.
// This is used to mark which sessions are currently open.
func (m *Mux) ActiveCrushSessions() []string {
	if m == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.bin, "list-panes", "-a", "-F", "#{@crush_session}").Output()
	if err != nil {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids
}

// SelectPaneBySession switches to the mux pane that has the given
// @crush_session value. It first selects the window containing the pane,
// then selects the pane itself. Returns true if such a pane was found.
func (m *Mux) SelectPaneBySession(sessionID string) bool {
	if m == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.bin, "list-panes", "-a", "-F", "#{@crush_session} #{window_id} #{pane_id}").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) == 3 && parts[0] == sessionID {
			_ = m.run("select-window", "-t", parts[1])
			_ = m.run("select-pane", "-t", parts[2])
			return true
		}
	}
	return false
}

func (m *Mux) run(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, m.bin, args...).Run()
}
