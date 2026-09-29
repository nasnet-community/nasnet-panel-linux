package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	mtprotoproto "github.com/nasnet-community/nasnet-panel-linux/internal/core/protocol/mtproto"
	sshproto "github.com/nasnet-community/nasnet-panel-linux/internal/core/protocol/ssh"
)

// ClientArtifact is a rendered connection config for one core_user.
type ClientArtifact struct {
	Protocol string `json:"protocol"`
	Kind     string `json:"kind"`    // always "link"
	Content  string `json:"content"` // link URIs, one per line
	// NodeName labels which node this artifact belongs to (for multi-node
	// subscription file lists). Best-effort: the node's display name, else its
	// host, else "".
	NodeName string `json:"node_name"`
	// DataUsed is this tunnel's consumed bytes (core_user.DataUsed) — surfaced
	// per-card on the public sub panel. Display-only.
	DataUsed int64 `json:"data_used"`
}

// prepareInboundSettings fills protocol-specific server-side settings that must
// be generated at create time (e.g. an SSH host key).
func prepareInboundSettings(in *domain.CoreInbound) error {
	switch in.Protocol {
	case domain.CoreProtoSSH:
		st := map[string]any{}
		if len(in.Settings) > 0 {
			if err := json.Unmarshal(in.Settings, &st); err != nil {
				return fmt.Errorf("core: bad ssh settings: %w", err)
			}
		}
		if hk, _ := st["host_key"].(string); hk == "" {
			pemStr, fp, err := sshproto.GenerateHostKeyPEM()
			if err != nil {
				return err
			}
			st["host_key"] = pemStr
			st["host_key_fingerprint"] = fp
		}
		raw, err := json.Marshal(st)
		if err != nil {
			return err
		}
		in.Settings = raw
	case domain.CoreProtoMTProto:
		if err := prepareMTProtoSettings(in); err != nil {
			return err
		}
	}
	return nil
}

// prepareMTProtoSettings validates and normalizes an mtproto inbound's settings
// so the hub never persists a combination that would leave the tunnel dead or a
// setting the relay silently ignores.
func prepareMTProtoSettings(in *domain.CoreInbound) error {
	s, err := mtprotoproto.ParseSettings(in.Settings)
	if err != nil {
		return err
	}
	if !s.Modes.Any() {
		return fmt.Errorf("core: mtproto needs at least one camouflage mode enabled")
	}
	// FakeTLS encodes the SNI into the client secret, so without a domain the
	// mode simply is not offered (EffectiveModes drops it, and the renderer
	// follows). That is only fatal when it leaves nothing to offer at all.
	if !s.EffectiveModes().Any() {
		return fmt.Errorf("core: mtproto FakeTLS is the only enabled mode but no TLS SNI domain is set")
	}
	// An ad tag is delivered through Telegram's middle proxy; with direct egress
	// the relay ignores it, so persisting one is a silent no-op.
	if s.AdtagDefault != "" && !s.MiddleProxy {
		return fmt.Errorf("core: mtproto ad tag requires the Telegram middle proxy to be enabled")
	}
	if lm := strings.ToLower(strings.TrimSpace(s.LinkMode)); lm != "" && lm != "auto" {
		switch mtprotoproto.Mode(lm) {
		case mtprotoproto.ModeClassic, mtprotoproto.ModeSecure, mtprotoproto.ModeTLS:
		default:
			return fmt.Errorf("core: mtproto link_mode must be auto, classic, secure or tls")
		}
	}
	if ll := strings.ToLower(strings.TrimSpace(s.LogLevel)); ll != "" {
		switch ll {
		case "debug", "verbose", "normal", "silent":
		default:
			return fmt.Errorf("core: mtproto log_level must be debug, verbose, normal or silent")
		}
	}
	return nil
}

// CreateUser validates and persists a core_user, filling defaults (auto
// username/secret, k_limit>=1, enabled).
func (u *CoreUsecase) CreateUser(ctx context.Context, cu *domain.CoreUser) error {
	in, err := u.repo.FindInboundByID(ctx, cu.CoreInboundID)
	if err != nil {
		return fmt.Errorf("core: unknown inbound %d: %w", cu.CoreInboundID, err)
	}
	if cu.Username == "" {
		cu.Username = "u" + randToken(6)
	}
	if cu.Secret == "" {
		cu.Secret = randToken(16)
	}
	if err := normalizeUserSecret(in, cu); err != nil {
		return err
	}
	if cu.KLimit < 1 {
		cu.KLimit = 1
	}
	if err := u.ensureUniqueUser(ctx, cu); err != nil {
		return err
	}
	cu.Enabled = true
	return u.repo.CreateUser(ctx, cu)
}

// normalizeUserSecret checks a secret against the inbound's protocol. An
// MTProto secret is 16 raw bytes: a client-facing dd/ee form is stripped to
// that, anything else is refused (telemt rejects the whole config over one bad
// secret).
func normalizeUserSecret(in *domain.CoreInbound, cu *domain.CoreUser) error {
	if in.Protocol != domain.CoreProtoMTProto {
		return nil
	}
	secret, err := mtprotoproto.NormalizeSecret(cu.Secret)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSecret, err)
	}
	cu.Secret = secret
	return nil
}

// GenerateUserConfig renders the native client artifact for a core_user. host is
// the node's public address (resolved by the caller).
func (u *CoreUsecase) GenerateUserConfig(ctx context.Context, userID uint, host string) (*ClientArtifact, error) {
	cu, err := u.repo.FindUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("core: user %d: %w", userID, err)
	}
	in, err := u.repo.FindInboundByID(ctx, cu.CoreInboundID)
	if err != nil {
		return nil, fmt.Errorf("core: inbound %d: %w", cu.CoreInboundID, err)
	}
	switch in.Protocol {
	case domain.CoreProtoSSH:
		link := sshproto.BuildShareLink(host, in.Port, cu.Username, cu.Secret, in.Remark)
		return &ClientArtifact{Protocol: string(in.Protocol), Kind: "link", Content: link}, nil
	case domain.CoreProtoMTProto:
		// The client secret encodes the camouflage mode (classic / dd / ee+SNI),
		// so it must be derived from the inbound's enabled modes — a hardcoded
		// classic link silently fails on a relay with classic disabled.
		st, err := mtprotoproto.ParseSettings(in.Settings)
		if err != nil {
			return nil, err
		}
		links, err := mtprotoproto.BuildLinks(host, in.Port, cu.Secret, st)
		if err != nil {
			return nil, fmt.Errorf("core: mtproto inbound %d: %w", in.ID, err)
		}
		uris := make([]string, 0, len(links))
		for _, l := range links {
			uris = append(uris, l.URI)
		}
		return &ClientArtifact{
			Protocol: string(in.Protocol), Kind: "link",
			Content: strings.Join(uris, "\n"),
		}, nil
	default:
		return nil, fmt.Errorf("core: client config not implemented for protocol %q", in.Protocol)
	}
}

func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should never fail; degrade to a fixed-length marker.
		// (strings.Repeat, not a sliced literal: n*2 overran the literal for
		// n >= 8 and panicked.)
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}
