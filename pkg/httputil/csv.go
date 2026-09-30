package httputil

// CSVText neutralises a free-text CSV cell a spreadsheet would run as a
// formula (one starting with = + - @, tab or CR) by prefixing a quote.
// Customer-controlled values such as Telegram names reach admin exports.
func CSVText(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}
