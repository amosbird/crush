package tools

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/fsext"
)

// regexCache provides thread-safe caching of compiled regex patterns
type regexCache struct {
	*csync.Map[string, *regexp.Regexp]
}

// newRegexCache creates a new regex cache
func newRegexCache() *regexCache {
	return &regexCache{
		Map: csync.NewMap[string, *regexp.Regexp](),
	}
}

// get retrieves a compiled regex from cache or compiles and caches it
func (rc *regexCache) get(pattern string) (*regexp.Regexp, error) {
	if cached, ok := rc.Get(pattern); ok {
		if cached == nil {
			return nil, fmt.Errorf("previously failed to compile %q", pattern)
		}
		return cached, nil
	}
	regex, err := regexp.Compile(pattern)
	if err != nil {
		rc.Set(pattern, nil)
		return nil, err
	}
	rc.Set(pattern, regex)
	return regex, nil
}

// ResetCache clears compiled regex caches to prevent unbounded growth across sessions.
func ResetCache() {
	searchRegexCache.Reset(map[string]*regexp.Regexp{})
	globRegexCache.Reset(map[string]*regexp.Regexp{})
}

// Global regex cache instances
var (
	searchRegexCache = newRegexCache()
	globRegexCache   = newRegexCache()
	// Pre-compiled regex for glob conversion (used frequently)
	globBraceRegex = regexp.MustCompile(`\{([^}]+)\}`)
)

type GrepParams struct {
	Pattern     string `json:"pattern" description:"The regex pattern to search for in file contents"`
	Path        string `json:"path,omitempty" description:"The directory to search in. Defaults to the current working directory."`
	Include     string `json:"include,omitempty" description:"File pattern to include in the search (e.g. \"*.js\", \"*.{ts,tsx}\")"`
	LiteralText bool   `json:"literal_text,omitempty" description:"If true, the pattern will be treated as literal text with special regex characters escaped. Default is false."`
	Context     int    `json:"context,omitempty" description:"Number of context lines to show before and after each match (0-5). Only works with ripgrep."`
}

type grepMatch struct {
	path      string
	modTime   time.Time
	lineNum   int
	charNum   int
	lineText  string
	isContext bool
}

type GrepResponseMetadata struct {
	NumberOfMatches int  `json:"number_of_matches"`
	Truncated       bool `json:"truncated"`
}

const (
	GrepToolName        = "grep"
	maxGrepContentWidth = 500
)

//go:embed grep.md
var grepDescription []byte

// escapeRegexPattern escapes special regex characters so they're treated as literal characters
func escapeRegexPattern(pattern string) string {
	specialChars := []string{"\\", ".", "+", "*", "?", "(", ")", "[", "]", "{", "}", "^", "$", "|"}
	escaped := pattern

	for _, char := range specialChars {
		escaped = strings.ReplaceAll(escaped, char, "\\"+char)
	}

	return escaped
}

func NewGrepTool(workingDir string, config config.ToolGrep) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		GrepToolName,
		string(grepDescription),
		func(ctx context.Context, params GrepParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Pattern == "" {
				return fantasy.NewTextErrorResponse("pattern is required"), nil
			}

			searchPattern := params.Pattern
			if params.LiteralText {
				searchPattern = escapeRegexPattern(params.Pattern)
			}

			searchPath := cmp.Or(params.Path, workingDir)

			searchCtx, cancel := context.WithTimeout(ctx, config.GetTimeout())
			defer cancel()

			matches, truncated, err := searchFiles(searchCtx, searchPattern, searchPath, params.Include, params.Context, 100)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("error searching files: %v", err)), nil
			}

			var output strings.Builder
			var matchCount int
			if len(matches) == 0 {
				output.WriteString("No files found")
			} else {
				for _, m := range matches {
					if !m.isContext {
						matchCount++
					}
				}
				fmt.Fprintf(&output, "Found %d matches\n", matchCount)

				currentFile := ""
				for _, match := range matches {
					if currentFile != match.path {
						if currentFile != "" {
							output.WriteString("\n")
						}
						currentFile = match.path
						fmt.Fprintf(&output, "%s:\n", filepath.ToSlash(match.path))
					}
					if match.isContext {
						lineText := match.lineText
						if len(lineText) > maxGrepContentWidth {
							lineText = lineText[:maxGrepContentWidth] + "..."
						}
						fmt.Fprintf(&output, "  %d: %s\n", match.lineNum, lineText)
						continue
					}
					if match.lineNum > 0 {
						lineText := match.lineText
						if len(lineText) > maxGrepContentWidth {
							lineText = lineText[:maxGrepContentWidth] + "..."
						}
						if match.charNum > 0 {
							fmt.Fprintf(&output, "  Line %d, Char %d: %s\n", match.lineNum, match.charNum, lineText)
						} else {
							fmt.Fprintf(&output, "  Line %d: %s\n", match.lineNum, lineText)
						}
					} else {
						fmt.Fprintf(&output, "  %s\n", match.path)
					}
				}

				if truncated {
					output.WriteString("\n(Results truncated to 100 matches. Narrow your search with a more specific pattern, path, or include filter. Use the Agent tool to delegate deeper searches.)")
				}
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(output.String()),
				GrepResponseMetadata{
					NumberOfMatches: matchCount,
					Truncated:       truncated,
				},
			), nil
		})
}

func searchFiles(ctx context.Context, pattern, rootPath, include string, contextLines, limit int) ([]grepMatch, bool, error) {
	if contextLines > 5 {
		contextLines = 5
	}
	matches, err := searchWithRipgrep(ctx, pattern, rootPath, include, contextLines)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		matches, err = searchFilesWithRegex(ctx, pattern, rootPath, include)
		if err != nil {
			return nil, false, err
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})

	// Truncate by match count (not including context lines).
	var matchCount int
	var truncated bool
	for i, m := range matches {
		if m.isContext {
			continue
		}
		matchCount++
		if matchCount > limit {
			matches = matches[:i]
			truncated = true
			break
		}
	}

	return matches, truncated, nil
}

func searchWithRipgrep(ctx context.Context, pattern, path, include string, contextLines int) ([]grepMatch, error) {
	cmd := getRgSearchCmd(ctx, pattern, path, include, contextLines)
	if cmd == nil {
		return nil, fmt.Errorf("ripgrep not found in $PATH")
	}

	// Only add ignore files if they exist
	for _, ignoreFile := range []string{".gitignore", ".crushignore"} {
		ignorePath := filepath.Join(path, ignoreFile)
		if _, err := os.Stat(ignorePath); err == nil {
			cmd.Args = append(cmd.Args, "--ignore-file", ignorePath)
		}
	}

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []grepMatch{}, nil
		}
		return nil, err
	}

	var matches []grepMatch
	modTimeCache := make(map[string]time.Time)
	fileModTime := func(path string) time.Time {
		if t, ok := modTimeCache[path]; ok {
			return t
		}
		fi, err := os.Stat(path)
		if err != nil {
			return time.Time{}
		}
		modTimeCache[path] = fi.ModTime()
		return fi.ModTime()
	}
	for line := range bytes.SplitSeq(bytes.TrimSpace(output), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var match ripgrepMatch
		if err := json.Unmarshal(line, &match); err != nil {
			continue
		}
		switch match.Type {
		case "match":
			for _, m := range match.Data.Submatches {
				mt := fileModTime(match.Data.Path.Text)
				if mt.IsZero() {
					continue
				}
				matches = append(matches, grepMatch{
					path:     match.Data.Path.Text,
					modTime:  mt,
					lineNum:  match.Data.LineNumber,
					charNum:  m.Start + 1,
					lineText: strings.TrimSpace(match.Data.Lines.Text),
				})
				break
			}
		case "context":
			if contextLines > 0 {
				matches = append(matches, grepMatch{
					path:      match.Data.Path.Text,
					modTime:   fileModTime(match.Data.Path.Text),
					lineNum:   match.Data.LineNumber,
					lineText:  strings.TrimSpace(match.Data.Lines.Text),
					isContext: true,
				})
			}
		}
	}
	return matches, nil
}

type ripgrepMatch struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

func searchFilesWithRegex(ctx context.Context, pattern, rootPath, include string) ([]grepMatch, error) {
	matches := []grepMatch{}

	// Use cached regex compilation
	regex, err := searchRegexCache.get(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	var includePattern *regexp.Regexp
	if include != "" {
		regexPattern := globToRegex(include)
		includePattern, err = globRegexCache.get(regexPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid include pattern: %w", err)
		}
	}

	// Create walker with gitignore and crushignore support
	walker := fsext.NewFastGlobWalker(rootPath)

	err = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if ctx.Err() != nil {
			return filepath.SkipAll
		}

		if info.IsDir() {
			// Check if directory should be skipped
			if walker.ShouldSkip(path) {
				return filepath.SkipDir
			}
			return nil // Continue into directory
		}

		// Use walker's shouldSkip method for files
		if walker.ShouldSkip(path) {
			return nil
		}

		// Skip hidden files (starting with a dot) to match ripgrep's default behavior
		base := filepath.Base(path)
		if base != "." && strings.HasPrefix(base, ".") {
			return nil
		}

		if includePattern != nil && !includePattern.MatchString(path) {
			return nil
		}

		match, lineNum, charNum, lineText, err := fileContainsPattern(path, regex)
		if err != nil {
			return nil // Skip files we can't read
		}

		if match {
			matches = append(matches, grepMatch{
				path:     path,
				modTime:  info.ModTime(),
				lineNum:  lineNum,
				charNum:  charNum,
				lineText: lineText,
			})

			if len(matches) >= 200 {
				return filepath.SkipAll
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return matches, nil
}

func fileContainsPattern(filePath string, pattern *regexp.Regexp) (bool, int, int, string, error) {
	// Only search text files.
	if !isTextFile(filePath) {
		return false, 0, 0, "", nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return false, 0, 0, "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if loc := pattern.FindStringIndex(line); loc != nil {
			charNum := loc[0] + 1
			return true, lineNum, charNum, line, nil
		}
	}

	return false, 0, 0, "", scanner.Err()
}

// isTextFile checks if a file is a text file by examining its MIME type.
func isTextFile(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	// Read first 512 bytes for MIME type detection.
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false
	}

	// Detect content type.
	contentType := http.DetectContentType(buffer[:n])

	// Check if it's a text MIME type.
	return strings.HasPrefix(contentType, "text/") ||
		contentType == "application/json" ||
		contentType == "application/xml" ||
		contentType == "application/javascript" ||
		contentType == "application/x-sh"
}

func globToRegex(glob string) string {
	regexPattern := strings.ReplaceAll(glob, ".", "\\.")
	regexPattern = strings.ReplaceAll(regexPattern, "*", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "?", ".")

	// Use pre-compiled regex instead of compiling each time
	regexPattern = globBraceRegex.ReplaceAllStringFunc(regexPattern, func(match string) string {
		inner := match[1 : len(match)-1]
		return "(" + strings.ReplaceAll(inner, ",", "|") + ")"
	})

	return regexPattern
}
