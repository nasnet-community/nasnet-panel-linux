package product

import (
	"strings"
	"testing"
)

// All placeholders should be substituted; unknown {var} sequences should
// survive verbatim so admins notice their typo in the rendered output.
func TestRenderRemark(t *testing.T) {
	ctx := RemarkContext{
		Flag:         "🇩🇪",
		Country:      "Germany",
		CountryCode:  "DE",
		Node:         "fra-1",
		Port:         443,
		Protocol:     "vless",
		Network:      "tcp",
		Security:     "tls",
		DataUsed:     "5 GB",
		DataLeft:     "10 GB Left",
		DaysLeft:     "12",
		TimeLeft:     "12d",
		DataLimit:    "50 GB",
		UsagePercent: "45%",
		StatusEmoji:  "🟢",
	}

	got := RenderRemark("{flag} {node}:{port} | {data_left} | {unknown}", ctx)
	want := "🇩🇪 fra-1:443 | 10 GB Left | {unknown}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderRemark_AllPlaceholders(t *testing.T) {
	ctx := RemarkContext{Flag: "F", Country: "C", CountryCode: "CC", Node: "N", Port: 1, Protocol: "P", Network: "NET", Security: "S", DataUsed: "DU", DataLeft: "DL", DaysLeft: "D", TimeLeft: "T", DataLimit: "LIM", UsagePercent: "UP", StatusEmoji: "SE"}
	template := "{flag}|{country}|{country_code}|{node}|{port}|{protocol}|{network}|{security}|{data_used}|{data_left}|{days_left}|{time_left}|{data_limit}|{usage_percent}|{status_emoji}"
	got := RenderRemark(template, ctx)
	if strings.Contains(got, "{") {
		t.Errorf("unsubstituted placeholder remains: %q", got)
	}
}

func TestRenderRemark_DefaultTemplate(t *testing.T) {
	ctx := RemarkContext{Flag: "🇩🇪", Node: "fra-1", DataLeft: "5 GB"}
	got := RenderRemark(DefaultRemarkTemplate, ctx)
	want := "🇩🇪 fra-1 | 5 GB"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
