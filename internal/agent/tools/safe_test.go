package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSafeCommand_SingleCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		{"simple git status", "git status", true},
		{"git diff with args", "git diff --stat HEAD", true},
		{"git log oneline", "git log --oneline -10", true},
		{"ls with flags", "ls -la", true},
		{"cat a file", "cat foo.go", true},
		{"head", "head -20 main.go", true},
		{"tail", "tail -f output.log", true},
		{"grep pattern", "grep -rn foo .", true},
		{"rg search", "rg --type go isSafe", true},
		{"go vet", "go vet ./...", true},
		{"jq", "jq '.name' package.json", true},
		{"tree", "tree -L 2", true},
		{"wc", "wc -l foo.go", true},
		{"cd", "cd /tmp", true},
		{"echo", "echo hello", true},
		{"pwd", "pwd", true},
		{"stat", "stat file.go", true},
		{"case insensitive", "Git Status", true},

		// Removed from safe list.
		{"find is unsafe", "find . -name '*.go'", false},
		{"awk is unsafe", "awk '{print $1}'", false},
		{"sed is unsafe", "sed -n '5,10p' file.go", false},
		{"xargs is unsafe", "xargs echo", false},
		{"python is unsafe", "python -c 'print(1)'", false},
		{"python3 is unsafe", "python3 script.py", false},
		{"node is unsafe", "node -e 'console.log(1)'", false},
		{"npx is unsafe", "npx something", false},
		{"make is unsafe", "make test", false},
		{"go test is unsafe", "go test ./...", false},
		{"go build is unsafe", "go build .", false},
		{"npm test is unsafe", "npm test", false},
		{"npm run is unsafe", "npm run build", false},
		{"cargo test is unsafe", "cargo test", false},
		{"kill is unsafe", "kill 1234", false},
		{"killall is unsafe", "killall vim", false},
		{"nohup is unsafe", "nohup sleep 10", false},
		{"nice is unsafe", "nice -n 10 ls", false},
		{"timeout is unsafe", "timeout 5 ls", false},
		{"yes is unsafe", "yes", false},
		{"gofumpt is unsafe", "gofumpt -w .", false},
		{"eslint is unsafe", "eslint src/", false},
		{"pytest is unsafe", "pytest tests/", false},
		{"go generate is unsafe", "go generate ./...", false},
		{"task is unsafe", "task lint", false},

		// Standard unsafe.
		{"rm is unsafe", "rm -rf /", false},
		{"mv is unsafe", "mv foo bar", false},
		{"cp is unsafe", "cp a b", false},
		{"chmod is unsafe", "chmod 777 foo", false},
		{"chown is unsafe", "chown root foo", false},
		{"docker is unsafe", "docker run alpine", false},
		{"kubectl is unsafe", "kubectl delete pod", false},
		{"empty command", "", true},
		{"arbitrary command", "my-custom-script --flag", false},

		// Dash in command name must not match.
		{"dash does not match", "git-status", false},
		{"prefix but not command", "lsof", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.safe, isSafeCommand(tt.cmd))
		})
	}
}

func TestIsSafeCommand_CompoundCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  string
		safe bool
	}{
		{
			"cd && git status && git diff",
			"cd /home/amos/git/kitty && git status && git diff --stat HEAD",
			true,
		},
		{
			"multiple git commands",
			"git status && git diff HEAD && git log -n 3",
			true,
		},
		{
			"pipe grep",
			"cat file.go | grep -n TODO",
			true,
		},
		{
			"pipe with wc",
			"ls -la | wc -l",
			true,
		},
		{
			"semicolon separated",
			"echo hello; pwd; ls -la",
			true,
		},
		{
			"or operator",
			"git status || echo failed",
			true,
		},
		{
			"mixed operators safe",
			"cd /tmp && ls -la | grep foo; echo done",
			true,
		},
		{
			"one unsafe in chain",
			"git status && rm -rf . && echo done",
			false,
		},
		{
			"unsafe piped to safe",
			"docker ps | grep running",
			false,
		},
		{
			"safe piped to unsafe",
			"echo hello | tee /etc/passwd",
			false,
		},

		// Shell metacharacter bypass attempts.
		{
			"command substitution is unsafe",
			"echo $(rm -rf /)",
			false,
		},
		{
			"backtick substitution is unsafe",
			"echo `rm -rf /`",
			false,
		},
		{
			"output redirect is unsafe",
			"echo hello > /etc/passwd",
			false,
		},
		{
			"append redirect is unsafe",
			"echo hello >> /etc/passwd",
			false,
		},
		{
			"input redirect is unsafe",
			"cat < /etc/shadow",
			false,
		},
		{
			"background exec is unsafe",
			"ls & rm -rf /",
			false,
		},
		{
			"dollar variable is unsafe",
			"echo $HOME",
			false,
		},
		{
			"newline is unsafe",
			"echo hello\nrm -rf /",
			false,
		},
		{
			"go test with 2>&1 redirect is unsafe",
			"go test ./... 2>&1 | tail -20",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.safe, isSafeCommand(tt.cmd))
		})
	}
}

func TestSplitShellCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cmd   string
		parts int
	}{
		{"single command", "ls -la", 1},
		{"two with &&", "cd /tmp && ls", 2},
		{"three with &&", "a && b && c", 3},
		{"pipe", "cat f | grep x", 2},
		{"semicolon", "a; b; c", 3},
		{"mixed", "a && b | c; d || e", 5},
		{"empty", "", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parts := splitShellCommands(tt.cmd)
			require.Len(t, parts, tt.parts)
		})
	}
}

func TestMatchesSafeCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cmd   string
		match bool
	}{
		{"exact match", "pwd", true},
		{"with args", "ls -la", true},
		{"with dash flag", "git-status", false},
		{"prefix but not command", "lsof", false},
		{"git status", "git status", true},
		{"git status porcelain", "git status --porcelain", true},
		{"git commit", "git commit -m 'msg'", false},
		{"git push", "git push origin main", false},

		// Dash in command name no longer matches.
		{"git diff-tree does not match", "git diff-tree HEAD", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.match, matchesSafeCommand(tt.cmd))
		})
	}
}
