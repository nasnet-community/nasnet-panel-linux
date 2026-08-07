package tool

import (
	"net/http"
	"os"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
)

// wizardHTTPClient honors OUTBOUND_PROXY_URL env var. nasnet-tool runs as a
// separate process and can't read hub DB settings, so env-only. Empty/invalid
// → direct-internet client.
func wizardHTTPClient(_ *Config) *http.Client {
	proxyURL := os.Getenv("OUTBOUND_PROXY_URL")
	if proxyURL == "" {
		return &http.Client{Timeout: 15 * time.Second}
	}

	// Use a one-shot factory: parses + validates the URL, falls back to
	// direct on failure.
	f := httpclient.NewFactory()
	f.Update(httpclient.Config{
		ProxyURL: proxyURL,
		Enabled:  map[httpclient.Feature]bool{httpclient.FeatureWizard: true},
	})
	return f.ClientFor(httpclient.FeatureWizard, httpclient.EgressForeign, 15*time.Second)
}
