package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReleaseUpdateAssets_CurrentReleaseWithoutStandaloneAgent(t *testing.T) {
	var rel githubRelease
	if err := json.Unmarshal([]byte(`{
		"tag_name": "v1.0.0",
		"assets": [
			{"name": "nasnet-panel-linux-amd64", "browser_download_url": "https://example.test/panel-amd64"},
			{"name": "nasnet-panel-linux-arm64", "browser_download_url": "https://example.test/panel-arm64"},
			{"name": "nasnet-tool-linux-amd64", "browser_download_url": "https://example.test/tool-amd64"},
			{"name": "nasnet-tool-linux-arm64", "browser_download_url": "https://example.test/tool-arm64"},
			{"name": "checksums.txt", "browser_download_url": "https://example.test/checksums"}
		]
	}`), &rel); err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			assets, err := releaseUpdateAssets(&rel, arch)
			if err != nil {
				t.Fatal(err)
			}
			if len(assets) != 3 {
				t.Fatalf("download set = %+v", assets)
			}
			wantURLs := []string{"https://example.test/panel-" + arch, "https://example.test/tool-" + arch, "https://example.test/checksums"}
			for i, asset := range assets {
				if asset.BrowserDownloadURL != wantURLs[i] {
					t.Errorf("asset %s URL = %q, want %q", asset.Name, asset.BrowserDownloadURL, wantURLs[i])
				}
			}
		})
	}
}

func TestReleaseUpdateAssets_RequiresPanelAndChecksums(t *testing.T) {
	panel := githubReleaseAsset{Name: "nasnet-panel-linux-amd64", BrowserDownloadURL: "https://example.test/panel"}
	checksums := githubReleaseAsset{Name: "checksums.txt", BrowserDownloadURL: "https://example.test/checksums"}
	for _, tc := range []struct {
		name    string
		assets  []githubReleaseAsset
		missing string
	}{
		{name: "tool is optional", assets: []githubReleaseAsset{panel, checksums}},
		{name: "missing panel", assets: []githubReleaseAsset{checksums}, missing: panel.Name},
		{name: "missing checksums", assets: []githubReleaseAsset{panel}, missing: checksums.Name},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assets, err := releaseUpdateAssets(&githubRelease{TagName: "v1.0.0", Assets: tc.assets}, "amd64")
			if tc.missing != "" {
				if err == nil || !strings.Contains(err.Error(), tc.missing) {
					t.Fatalf("error = %v, want missing %s", err, tc.missing)
				}
				return
			}
			if err != nil || len(assets) != 2 {
				t.Fatalf("download set = %+v, err = %v", assets, err)
			}
		})
	}
}
