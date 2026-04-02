package tools

import (
	"runtime"
	"strings"
)

// safeCommandsExact contains single-word commands that can be matched with
// an O(1) map lookup instead of a linear scan.
var safeCommandsExact = map[string]struct{}{
	// Shell builtins and core utils (read-only / harmless).
	"[":         {},
	"basename":  {},
	"cal":       {},
	"cat":       {},
	"cd":        {},
	"column":    {},
	"comm":      {},
	"cmp":       {},
	"cut":       {},
	"date":      {},
	"df":        {},
	"diff":      {},
	"dirname":   {},
	"du":        {},
	"echo":      {},
	"env":       {},
	"expand":    {},
	"expr":      {},
	"false":     {},
	"file":      {},
	"fmt":       {},
	"fold":      {},
	"free":      {},
	"getent":    {},
	"groups":    {},
	"head":      {},
	"hostname":  {},
	"id":        {},
	"join":      {},
	"less":      {},
	"locale":    {},
	"ls":        {},
	"man":       {},
	"md5sum":    {},
	"more":      {},
	"nl":        {},
	"nproc":     {},
	"od":        {},
	"paste":     {},
	"printenv":  {},
	"printf":    {},
	"ps":        {},
	"pwd":       {},
	"readlink":  {},
	"realpath":  {},
	"rev":       {},
	"seq":       {},
	"set":       {},
	"sha1sum":   {},
	"sha256sum": {},
	"sort":      {},
	"stat":      {},
	"strings":   {},
	"tac":       {},
	"tail":      {},
	"test":      {},
	"time":      {},
	"top":       {},
	"tr":        {},
	"true":      {},
	"type":      {},
	"uname":     {},
	"unexpand":  {},
	"uniq":      {},
	"unset":     {},
	"uptime":    {},
	"wc":        {},
	"whatis":    {},
	"whereis":   {},
	"which":     {},
	"whoami":    {},

	// Search tools (read-only).
	"fd":   {},
	"grep": {},
	"rg":   {},

	// Data processing (read-only).
	"bat":  {},
	"jq":   {},
	"tree": {},
	"yq":   {},
}

// safeCommandsPrefix contains multi-word command prefixes that require a
// linear prefix scan. This list is short (~25 entries), so the scan is fast.
var safeCommandsPrefix = []string{
	// Git (read-only operations).
	"git blame",
	"git branch",
	"git config --get",
	"git config --list",
	"git describe",
	"git diff",
	"git grep",
	"git log",
	"git ls-files",
	"git ls-remote",
	"git remote",
	"git rev-list",
	"git rev-parse",
	"git shortlog",
	"git show",
	"git stash list",
	"git status",
	"git tag",

	// Go (read-only / low-risk dev workflow).
	"go doc",
	"go env",
	"go list",
	"go version",
	"go vet",
}

func init() {
	if runtime.GOOS == "windows" {
		for _, cmd := range []string{
			"ipconfig",
			"nslookup",
			"ping",
			"systeminfo",
			"tasklist",
			"where",
		} {
			safeCommandsExact[cmd] = struct{}{}
		}
	}
}

// shellMetaChars contains characters that indicate non-trivial shell syntax
// that our simple splitter cannot safely parse. If any of these appear in
// the command string we bail out and treat the whole command as unsafe.
const shellMetaChars = "`$><\n"

// isSafeCommand checks whether a shell command (potentially compound) consists
// entirely of safe, read-only commands. It splits on &&, ||, ;, and | to
// handle compound commands like "cd /tmp && git status && git diff --stat HEAD".
//
// Commands containing shell metacharacters that could hide unsafe operations
// (subshells, redirections, background execution, etc.) are always treated
// as unsafe.
func isSafeCommand(cmd string) bool {
	if strings.ContainsAny(cmd, shellMetaChars) {
		return false
	}
	if containsBackgroundOp(cmd) {
		return false
	}
	for _, part := range splitShellCommands(cmd) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !matchesSafeCommand(part) {
			return false
		}
	}
	return true
}

// containsBackgroundOp checks if the command contains a bare & (background
// operator) that is not part of && (logical AND).
func containsBackgroundOp(cmd string) bool {
	for i := range len(cmd) {
		if cmd[i] != '&' {
			continue
		}
		// Check if this & is part of &&.
		if i+1 < len(cmd) && cmd[i+1] == '&' {
			continue
		}
		if i > 0 && cmd[i-1] == '&' {
			continue
		}
		return true
	}
	return false
}

// splitShellCommands splits a command string by shell operators (&&, ||, ;, |)
// into individual commands. It handles && and || before single & and |.
func splitShellCommands(cmd string) []string {
	var parts []string
	var current strings.Builder
	i := 0
	for i < len(cmd) {
		switch {
		case i+1 < len(cmd) && cmd[i:i+2] == "&&":
			parts = append(parts, current.String())
			current.Reset()
			i += 2
		case i+1 < len(cmd) && cmd[i:i+2] == "||":
			parts = append(parts, current.String())
			current.Reset()
			i += 2
		case cmd[i] == ';':
			parts = append(parts, current.String())
			current.Reset()
			i++
		case cmd[i] == '|':
			parts = append(parts, current.String())
			current.Reset()
			i++
		default:
			current.WriteByte(cmd[i])
			i++
		}
	}
	parts = append(parts, current.String())
	return parts
}

// matchesSafeCommand checks a single (non-compound) command against the safe
// commands list. The command is lowercased before matching. Only an exact
// match or a match followed by a space (i.e. arguments) is accepted.
//
// Single-word commands are checked via O(1) map lookup. Multi-word prefixes
// (e.g. "git diff", "go vet") fall back to a short linear scan.
func matchesSafeCommand(cmd string) bool {
	cmdLower := strings.ToLower(strings.TrimSpace(cmd))
	if cmdLower == "" {
		return true
	}

	// Extract the first word for exact-match lookup.
	firstWord, _, _ := strings.Cut(cmdLower, " ")

	// O(1) check: is the first word itself a safe single-word command?
	if _, ok := safeCommandsExact[firstWord]; ok {
		return true
	}

	// Linear scan over the short multi-word prefix list.
	for _, safe := range safeCommandsPrefix {
		if strings.HasPrefix(cmdLower, safe) {
			if len(cmdLower) == len(safe) || cmdLower[len(safe)] == ' ' {
				return true
			}
		}
	}
	return false
}
