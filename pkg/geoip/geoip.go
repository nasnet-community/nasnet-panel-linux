package geoip

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type Location struct {
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	ISP         string `json:"isp"` // Using ISP/Org as Datacenter
	Org         string `json:"org"`
	Status      string `json:"status"`
}

// ClientFactory is the optional outbound-proxy-aware HTTP client supplier.
// When set via SetClientFactory, Lookup uses it; otherwise a default 3s
// client is built per call.
type ClientFactory func() *http.Client

var clientFactory atomic.Pointer[ClientFactory]

// SetClientFactory installs an HTTP client factory for outbound lookups.
// Pass nil to revert to the default per-call client.
func SetClientFactory(f ClientFactory) {
	if f == nil {
		clientFactory.Store(nil)
		return
	}
	clientFactory.Store(&f)
}

func currentClient() *http.Client {
	if p := clientFactory.Load(); p != nil && *p != nil {
		if c := (*p)(); c != nil {
			return c
		}
	}
	return &http.Client{Timeout: 3 * time.Second}
}

func Lookup(ip string) (*Location, error) {
	client := currentClient()

	// filtering fields to save bandwidth: status,country,countryCode,isp,org
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,countryCode,isp,org", ip)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geoip api returned status: %d", resp.StatusCode)
	}

	var loc Location
	if err := json.NewDecoder(resp.Body).Decode(&loc); err != nil {
		return nil, err
	}

	if loc.Status == "fail" {
		return nil, fmt.Errorf("geoip lookup failed")
	}

	return &loc, nil
}
