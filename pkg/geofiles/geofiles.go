package geofiles

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/geofiles/embedded"
)

// Region represents a geographic region preset for geofile sources.
type Region string

const (
	RegionIran   Region = "iran"
	RegionChina  Region = "china"
	RegionRussia Region = "russia"
	RegionCustom Region = "custom"
)

// GeoSource holds download URLs for geoip.dat and geosite.dat.
type GeoSource struct {
	GeoIPURL   string
	GeoSiteURL string
}

// regionPresets maps each preset region to its upstream geofile URLs.
var regionPresets = map[Region]GeoSource{
	RegionIran: {
		GeoIPURL:   "https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geoip.dat",
		GeoSiteURL: "https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geosite.dat",
	},
	RegionChina: {
		GeoIPURL:   "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
		GeoSiteURL: "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat",
	},
	RegionRussia: {
		GeoIPURL:   "https://github.com/v2fly/geoip/releases/latest/download/geoip.dat",
		GeoSiteURL: "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat",
	},
}

// GetSource returns the GeoSource for a preset region.
// For RegionCustom, callers should construct GeoSource manually.
func GetSource(region Region) (GeoSource, bool) {
	src, ok := regionPresets[region]
	return src, ok
}

// GetEmbeddedGeoFiles returns the build-time embedded geoip.dat and geosite.dat
// for the given region. Currently only RegionIran has embedded files.
// Returns ok=false if no embedded files exist for the region.
func GetEmbeddedGeoFiles(region Region) (geoipData, geositeData []byte, ok bool) {
	if region == RegionIran && len(embedded.IranGeoIP) > 0 && len(embedded.IranGeoSite) > 0 {
		return embedded.IranGeoIP, embedded.IranGeoSite, true
	}
	return nil, nil, false
}

// Download fetches a geofile from the given URL with a 2-minute timeout.
// It validates the response is non-empty and returns the raw bytes.
// The caller supplies the *http.Client so proxy routing can be applied
// (use httpclient.Factory.ClientFor on the hub side).
func Download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download geofile from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from %s: %w", url, err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty response from %s", url)
	}

	return data, nil
}

// DownloadGeoFiles downloads both geoip.dat and geosite.dat concurrently
// from the given source via the supplied http.Client. Returns the file
// contents or an error.
func DownloadGeoFiles(ctx context.Context, client *http.Client, src GeoSource) (geoipData []byte, geositeData []byte, err error) {
	var wg sync.WaitGroup
	var geoipErr, geositeErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		geoipData, geoipErr = Download(ctx, client, src.GeoIPURL)
	}()
	go func() {
		defer wg.Done()
		geositeData, geositeErr = Download(ctx, client, src.GeoSiteURL)
	}()
	wg.Wait()

	if geoipErr != nil {
		return nil, nil, fmt.Errorf("geoip download failed: %w", geoipErr)
	}
	if geositeErr != nil {
		return nil, nil, fmt.Errorf("geosite download failed: %w", geositeErr)
	}

	return geoipData, geositeData, nil
}
