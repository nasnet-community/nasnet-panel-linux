package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
)

// Repo is where releases come from.
const Repo = "nasnet-community/nasnet-panel-linux"

// Channels.
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

// ReleaseURL is a release asset's github.com URL; an empty tag means the
// latest stable release.
func ReleaseURL(tag, name string) string {
	if tag == "" {
		return "https://github.com/" + Repo + "/releases/latest/download/" + name
	}
	return "https://github.com/" + Repo + "/releases/download/" + tag + "/" + name
}

const feedURL = "https://github.com/" + Repo + "/releases.atom"

// Fetcher gets one file over the first route that works and names that route.
type Fetcher interface {
	Get(ctx context.Context, url string, limit int64) ([]byte, string, error)
}

// Offer is a release as the Updates section shows it.
type Offer struct {
	Version    string    `json:"version"`
	Prerelease bool      `json:"prerelease"`
	Notes      string    `json:"notes"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256"`
	MinFrom    string    `json:"min_from,omitempty"`
	Xray       string    `json:"xray"`
	BuiltAt    time.Time `json:"built_at"`
}

// Check is what the last check found. A failed check is only shown, never
// announced.
type Check struct {
	CheckedAt time.Time `json:"checked_at"`
	Channel   string    `json:"channel"`
	Route     string    `json:"route,omitempty"`
	Latest    *Offer    `json:"latest,omitempty"`
	Available bool      `json:"available"`
	Blocked   string    `json:"blocked,omitempty"`    // newer, but not installable from here
	Error     string    `json:"error,omitempty"`      // nothing could be checked
	FeedError string    `json:"feed_error,omitempty"` // beta feed unreachable, stable still checked
}

type Checker struct {
	fetch Fetcher
	arch  string
	now   func() time.Time
}

func NewChecker(f Fetcher, arch string) *Checker {
	return &Checker{fetch: f, arch: arch, now: time.Now}
}

// Check finds the newest release the channel offers above running. Turning
// beta off on a beta waits for the next stable; nothing goes backwards.
func (c *Checker) Check(ctx context.Context, running release.Version, channel string) Check {
	res := Check{CheckedAt: c.now().UTC(), Channel: channel}

	best, route, stableErr := c.manifest(ctx, "")
	if channel == ChannelBeta {
		beta, betaRoute, err := c.newestBeta(ctx, running)
		switch {
		case err != nil:
			res.FeedError = err.Error()
		case beta != nil && (best == nil || newer(beta, best)):
			best, route = beta, betaRoute
		}
	}
	if best == nil {
		if stableErr == nil {
			stableErr = errors.New("no release found")
		}
		res.Error = stableErr.Error()
		return res
	}
	res.Route = route
	res.Latest = c.offer(best)

	if !release.MustParse(best.Version).Newer(running) {
		return res
	}
	switch {
	case !release.AllowsFrom(best.MinFrom, running):
		res.Blocked = fmt.Sprintf("%s needs %s or later first.", best.Version, best.MinFrom)
	case res.Latest.SHA256 == "":
		res.Blocked = fmt.Sprintf("%s has no %s build.", best.Version, c.arch)
	default:
		res.Available = true
	}
	return res
}

// Manifest fetches and verifies one release's release.json ("" = latest).
func (c *Checker) Manifest(ctx context.Context, tag string) (*release.Manifest, string, error) {
	return c.manifest(ctx, tag)
}

func (c *Checker) manifest(ctx context.Context, tag string) (*release.Manifest, string, error) {
	data, route, err := c.fetch.Get(ctx, ReleaseURL(tag, release.ManifestName), 4<<20)
	if err != nil {
		return nil, "", err
	}
	sig, _, err := c.fetch.Get(ctx, ReleaseURL(tag, release.ManifestName+release.SigSuffix), 4<<10)
	if err != nil {
		return nil, "", err
	}
	m, err := release.ParseManifest(data, sig)
	if err != nil {
		return nil, "", err
	}
	if tag != "" && m.Version != tag {
		return nil, "", fmt.Errorf("update: %s carries the manifest of %s", tag, m.Version)
	}
	return m, route, nil
}

func (c *Checker) newestBeta(ctx context.Context, running release.Version) (*release.Manifest, string, error) {
	feed, _, err := c.fetch.Get(ctx, feedURL, 2<<20)
	if err != nil {
		return nil, "", fmt.Errorf("the release feed is unreachable: %w", err)
	}
	v, ok := release.Pick(release.AtomTags(feed), running, true)
	if !ok {
		return nil, "", nil
	}
	return c.manifest(ctx, v.String())
}

func (c *Checker) offer(m *release.Manifest) *Offer {
	a := m.Assets[release.BundleAsset(c.arch)]
	return &Offer{
		Version:    m.Version,
		Prerelease: m.Prerelease,
		Notes:      m.Notes,
		Size:       a.Size,
		SHA256:     a.SHA256,
		MinFrom:    m.MinFrom,
		Xray:       m.Xray,
		BuiltAt:    m.BuiltAt,
	}
}

func newer(a, b *release.Manifest) bool {
	return release.MustParse(a.Version).Newer(release.MustParse(b.Version))
}
