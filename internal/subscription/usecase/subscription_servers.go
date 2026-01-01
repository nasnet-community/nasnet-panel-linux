package usecase

import (
	"context"
	"strings"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/cache"
	xrayProvider "github.com/nasnet-community/nasnet-panel-linux/pkg/product/xray"
)

// SubServer is one config-bearing server for a subscription, used by the bot's
// per-server view.
type SubServer struct {
	InboundID   uint
	Name        string
	CountryCode string
	Protocol    string
	Online      bool
	Link        string
}

// GetSubscriptionServers returns the sub's xray servers
func (u *subscriptionUsecase) GetSubscriptionServers(ctx context.Context, subID uint) ([]SubServer, error) {
	sub, err := u.subRepo.FindByID(ctx, subID)
	if err != nil {
		return nil, err
	}

	// Per-inbound account metadata: UUID (link credential, may differ from the
	// sub's ConfigID after key regen) + email (for per-server online lookup).
	type acctMeta struct{ uuid, email string }
	accMap := map[uint]acctMeta{}
	var accounts []*accountDomain.Account
	if u.accountManager != nil {
		if accs, aerr := u.accountManager.ListAccountsBySubscription(ctx, subID); aerr == nil {
			accounts = accs
			for _, a := range accs {
				accMap[a.InboundID] = acctMeta{uuid: a.UUID, email: a.Email}
			}
		}
	}

	var out []SubServer
	seen := map[uint]bool{}
	add := func(in *nodeDomain.Inbound) {
		if in == nil || in.IsDisabled || in.Node == nil || !in.Node.IsActive || seen[in.ID] {
			return
		}
		if strings.EqualFold(in.Protocol, "wireguard") {
			return
		}
		seen[in.ID] = true

		detail := u.buildInboundDetail(ctx, in)
		uuid, email := sub.ConfigID, sub.ConfigEmail
		if m, ok := accMap[in.ID]; ok {
			if m.uuid != "" {
				uuid = m.uuid
			}
			if m.email != "" {
				email = m.email
			}
		}
		out = append(out, SubServer{
			InboundID:   in.ID,
			Name:        detail.Remark,
			CountryCode: detail.CountryCode,
			Protocol:    strings.ToUpper(in.Protocol),
			Online:      cache.IsOnlineOnNode(email, in.Node.ID),
			Link:        xrayProvider.ServerLink(detail, uuid, detail.NodeIP),
		})
	}

	for _, a := range accounts {
		add(a.Inbound)
	}
	return out, nil
}
