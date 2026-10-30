// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg

// Structs rather than maps so the keys come out in one fixed order.

type config struct {
	Log       logConfig     `json:"log"`
	API       apiConfig     `json:"api"`
	Routing   routingConfig `json:"routing"`
	Policy    policyConfig  `json:"policy"`
	Inbounds  []inbound     `json:"inbounds"`
	Outbounds []outbound    `json:"outbounds"`
	Stats     struct{}      `json:"stats"`
}

type logConfig struct {
	LogLevel string `json:"loglevel"`
	Access   string `json:"access"`
	DNSLog   bool   `json:"dnsLog"`
}

type apiConfig struct {
	Tag      string   `json:"tag"`
	Services []string `json:"services"`
}

type routingConfig struct {
	DomainStrategy string `json:"domainStrategy"`
	Rules          []rule `json:"rules"`
}

type rule struct {
	Type        string   `json:"type"`
	RuleTag     string   `json:"ruleTag,omitempty"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	IP          []string `json:"ip,omitempty"`
	OutboundTag string   `json:"outboundTag"`
}

type policyConfig struct {
	Levels levels       `json:"levels"`
	System systemPolicy `json:"system"`
}

type levels struct {
	Zero userLevel `json:"0"`
}

type userLevel struct {
	Uplink   bool `json:"statsUserUplink"`
	Downlink bool `json:"statsUserDownlink"`
	Online   bool `json:"statsUserOnline"`
}

type systemPolicy struct {
	InboundUplink    bool `json:"statsInboundUplink"`
	InboundDownlink  bool `json:"statsInboundDownlink"`
	OutboundUplink   bool `json:"statsOutboundUplink"`
	OutboundDownlink bool `json:"statsOutboundDownlink"`
}

type inbound struct {
	Tag            string    `json:"tag"`
	Listen         string    `json:"listen"`
	Port           int       `json:"port"`
	Protocol       string    `json:"protocol"`
	Settings       any       `json:"settings"`
	StreamSettings *stream   `json:"streamSettings,omitempty"`
	Sniffing       *sniffing `json:"sniffing,omitempty"`
}

type dokodemoSettings struct {
	Address string `json:"address"`
}

type vlessSettings struct {
	Clients    []vlessClient `json:"clients"`
	Decryption string        `json:"decryption"`
}

type vlessClient struct {
	ID      string   `json:"id"`
	Flow    string   `json:"flow"`
	Email   string   `json:"email"`
	Level   int      `json:"level"`
	Reverse *reverse `json:"reverse,omitempty"`
}

type reverse struct {
	Tag string `json:"tag"`
}

type stream struct {
	Network  string  `json:"network"`
	Security string  `json:"security"`
	Reality  reality `json:"realitySettings"`
}

type reality struct {
	Show        bool     `json:"show"`
	Dest        string   `json:"dest"`
	ServerNames []string `json:"serverNames"`
	PrivateKey  string   `json:"privateKey"`
	ShortIDs    []string `json:"shortIds"`
}

type sniffing struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"`
	MetadataOnly bool     `json:"metadataOnly"`
	RouteOnly    bool     `json:"routeOnly"`
}

type outbound struct {
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"`
	Settings any    `json:"settings"`
}

type blackholeSettings struct {
	Response struct {
		Type string `json:"type"`
	} `json:"response"`
}

const (
	apiTag     = "api"
	directTag  = "direct"
	blockedTag = "blocked"
)

// assemble lays out the config for an input that has passed validate.
func assemble(in Input, domestic []string) config {
	clients := make([]vlessClient, 0, len(in.Clients)+1)
	for _, c := range in.Clients {
		clients = append(clients, vlessClient{ID: c.UUID, Flow: Flow, Email: c.Email})
	}
	// Last, as in the panel. Only this client carries a reverse tag; one set on
	// the inbound's metadata (xcoreHub) would also go to users added later.
	clients = append(clients, vlessClient{
		ID: in.Bridge.UUID, Flow: Flow, Email: in.Bridge.Email,
		Reverse: &reverse{Tag: in.Bridge.ReverseTag},
	})

	return config{
		Log: logConfig{
			LogLevel: in.logLevel(),
			// The node has no use for a line per connection, and it would hold
			// every subscriber's destinations.
			Access: "none",
		},
		API: apiConfig{
			Tag:      apiTag,
			Services: []string{"HandlerService", "LoggerService", "StatsService", "RoutingService"},
		},
		Routing: routingConfig{
			// The rules match addresses, so a domain has to be resolved before
			// they run. With IPIfNonMatch the last rule would match first, on
			// the unresolved name, and send domestic domains into the tunnel.
			// A name that doesn't resolve falls through to it, which is the
			// right place: the other end resolves it again.
			DomainStrategy: "IPOnDemand",
			Rules: []rule{
				{Type: "field", InboundTag: []string{apiTag}, OutboundTag: apiTag},
				{Type: "field", RuleTag: "block-private", IP: privateRanges, OutboundTag: blockedTag},
				{Type: "field", RuleTag: "domestic-direct", IP: domestic, OutboundTag: directTag},
				{Type: "field", RuleTag: "to-tunnel", InboundTag: []string{EntryTag}, OutboundTag: in.Bridge.ReverseTag},
			},
		},
		Policy: policyConfig{
			// Every client is level 0.
			Levels: levels{Zero: userLevel{Uplink: true, Downlink: true, Online: true}},
			System: systemPolicy{InboundUplink: true, InboundDownlink: true, OutboundUplink: true, OutboundDownlink: true},
		},
		Inbounds: []inbound{
			{
				Tag: apiTag, Listen: "127.0.0.1", Port: in.apiPort(), Protocol: "dokodemo-door",
				Settings: dokodemoSettings{Address: "127.0.0.1"},
			},
			{
				Tag: EntryTag, Listen: "0.0.0.0", Port: in.EntryPort, Protocol: "vless",
				Settings: vlessSettings{Clients: clients, Decryption: "none"},
				StreamSettings: &stream{
					Network:  "tcp",
					Security: "reality",
					Reality: reality{
						Dest:        in.RealityDest,
						ServerNames: in.ServerNames,
						PrivateKey:  in.PrivateKey,
						ShortIDs:    in.ShortIDs,
					},
				},
				Sniffing: &sniffing{Enabled: true, DestOverride: []string{"http", "tls"}},
			},
		},
		// Xray falls back to the first outbound when no rule matches, so
		// blocked leads: nothing leaves the node unless a rule says so.
		Outbounds: []outbound{
			{Tag: blockedTag, Protocol: "blackhole", Settings: newBlackholeSettings()},
			{Tag: directTag, Protocol: "freedom", Settings: struct{}{}},
		},
	}
}

func newBlackholeSettings() blackholeSettings {
	var s blackholeSettings
	s.Response.Type = "none"
	return s
}
