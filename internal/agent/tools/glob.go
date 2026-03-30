package tools

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/fsext"
)

const GlobToolName = "glob"

//go:embed glob.md
var globDescription []byte

type GlobParams struct {
	Pattern string `json:"pattern" description:"The glob pattern to match files against"`
	Path    string `json:"path,omitempty" description:"The directory to search in. Defaults to the current working directory."`
}

type GlobResponseMetadata struct {
	NumberOfFiles int  `json:"number_of_files"`
	Truncated     bool `json:"truncated"`
}

func NewGlobTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		GlobToolName,
		string(globDescription),
		func(ctx context.Context, params GlobParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Pattern == "" {
				return fantasy.NewTextErrorResponse("pattern is required"), nil
			}

			searchPath := cmp.Or(params.Path, workingDir)

			globCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			files, truncated, err := globFiles(globCtx, params.Pattern, searchPath, 100)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error finding files: %w", err)
			}

			var output string
			if len(files) == 0 {
				output = "No files found"
			} else {
				normalizeFilePaths(files)
				output = strings.Join(files, "\n")
				if truncated {
					output += "\n\n(Results truncated to 100 files. Use a more specific pattern or path to narrow results.)"
				}
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(output),
				GlobResponseMetadata{
					NumberOfFiles: len(files),
					Truncated:     truncated,
				},
			), nil
		})
}

func globFiles(ctx context.Context, pattern, searchPath string, limit int) ([]string, bool, error) {
	cmdRg := getRgCmd(ctx, pattern)
	if cmdRg != nil {
		cmdRg.Dir = searchPath
		matches, err := runRipgrep(cmdRg, searchPath, limit)
		if err == nil {
			return matches, len(matches) >= limit && limit > 0, nil
		}
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		slog.Warn("Ripgrep execution failed, falling back to doublestar", "error", err)
	}

	return fsext.GlobGitignoreAware(ctx, pattern, searchPath, limit)
}

func runRipgrep(cmd *exec.Cmd, searchRoot string, limit int) ([]string, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("ripgrep: %w\n%s", err, out)
	}

	type fileEntry struct {
		path    string
		modTime time.Time
	}

	var entries []fileEntry
	for p := range bytes.SplitSeq(out, []byte{0}) {
		if len(p) == 0 {
			continue
		}
		absPath := string(p)
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(searchRoot, absPath)
		}
		fi, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		entries = append(entries, fileEntry{path: absPath, modTime: fi.ModTime()})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].modTime.After(entries[j].modTime)
	})

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	matches := make([]string, len(entries))
	for i, e := range entries {
		matches[i] = e.path
	}
	return matches, nil
}

func normalizeFilePaths(paths []string) {
	for i, p := range paths {
		paths[i] = filepath.ToSlash(p)
	}
}
