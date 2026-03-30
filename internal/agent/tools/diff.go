package tools

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/fantasy"
)

const (
	DiffToolName     = "diff"
	MaxDiffOutputLen = 30000
)

//go:embed diff.md
var diffDescription []byte

type DiffParams struct {
	FileA string `json:"file_a,omitempty" description:"First file path for comparison"`
	FileB string `json:"file_b,omitempty" description:"Second file path for comparison"`
	Ref   string `json:"ref,omitempty" description:"Git ref to diff against (e.g., HEAD, main, HEAD~3). If provided without files, shows all changes vs that ref."`
}

type DiffResponseMetadata struct {
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

func NewDiffTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		DiffToolName,
		string(diffDescription),
		func(ctx context.Context, params DiffParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			// Validate ref does not look like a flag.
			if params.Ref != "" && strings.HasPrefix(params.Ref, "-") {
				return fantasy.NewTextErrorResponse("ref must be a git ref (e.g., HEAD, main), not a flag"), nil
			}

			// Validate file paths stay within the working directory.
			if params.FileA != "" {
				if err := validateDiffPath(workingDir, params.FileA); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
			}
			if params.FileB != "" {
				if err := validateDiffPath(workingDir, params.FileB); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
			}

			// Case 1: Diff two files.
			if params.FileA != "" && params.FileB != "" {
				return diffTwoFiles(ctx, workingDir, params.FileA, params.FileB)
			}

			// Case 2: Git diff against a ref.
			if params.Ref != "" {
				return gitDiff(ctx, workingDir, params.Ref, params.FileA)
			}

			// Case 3: Git diff of unstaged changes (default).
			if params.FileA != "" {
				return gitDiff(ctx, workingDir, "", params.FileA)
			}

			return gitDiff(ctx, workingDir, "", "")
		})
}

// validateDiffPath ensures the resolved path stays within the working
// directory. Absolute paths outside the working directory are rejected,
// and relative paths are resolved and checked for traversal.
func validateDiffPath(workingDir, file string) error {
	resolved := resolveFilePath(workingDir, file)
	resolved = filepath.Clean(resolved)
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return fmt.Errorf("cannot resolve working directory: %w", err)
	}
	rel, err := filepath.Rel(absWorkingDir, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path %q is outside working directory", file)
	}
	return nil
}

func diffTwoFiles(ctx context.Context, workingDir, fileA, fileB string) (fantasy.ToolResponse, error) {
	fileA = resolveFilePath(workingDir, fileA)
	fileB = resolveFilePath(workingDir, fileB)

	if _, err := os.Stat(fileA); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("file_a not found: %s", fileA)), nil
	}
	if _, err := os.Stat(fileB); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("file_b not found: %s", fileB)), nil
	}

	cmd := exec.CommandContext(ctx, "diff", "-u", "--label", fileA, "--label", fileB, fileA, fileB)
	cmd.Dir = workingDir
	out, err := cmd.CombinedOutput()

	output := string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				// Exit code 1 = files differ (normal).
				meta := countDiffLines(output)
				output = TruncateString(output, MaxDiffOutputLen)
				return fantasy.WithResponseMetadata(
					fantasy.NewTextResponse(output),
					meta,
				), nil
			}
		}
		return fantasy.NewTextErrorResponse(fmt.Sprintf("diff failed: %s\n%s", err, output)), nil
	}

	return fantasy.NewTextResponse("Files are identical."), nil
}

func gitDiff(ctx context.Context, workingDir, ref, file string) (fantasy.ToolResponse, error) {
	args := []string{"diff", "--no-color"}
	if ref != "" {
		// Insert -- before the ref to prevent flag injection, then use
		// the ref as a revision argument. Git treats arguments after --
		// as paths, so we place -- between ref and file instead.
		args = append(args, ref)
	}
	if file != "" {
		file = resolveFilePath(workingDir, file)
	}
	// Always insert -- to separate revisions from paths.
	args = append(args, "--")
	if file != "" {
		args = append(args, file)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workingDir
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("git diff failed: %s\n%s", err, output)), nil
	}

	if strings.TrimSpace(output) == "" {
		return fantasy.NewTextResponse("No differences found."), nil
	}

	meta := countDiffLines(output)
	output = TruncateString(output, MaxDiffOutputLen)
	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(output),
		meta,
	), nil
}

func resolveFilePath(workingDir, file string) string {
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(workingDir, file)
}

func countDiffLines(diff string) DiffResponseMetadata {
	var added, removed int
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removed++
		}
	}
	return DiffResponseMetadata{LinesAdded: added, LinesRemoved: removed}
}
