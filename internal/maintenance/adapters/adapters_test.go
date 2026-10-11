package adapters

import (
	"context"
	"slices"
	"testing"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	coreDomain "github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// MTProto node users are tunnel users, so their node's notice must reach them too.
func TestSubLinkedNodesIncludeTunnelNodes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&nodeDomain.Inbound{}, &accountDomain.Account{}, &coreDomain.CoreInbound{}, &coreDomain.CoreUser{}); err != nil {
		t.Fatal(err)
	}
	sub, other := uint(3), uint(4)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Create(&nodeDomain.Inbound{ID: 10, NodeID: 1, Tag: "a"}).Error)
	must(db.Create(&accountDomain.Account{InboundID: 10, SubscriptionID: &sub, Email: "x@a", UUID: "u1"}).Error)
	must(db.Create(&coreDomain.CoreInbound{ID: 20, NodeID: 2, Protocol: coreDomain.CoreProtoMTProto, Port: 443}).Error)
	must(db.Create(&coreDomain.CoreUser{CoreInboundID: 20, SubscriptionID: &sub, Username: "m", Email: "x@a"}).Error)
	must(db.Create(&coreDomain.CoreInbound{ID: 21, NodeID: 9, Protocol: coreDomain.CoreProtoSSH, Port: 22}).Error)
	must(db.Create(&coreDomain.CoreUser{CoreInboundID: 21, SubscriptionID: &other, Username: "n", Email: "y@a"}).Error)

	ids, err := (&SubAdapter{DB: db}).GetSubLinkedNodeIDs(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []uint{1, 2}) {
		t.Fatalf("linked nodes = %v, want [1 2]", ids)
	}
}
