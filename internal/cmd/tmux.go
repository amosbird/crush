package cmd

import (
	"crypto/sha1"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/spf13/cobra"
)

//go:embed tmux.conf
var embeddedTmuxConf string

// tmuxSocketPath returns the path to the dedicated crush tmux socket.
func tmuxSocketPath(cwd string) string {
	sum := sha1.Sum([]byte(filepath.Clean(cwd)))
	name := fmt.Sprintf("tmux-crush-%x", sum[:6])
	return filepath.Join(os.TempDir(), name)
}

// tmuxConfPath returns the path where the embedded tmux config is written.
func tmuxConfPath() string {
	return filepath.Join(filepath.Dir(config.GlobalConfig()), "tmux.conf")
}

// ensureTmuxConf writes the embedded tmux config to disk only if the
// file does not already exist. This allows users to customize the file
// without it being overwritten on upgrade.
func ensureTmuxConf() (string, error) {
	path := tmuxConfPath()
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(embeddedTmuxConf), 0o644)
}

// findMux returns the path to tmux or psmux if available.
func findMux() string {
	for _, name := range []string{"tmux", "psmux"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// shouldAutoTmux returns true if crush should exec into a tmux session.
// Conditions: not already in tmux, tmux/psmux available, not disabled.
func shouldAutoTmux() bool {
	if os.Getenv("TMUX") != "" {
		return false
	}
	if os.Getenv("CRUSH_NO_TMUX") != "" {
		return false
	}
	return findMux() != ""
}

func tmuxWindowName(cwd string) string {
	cleaned := filepath.Clean(cwd)
	name := filepath.Base(cleaned)
	if name == "." || name == string(filepath.Separator) || name == "" {
		trimmed := strings.TrimRight(cleaned, string(filepath.Separator))
		if vol := filepath.VolumeName(trimmed); vol != "" && strings.EqualFold(trimmed, vol+string(filepath.Separator)) {
			return vol
		}
		return "crush"
	}
	return name
}

// execIntoTmux replaces the current process with a tmux session running crush.
// On Unix this uses syscall.Exec; on Windows it uses os/exec and waits.
//
// This mirrors the behavior of startcrush-tmux:
//
//	TMUX=/tmp/tmux-crush-<hash> tmux -f <config> -u new-session -A -s crush -n <window-name> -c <cwd> <crush> [args...]
func execIntoTmux(cwd string, crushArgs []string) error {
	muxBin := findMux()
	if muxBin == "" {
		return nil
	}

	confPath, err := ensureTmuxConf()
	if err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	// Set a dedicated socket path per working directory so crush sessions
	// from different projects do not reuse the same tmux server.
	socket := tmuxSocketPath(cwd)
	windowName := tmuxWindowName(cwd)

	// Build tmux args:
	// tmux -S <socket> -f <config> -u new-session -A -s crush -n <window-name> -c <cwd> <exe> [args...]
	args := []string{
		"-S", socket,
		"-f", confPath,
		"-u",
		"new-session", "-A",
		"-s", "crush",
		"-n", windowName,
		"-c", cwd,
	}

	// Append the crush executable and its original args as the tmux
	// window command.
	windowCmd := append([]string{exe}, crushArgs...)
	args = append(args, windowCmd...)

	return muxExec(muxBin, args)
}

// buildInnerTmuxArgs builds the argument list for the crush process that
// will run inside tmux. It preserves the user's original flags.
func buildInnerTmuxArgs(cmd *cobra.Command) []string {
	var args []string

	yolo, _ := cmd.Flags().GetBool("yolo")
	if yolo {
		args = append(args, "--yolo")
	}

	cwd, _ := cmd.Flags().GetString("cwd")
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}

	dataDir, _ := cmd.Flags().GetString("data-dir")
	if dataDir != "" {
		args = append(args, "--data-dir", dataDir)
	}

	debug, _ := cmd.Flags().GetBool("debug")
	if debug {
		args = append(args, "--debug")
	}

	sessionID, _ := cmd.Flags().GetString("session")
	continueLast, _ := cmd.Flags().GetBool("continue")

	switch {
	case sessionID != "":
		args = append(args, "--session", sessionID)
	case continueLast:
		args = append(args, "--continue")
	}

	return args
}
