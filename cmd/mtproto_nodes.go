package cmd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
	"github.com/nasnet-community/mtproto-remote-plugin/tgnet"
	"github.com/nasnet-community/nasnet-panel-linux/internal/mtprotonode"
	networkDomain "github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	networkUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeRepository "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"

	nodeUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
)

// mtprotoDeps is everything the MTProto node manager is built from.
type mtprotoDeps struct {
	DB         *gorm.DB
	Nodes      nodeUsecase.NodeUsecase
	Network    networkUsecase.NetworkUsecase // nil off router mode
	Bus        *events.EventBus
	HTTP       *httpclient.Factory
	Setting    func(ctx context.Context, key string) (string, error)
	Reconcile  func(ctx context.Context, nodeID uint) error
	RouterMode bool
	Log        *logrus.Logger
}

// liteNodeAdmin manages an MTProto node's nodes row; it has no gRPC agent to call.
type liteNodeAdmin struct {
	repo    nodeRepository.NodeRepository
	uc      nodeUsecase.NodeUsecase
	records *mtprotonode.RecordStore
}

func (a liteNodeAdmin) CreateNode(ctx context.Context, name, host string) (uint, error) {
	n := &nodeDomain.Node{
		Name: name, IP: host, Kind: nodeDomain.NodeKindLite, ConnectMode: nodeDomain.ConnectModeLink,
		IsActive: true,
	}
	if err := a.repo.CreateNode(ctx, n); err != nil {
		return 0, err
	}
	a.records.Installing(n.ID)
	return n.ID, nil
}

// DeleteNode forces the delete: the MTProto node service has already dealt with the agent.
func (a liteNodeAdmin) DeleteNode(ctx context.Context, id uint) error {
	// on a removal the record store took the row first
	if _, err := a.uc.GetNode(ctx, id); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return a.uc.DeleteNode(nodeUsecase.FromMTProtoService(ctx), id, true)
}

// mtprotoRecords deletes a node's row before its MTProto record, so a failed delete leaves it removable.
func mtprotoRecords(s mtprotonode.Store, repo nodeRepository.NodeRepository, uc nodeUsecase.NodeUsecase) (*mtprotonode.RecordStore, liteNodeAdmin) {
	records := mtprotonode.NewRecordStore(s)
	admin := liteNodeAdmin{repo: repo, uc: uc, records: records}
	records.BeforeDelete(admin.DeleteNode)
	return records, admin
}

// SetHost is the guarded address move's way in: plain node edits can't change it.
func (a liteNodeAdmin) SetHost(ctx context.Context, id uint, host string) error {
	return a.repo.SetNodeIP(ctx, id, host)
}

func (a liteNodeAdmin) Host(ctx context.Context, id uint) (string, error) {
	n, err := a.uc.GetNode(ctx, id)
	if err != nil {
		return "", err
	}
	return n.IP, nil
}

// newMTProtoAPI builds the shared install/ops service and the HTTP handler over it.
func newMTProtoAPI(d mtprotoDeps, repo nodeRepository.NodeRepository, m *mtprotonode.Manager, allow *tgnet.Refresher) *mtprotonode.Handler {
	dir := os.Getenv("MTPROTO_BIN_DIR")
	if dir == "" {
		dir = os.Getenv("RELAY_BIN_DIR") // the old name, so existing setups keep working
	}
	if dir == "" {
		dir = "./data/relay-bins" // already holds downloads; renaming would refetch them
	}
	bins := relayops.NewBinaries(dir, func() *http.Client {
		return d.HTTP.LiveClient(httpclient.FeatureCoreBinaries, httpclient.EgressForeign, 10*time.Minute)
	})
	// dev only: until an agent release is pinned, install a local build from here
	if local := os.Getenv("NASNET_MTPROTO_AGENT_DIR"); local != "" {
		bins.LocalAgents = local
		if d.Log != nil {
			d.Log.Warnf("MTProto nodes: installing agent builds from %s while no agent release is pinned", local)
		}
	}
	store, admin := mtprotoRecords(mtprotonode.NewStore(d.DB), repo, d.Nodes)
	svc := relayops.NewService(relayops.ServiceConfig{
		Store: store, Links: m.Links(), Bins: bins, SSH: relayops.NewSSH(),
		Admin: admin,
		Dial:  m.SSHDial(), // names resolve through the system resolver, as for the link
	})
	return mtprotonode.NewHandler(svc, m, allow)
}

// newMTProtoNodes builds the MTProto node manager and its allowlist. Nothing runs until Start.
func newMTProtoNodes(d mtprotoDeps) (*mtprotonode.Manager, *tgnet.Refresher, error) {
	allow, err := tgnet.NewRefresher()
	if err != nil {
		return nil, nil, err
	}
	// unset refreshes from Telegram's own list; "off" keeps only the built-in one
	allow.URL = func() string {
		v, err := d.Setting(context.Background(), "relay_cidr_url")
		if err != nil {
			v = ""
		}
		return tgnet.ListURL(v)
	}
	// the list is fetched through the foreign exit, on every run
	allow.Client = func() *http.Client {
		return d.HTTP.ClientFor(httpclient.FeatureGeofiles, httpclient.EgressForeign, 30*time.Second)
	}

	var wans mtprotonode.WANSource
	if d.Network != nil {
		wans = domesticWANs{uc: d.Network}
	}
	m := mtprotonode.NewManager(mtprotonode.Config{
		Store: mtprotonode.NewStore(d.DB), Nodes: d.Nodes, WANs: wans,
		Dialers: mtprotonode.NewDialers(d.RouterMode), Allow: allow, Bus: d.Bus,
		Reconcile: d.Reconcile, Log: d.Log, Ranges: localRanges,
	})
	allow.Changed = m.PrefixesChanged
	return m, allow, nil
}

// domesticWANs lists the domestic uplinks, with the pin each is bound by.
type domesticWANs struct{ uc networkUsecase.NetworkUsecase }

func (d domesticWANs) DomesticWANs(ctx context.Context) ([]mtprotonode.WAN, error) {
	h, err := d.uc.HealthState(ctx)
	if err != nil {
		return nil, err
	}
	var out []mtprotonode.WAN
	for _, u := range h.Uplinks {
		slot := networkDomain.UplinkSlot(u.Slot)
		if !slot.IsDomestic() || u.IfName == "" {
			continue
		}
		out = append(out, mtprotonode.WAN{Slot: u.Slot, IfName: u.IfName, Pin: networkUsecase.UplinkIndex(slot)})
	}
	return out, nil
}

// localRanges is decoded on first use: a large file most panels never need.
var localRanges = sync.OnceValue(func() mtprotonode.Ranges {
	cs, err := geoip.EmbeddedCIDRSet(networkUsecase.DomesticCountry)
	if err != nil {
		return nil
	}
	return mtprotonode.PrefixRanges(cs.V4, cs.V6)
})
