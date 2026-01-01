//go:build !geolite2

package geoip

// embeddedGeoLite2 is nil when built without the geolite2 build tag.
// To embed the database into the binary, place GeoLite2-City.mmdb in
// pkg/geoip/ and build with: go build -tags geolite2
var embeddedGeoLite2 []byte
