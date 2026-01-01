package geoip

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/oschwald/geoip2-golang"
	log "github.com/sirupsen/logrus"
)

var (
	geolite2DB   *geoip2.Reader
	geolite2Once sync.Once
	geolite2Err  error
)

// Default search paths for the GeoLite2-City database file (disk fallback).
var geolite2Paths = []string{
	"data/GeoLite2-City.mmdb",
	"GeoLite2-City.mmdb",
	"/usr/share/GeoIP/GeoLite2-City.mmdb",
	"/var/lib/GeoIP/GeoLite2-City.mmdb",
}

// CityLocation holds GeoLite2 city-level geolocation data.
type CityLocation struct {
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	City        string `json:"city"`
	Flag        string `json:"flag"`
}

// InitGeoLite2 opens the GeoLite2-City DB. Resolution: embedded
// (-tags geolite2) → path arg → GEOLITE2_DB → default fs paths.
// Idempotent.
func InitGeoLite2(path string) error {
	geolite2Once.Do(func() {
		// 1. Try embedded database (compiled in with -tags geolite2)
		if len(embeddedGeoLite2) > 0 {
			geolite2DB, geolite2Err = geoip2.FromBytes(embeddedGeoLite2)
			if geolite2Err == nil {
				log.Info("GeoLite2 database loaded from embedded binary")
				return
			}
			log.Warnf("Failed to load embedded GeoLite2 database: %v", geolite2Err)
		}

		// 2. Fall back to filesystem
		paths := geolite2Paths
		if path != "" {
			paths = append([]string{path}, paths...)
		}
		if envPath := os.Getenv("GEOLITE2_DB"); envPath != "" {
			paths = append([]string{envPath}, paths...)
		}

		for _, p := range paths {
			if _, err := os.Stat(p); err != nil {
				continue
			}
			geolite2DB, geolite2Err = geoip2.Open(p)
			if geolite2Err == nil {
				log.Infof("GeoLite2 database loaded from %s", p)
				return
			}
			log.Warnf("Failed to open GeoLite2 database at %s: %v", p, geolite2Err)
		}

		geolite2Err = fmt.Errorf("GeoLite2-City.mmdb not found (build with -tags geolite2 to embed, or place on disk)")
	})
	return geolite2Err
}

// GeoLite2Available returns true if the database was loaded successfully.
func GeoLite2Available() bool {
	_ = InitGeoLite2("") // lazy init
	return geolite2DB != nil
}

// LookupCity resolves an IP address to city-level geolocation using GeoLite2.
func LookupCity(ipStr string) (*CityLocation, error) {
	_ = InitGeoLite2("") // lazy init (no-op after first call)
	if geolite2DB == nil {
		return nil, geolite2Err
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ipStr)
	}

	record, err := geolite2DB.City(ip)
	if err != nil {
		return nil, err
	}

	loc := &CityLocation{
		Country:     record.Country.Names["en"],
		CountryCode: record.Country.IsoCode,
		City:        record.City.Names["en"],
	}

	// Generate flag emoji from 2-letter country code
	if len(loc.CountryCode) == 2 {
		cc := strings.ToUpper(loc.CountryCode)
		loc.Flag = string(rune(cc[0])+127397) + string(rune(cc[1])+127397)
	}

	return loc, nil
}

// LookupCityBatch resolves multiple IPs. IPs that fail to resolve are silently skipped.
func LookupCityBatch(ips []string) map[string]*CityLocation {
	results := make(map[string]*CityLocation, len(ips))
	for _, ip := range ips {
		if loc, err := LookupCity(ip); err == nil {
			results[ip] = loc
		}
	}
	return results
}
