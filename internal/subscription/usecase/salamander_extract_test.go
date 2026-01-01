package usecase

import (
	"encoding/json"
	"testing"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

func TestExtractSalamanderPassword(t *testing.T) {
	cases := []struct {
		name string
		fm   *nodeDomain.FinalMask
		want string
	}{
		{"nil", nil, ""},
		{"array shape", &nodeDomain.FinalMask{
			UDP: json.RawMessage(`[{"type":"salamander","settings":{"password":"NasNet1234"}}]`),
		}, "NasNet1234"},
		{"legacy object shape", &nodeDomain.FinalMask{
			UDP: json.RawMessage(`{"type":"salamander","settings":{"password":"pw2"}}`),
		}, "pw2"},
		{"non-salamander mask", &nodeDomain.FinalMask{
			UDP: json.RawMessage(`[{"type":"noise","settings":{"packet":"x"}}]`),
		}, ""},
		{"salamander among others", &nodeDomain.FinalMask{
			UDP: json.RawMessage(`[{"type":"noise"},{"type":"salamander","settings":{"password":"pw3"}}]`),
		}, "pw3"},
		{"empty udp", &nodeDomain.FinalMask{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractSalamanderPassword(c.fm); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
