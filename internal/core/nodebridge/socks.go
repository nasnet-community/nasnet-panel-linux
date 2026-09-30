package nodebridge

import (
	"encoding/json"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	mtprotoproto "github.com/nasnet-community/nasnet-panel-linux/internal/core/protocol/mtproto"
)

// TunnelSocksPort is the loopback port of the Xray SOCKS inbound a tunnel
// with egress=xray leaves through: an MTProto inbound's relay port (settings
// override, else 12300 + id mod 1000; it runs no relay then), and the same
// scheme for SSH. 0 for a direct tunnel, or one without an id yet.
func TunnelSocksPort(in *domain.CoreInbound) int {
	if in.ID == 0 || in.EgressMode != domain.EgressXray {
		return 0
	}
	switch in.Protocol {
	case domain.CoreProtoMTProto:
		if st, err := mtprotoproto.ParseSettings(in.Settings); err == nil {
			relay, _ := st.LoopbackPorts(in.ID)
			return relay
		}
		return 0
	case domain.CoreProtoSSH:
		return mtprotoproto.RelayPortBase + int(in.ID%1000)
	}
	return 0
}

// withSocksAddr hands an SSH tunnel with egress=xray the address of its SOCKS
// inbound. The agent's MTProto core works the port out itself.
func withSocksAddr(in *domain.CoreInbound) (json.RawMessage, error) {
	port := TunnelSocksPort(in)
	if port == 0 || in.Protocol != domain.CoreProtoSSH {
		return in.Settings, nil
	}
	m := map[string]json.RawMessage{}
	if len(in.Settings) > 0 {
		if err := json.Unmarshal(in.Settings, &m); err != nil {
			return nil, fmt.Errorf("inbound %d settings: %w", in.ID, err)
		}
	}
	if m == nil { // settings were JSON null
		m = map[string]json.RawMessage{}
	}
	addr, _ := json.Marshal(fmt.Sprintf("127.0.0.1:%d", port))
	m["socks_addr"] = addr
	return json.Marshal(m)
}
