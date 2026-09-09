package domain

// XrayLogSettings is a partial update. Pointers distinguish an explicit empty
// path or disabled DNS logging from an omitted setting.
type XrayLogSettings struct {
	Level  *string `json:"loglevel"`
	Access *string `json:"access"`
	Error  *string `json:"error"`
	DNS    *bool   `json:"dnsLog"`
}
