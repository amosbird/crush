package update

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	githubReleasesURL = "https://github.com/amosbird/crush/releases/latest"
	userAgent         = "crush/1.0"
)

// Default is the default [Client].
var Default Client = &github{}

// Info contains information about an available update.
type Info struct {
	Current       string
	Latest        string
	URL           string
	BuildTime     time.Time
	LatestPubTime time.Time
}

// Matches a version string like:
// v0.0.0-0.20251231235959-06c807842604
var goInstallRegexp = regexp.MustCompile(`^v?\d+\.\d+\.\d+-\d+\.\d{14}-[0-9a-f]{12}$`)

func (i Info) IsDevelopment() bool {
	return i.Current == "devel" || i.Current == "unknown" || strings.Contains(i.Current, "dirty") || goInstallRegexp.MatchString(i.Current)
}

// Available returns true if there's an update available.
//
// For development builds, compares the local build time against the
// release publish time — only offers an update if the release is newer.
// For release builds, compares version strings.
func (i Info) Available() bool {
	if i.IsDevelopment() {
		if i.BuildTime.IsZero() {
			return true
		}
		return i.LatestPubTime.After(i.BuildTime)
	}
	cpr := strings.Contains(i.Current, "-")
	lpr := strings.Contains(i.Latest, "-")
	if cpr && !lpr {
		return true
	}
	if lpr && !cpr {
		return false
	}
	return i.Current != i.Latest
}

// Check checks if a new version is available.
func Check(ctx context.Context, current string, buildTime time.Time, client Client) (Info, error) {
	info := Info{
		Current:   current,
		Latest:    current,
		BuildTime: buildTime,
	}

	release, err := client.Latest(ctx)
	if err != nil {
		return info, fmt.Errorf("failed to fetch latest release: %w", err)
	}

	info.Latest = strings.TrimPrefix(release.TagName, "v")
	info.Current = strings.TrimPrefix(info.Current, "v")
	info.URL = release.HTMLURL
	info.LatestPubTime = release.PublishedAt
	return info, nil
}

// Release represents a GitHub release.
type Release struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// Client is a client that can get the latest release.
type Client interface {
	Latest(ctx context.Context) (*Release, error)
}

type github struct{}

// Latest implements [Client].
func (c *github) Latest(ctx context.Context) (*Release, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, "HEAD", githubReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		return nil, fmt.Errorf("expected redirect from GitHub, got status %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc == "" {
		return nil, fmt.Errorf("no Location header in GitHub redirect")
	}

	// Location is like https://github.com/{owner}/{repo}/releases/tag/{tag}
	idx := strings.LastIndex(loc, "/tag/")
	if idx == -1 {
		return nil, fmt.Errorf("unexpected redirect location: %s", loc)
	}
	tag := loc[idx+len("/tag/"):]

	return &Release{
		TagName: tag,
		HTMLURL: loc,
	}, nil
}
