package embedded

import _ "embed"

// IranGeoIP contains the chocolate4u/Iran-v2ray-rules geoip.dat,
// embedded at build time.
//
//go:embed geoip.dat
var IranGeoIP []byte

// IranGeoSite contains the chocolate4u/Iran-v2ray-rules geosite.dat,
// embedded at build time.
//
//go:embed geosite.dat
var IranGeoSite []byte
