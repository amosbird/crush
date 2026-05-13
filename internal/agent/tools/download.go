package tools

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/filepathext"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/permission"
)

type DownloadParams struct {
	URL      string `json:"url" description:"The URL to download from"`
	FilePath string `json:"file_path" description:"The local file path where the downloaded content should be saved"`
	Timeout  int    `json:"timeout,omitempty" description:"Optional timeout in seconds (max 600)"`
}

type DownloadPermissionsParams struct {
	URL      string `json:"url"`
	FilePath string `json:"file_path"`
	Timeout  int    `json:"timeout,omitempty"`
}

const (
	DownloadToolName = "download"

	// MaxDownloadSize is the maximum allowed download size (100 MB).
	MaxDownloadSize = 100 * 1024 * 1024
)

//go:embed download.md
var downloadDescription []byte

func NewDownloadTool(permissions permission.Service, workingDir string, client *http.Client) fantasy.AgentTool {
	if client == nil {
		client = &http.Client{
			Timeout:   5 * time.Minute, // Default 5 minute timeout for downloads
			Transport: http.DefaultTransport.(*http.Transport).Clone(),
		}
	}
	return fantasy.NewParallelAgentTool(
		DownloadToolName,
		string(downloadDescription),
		func(ctx context.Context, params DownloadParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.URL == "" {
				return fantasy.NewTextErrorResponse("URL parameter is required"), nil
			}

			if params.FilePath == "" {
				return fantasy.NewTextErrorResponse("file_path parameter is required"), nil
			}

			if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
				return fantasy.NewTextErrorResponse("URL must start with http:// or https://"), nil
			}

			filePath := filepathext.SmartJoin(workingDir, params.FilePath)

			if !fsext.HasPrefix(filePath, workingDir) {
				return fantasy.NewTextErrorResponse("file_path must be within the working directory"), nil
			}

			relPath, _ := filepath.Rel(workingDir, filePath)
			relPath = filepath.ToSlash(cmp.Or(relPath, filePath))

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session_id is required")
			}

			p, err := permissions.Request(ctx,
				permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        fsext.PathOrPrefix(filePath, workingDir),
					ToolCallID:  call.ID,
					ToolName:    DownloadToolName,
					Action:      "download",
					Description: fmt.Sprintf("Download %s to %s", params.URL, filePath),
					Params:      DownloadPermissionsParams(params),
				},
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !p {
				return fantasy.ToolResponse{}, permission.ErrorPermissionDenied
			}

			// Handle timeout with context
			requestCtx := ctx
			if params.Timeout > 0 {
				maxTimeout := 600 // 10 minutes
				if params.Timeout > maxTimeout {
					params.Timeout = maxTimeout
				}
				var cancel context.CancelFunc
				requestCtx, cancel = context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
				defer cancel()
			}

			req, err := http.NewRequestWithContext(requestCtx, "GET", params.URL, nil)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to create request: %s", err)), nil
			}

			req.Header.Set("User-Agent", "crush/1.0")

			resp, err := client.Do(req)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to download from URL: %s", err)), nil
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Request failed with status code: %d", resp.StatusCode)), nil
			}

			// Create parent directories after permission check.
			if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to create parent directories: %s", err)), nil
			}

			// Create the output file
			outFile, err := os.Create(filePath)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to create output file: %s", err)), nil
			}
			defer outFile.Close()

			// Limit download size to prevent disk exhaustion.
			bytesWritten, err := io.Copy(outFile, io.LimitReader(resp.Body, MaxDownloadSize))
			if err != nil {
				outFile.Close()
				os.Remove(filePath)
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to write file: %s", err)), nil
			}

			contentType := resp.Header.Get("Content-Type")
			responseMsg := fmt.Sprintf("Successfully downloaded %d bytes to %s", bytesWritten, relPath)
			if contentType != "" {
				responseMsg += fmt.Sprintf(" (Content-Type: %s)", contentType)
			}
			if bytesWritten >= MaxDownloadSize {
				responseMsg += fmt.Sprintf("\n\nWarning: download was truncated at %d bytes", MaxDownloadSize)
			}

			return fantasy.NewTextResponse(responseMsg), nil
		})
}
