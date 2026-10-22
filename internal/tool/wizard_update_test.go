package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestAssetsAreMatchedByExactName(t *testing.T) {
	rel := &githubRelease{Assets: []githubReleaseAsset{
		{Name: "nasnet-panel-linux-amd64.sig", BrowserDownloadURL: "https://example.test/sig"},
		{Name: "nasnet-panel-linux-amd64", BrowserDownloadURL: "https://example.test/panel"},
	}}
	if got := findAssetURL(rel, "nasnet-panel-linux-amd64"); got != "https://example.test/panel" {
		t.Fatalf("got %s", got)
	}
	if got := findAssetURL(rel, "nasnet-panel"); got != "" {
		t.Fatalf("a partial name matched %s", got)
	}
}

func TestPanelUpdaterNeedsTheUpdateCommand(t *testing.T) {
	dir := t.TempDir()
	if _, ok := panelUpdater(dir); ok {
		t.Fatal("no binary, yet an updater")
	}
	bin := filepath.Join(dir, "bin", "nasnet-panel")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("#!/bin/sh\n[ \"$1 $2\" = \"update apply\" ]\n"), 0o755)
	if got, ok := panelUpdater(dir); !ok || got != bin {
		t.Fatalf("updater = %s %v", got, ok)
	}
	_ = os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755)
	if _, ok := panelUpdater(dir); ok {
		t.Fatal("a panel without the update command counts as one")
	}
}
