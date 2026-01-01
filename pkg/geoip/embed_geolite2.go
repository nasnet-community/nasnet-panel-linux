//go:build geolite2

package geoip

import _ "embed"

// Build with `-tags geolite2`; place GeoLite2-City.mmdb in pkg/geoip/ first.
//
//go:embed GeoLite2-City.mmdb
var embeddedGeoLite2 []byte
