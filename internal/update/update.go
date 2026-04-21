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
var semverRegexp = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func (i Info) IsDevelopment() bool {
	v := strings.TrimSuffix(i.Current, "+dirty")
	return i.Current == "devel" || i.Current == "unknown" || goInstallRegexp.MatchString(v)
}

// Available returns true if there's an update available.
//
// For development builds, always considers an update available so the
// user can pull the latest release. For release builds, compares
// version strings.
func (i Info) Available() bool {
	if i.IsDevelopment() {
		return true
	}
	current := cleanVersion(i.Current)
	latest := i.Latest
	cpr := strings.Contains(current, "-")
	lpr := strings.Contains(latest, "-")
	if cpr && !lpr {
		return true
	}
	if lpr && !cpr {
		return false
	}
	return current != latest
}

// cleanVersion strips dirty suffixes and pseudo-version metadata so that
// a locally-built version like "0.56.1-0.20260413044447-d1274568a06b+dirty"
// is compared as its base version "0.56.1".
func cleanVersion(v string) string {
	v = strings.TrimSuffix(v, "+dirty")
	if goInstallRegexp.MatchString(v) {
		return v
	}
	// Strip pseudo-version suffix: "0.56.1-0.20260413...-abcdef123456" → "0.56.1".
	if i := strings.Index(v, "-0."); i != -1 {
		candidate := v[:i]
		if semverRegexp.MatchString(candidate) {
			return candidate
		}
	}
	return v
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
