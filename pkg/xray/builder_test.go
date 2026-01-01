package xray

import (
	"testing"

	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	vlessOutbound "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/proxy/vmess"
	vmessOutbound "github.com/xtls/xray-core/proxy/vmess/outbound"
)

func TestBuildOutboundVLESS(t *testing.T) {
	cfg := &OutboundConfig{
		Tag:        "vless_test",
		Protocol:   "vless",
		Address:    "example.com",
		Port:       443,
		UUID:       "00000000-0000-0000-0000-000000000000",
		Flow:       "xtls-rprx-vision",
		Level:      1,
		Email:      "user@example.com",
		Encryption: "none",
		Network:    "tcp",
		Security:   "reality",
		Reality: &OutboundRealityClientConfig{
			ServerName:  "example.com",
			PublicKey:   "publicKey",
			ShortID:     "shortId",
			Fingerprint: "chrome",
		},
	}

	out, err := BuildOutboundHandlerConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to build VLESS config: %v", err)
	}

	if out.Tag != cfg.Tag {
		t.Errorf("Expected tag %s, got %s", cfg.Tag, out.Tag)
	}

	// Verify ProxySettings are VLESS
	msg, err := out.ProxySettings.GetInstance()
	if err != nil {
		t.Fatalf("Failed to unmarshal proxy settings: %v", err)
	}
	vlessConfig, ok := msg.(*vlessOutbound.Config)
	if !ok {
		t.Fatalf("Expected VLESS config, got %T", msg)
	}

	if vlessConfig.Vnext == nil {
		t.Fatalf("Expected Vnext validation")
	}
	server := vlessConfig.Vnext
	if server.Port != cfg.Port {
		t.Errorf("Expected port %d, got %d", cfg.Port, server.Port)
	}

	user := server.User
	if user.Level != cfg.Level {
		t.Errorf("Expected level %d, got %d", cfg.Level, user.Level)
	}
	if user.Email != cfg.Email {
		t.Errorf("Expected email %s, got %s", cfg.Email, user.Email)
	}

	accountMsg, err := user.Account.GetInstance()
	if err != nil {
		t.Fatalf("Failed to unmarshal account: %v", err)
	}
	account, ok := accountMsg.(*vless.Account)
	if !ok {
		t.Fatalf("Expected VLESS account, got %T", accountMsg)
	}

	if account.Id != cfg.UUID {
		t.Errorf("Expected UUID %s, got %s", cfg.UUID, account.Id)
	}
	if account.Flow != cfg.Flow {
		t.Errorf("Expected Flow %s, got %s", cfg.Flow, account.Flow)
	}
	if account.Encryption != cfg.Encryption {
		t.Errorf("Expected Encryption %s, got %s", cfg.Encryption, account.Encryption)
	}
}

func TestBuildOutboundVMess(t *testing.T) {
	cfg := &OutboundConfig{
		Tag:         "vmess_test",
		Protocol:    "vmess",
		Address:     "example.com",
		Port:        443,
		UUID:        "00000000-0000-0000-0000-000000000000",
		Level:       2,
		Email:       "vmess@example.com",
		Experiments: "debug",
		Network:     "ws",
		WS: &WSConfig{
			Path: "/ws",
			Host: "example.com",
		},
	}

	out, err := BuildOutboundHandlerConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to build VMess config: %v", err)
	}

	// Verify VMess
	msg, err := out.ProxySettings.GetInstance()
	if err != nil {
		t.Fatalf("Failed to unmarshal proxy settings: %v", err)
	}
	vmessConfig, ok := msg.(*vmessOutbound.Config)
	if !ok {
		t.Fatalf("Expected VMess config, got %T", msg)
	}

	user := vmessConfig.Receiver.User
	if user.Level != cfg.Level {
		t.Errorf("Expected level %d, got %d", cfg.Level, user.Level)
	}
	if user.Email != cfg.Email {
		t.Errorf("Expected email %s, got %s", cfg.Email, user.Email)
	}

	accountMsg, err := user.Account.GetInstance()
	if err != nil {
		t.Fatalf("Failed to unmarshal account: %v", err)
	}
	account, ok := accountMsg.(*vmess.Account)
	if !ok {
		t.Fatalf("Expected VMess account, got %T", accountMsg)
	}
	if account.TestsEnabled != cfg.Experiments {
		t.Errorf("Expected Experiments %s, got %s", cfg.Experiments, account.TestsEnabled)
	}
}

func TestBuildOutboundTrojan(t *testing.T) {
	cfg := &OutboundConfig{
		Tag:      "trojan_test",
		Protocol: "trojan",
		Address:  "example.com",
		Port:     443,
		Password: "password",
		Level:    3,
		Email:    "trojan@example.com",
	}

	out, err := BuildOutboundHandlerConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to build Trojan config: %v", err)
	}

	msg, err := out.ProxySettings.GetInstance()
	if err != nil {
		t.Fatalf("Failed to unmarshal proxy settings: %v", err)
	}
	trojanConfig, ok := msg.(*trojan.ClientConfig)
	if !ok {
		t.Fatalf("Expected Trojan config, got %T", msg)
	}

	user := trojanConfig.Server.User
	if user.Level != cfg.Level {
		t.Errorf("Expected level %d, got %d", cfg.Level, user.Level)
	}
	if user.Email != cfg.Email {
		t.Errorf("Expected email %s, got %s", cfg.Email, user.Email)
	}
}

func TestBuildOutboundShadowsocks(t *testing.T) {
	cfg := &OutboundConfig{
		Tag:      "ss_test",
		Protocol: "shadowsocks",
		Address:  "example.com",
		Port:     8388,
		Password: "password",
		Method:   "aes-128-gcm",
		Level:    1,
		Email:    "ss@example.com",
		IVCheck:  true,
	}

	out, err := BuildOutboundHandlerConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to build Shadowsocks config: %v", err)
	}

	msg, err := out.ProxySettings.GetInstance()
	if err != nil {
		t.Fatalf("Failed to unmarshal proxy settings: %v", err)
	}
	ssConfig, ok := msg.(*shadowsocks.ClientConfig)
	if !ok {
		t.Fatalf("Expected Shadowsocks config, got %T", msg)
	}

	user := ssConfig.Server.User
	if user.Level != cfg.Level {
		t.Errorf("Expected level %d, got %d", cfg.Level, user.Level)
	}
	if user.Email != cfg.Email {
		t.Errorf("Expected email %s, got %s", cfg.Email, user.Email)
	}

	accountMsg, err := user.Account.GetInstance()
	if err != nil {
		t.Fatalf("Failed to unmarshal account: %v", err)
	}
	account, ok := accountMsg.(*shadowsocks.Account)
	if !ok {
		t.Fatalf("Expected SS account, got %T", accountMsg)
	}
	if !account.IvCheck {
		t.Errorf("Expected IVCheck true, got false")
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *OutboundConfig
		wantErr bool
	}{
		{
			name:    "Empty Protocol",
			cfg:     &OutboundConfig{},
			wantErr: true,
		},
		{
			name: "Missing Address",
			cfg: &OutboundConfig{
				Protocol: "vless",
			},
			wantErr: true,
		},
		{
			name: "Missing UUID for VLESS",
			cfg: &OutboundConfig{
				Protocol: "vless",
				Address:  "example.com",
				Port:     443,
			},
			wantErr: true,
		},
		{
			name: "Valid VLESS",
			cfg: &OutboundConfig{
				Protocol: "vless",
				Address:  "example.com",
				Port:     443,
				UUID:     "uuid",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateOutboundConfig(tt.cfg); (err != nil) != tt.wantErr {
				t.Errorf("ValidateOutboundConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildOutboundFreedom(t *testing.T) {
	cfg := &OutboundConfig{
		Protocol: "freedom",
		Tag:      "freedom-out",
		Sockopt: &SockoptConfig{
			Mark:        100,
			Interface:   "tun1",
			TcpMptcp:    true,
			TcpFastOpen: true,
		},
	}

	handlerConfig, err := BuildOutboundHandlerConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to build outbound config: %v", err)
	}

	if handlerConfig.Tag != "freedom-out" {
		t.Errorf("Expected tag freedom-out, got %s", handlerConfig.Tag)
	}

	senderSettingsRaw, err := handlerConfig.SenderSettings.GetInstance()
	if err != nil {
		t.Fatalf("Failed to unmarshal SenderSettings: %v", err)
	}
	senderConfig, ok := senderSettingsRaw.(*proxyman.SenderConfig)
	if !ok {
		t.Fatalf("Expected *proxyman.SenderConfig, got %T", senderSettingsRaw)
	}

	streamConfig := senderConfig.StreamSettings
	if streamConfig == nil {
		t.Fatal("Expected StreamSettings, got nil")
	}

	if streamConfig.SocketSettings == nil {
		t.Fatal("Expected SocketSettings, got nil")
	}

	if streamConfig.SocketSettings.Mark != 100 {
		t.Errorf("Expected Mark 100, got %d", streamConfig.SocketSettings.Mark)
	}
	if streamConfig.SocketSettings.Interface != "tun1" {
		t.Errorf("Expected Interface tun1, got %s", streamConfig.SocketSettings.Interface)
	}
	if !streamConfig.SocketSettings.TcpMptcp {
		t.Errorf("Expected TcpMptcp true, got false")
	}
	if streamConfig.SocketSettings.Tfo != 1 {
		t.Errorf("Expected Tfo 1 (Enable), got %d", streamConfig.SocketSettings.Tfo)
	}
}
