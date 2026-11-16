// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/backup"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
)

// The outbounds, the routes through them and the check's address are part of
// the state, and a backup brings them back, logins and counter included.
func TestABackupBringsBackTheOutboundsAndTheRoutes(t *testing.T) {
	ctx := context.Background()
	a, cfgA, _ := anInstall(t)
	f := addOutbound(t, cfgA.Store, "Frankfurt", realityOut())
	gone := addOutbound(t, cfgA.Store, "Lyon", wsOut())
	p := addOutbound(t, cfgA.Store, "Paris", trojanOut())
	route(t, cfgA.Store, 2, f.ID)
	must(t, cfgA.Store.Update(func(s *model.State) error {
		s.Local.OutboundID, s.ProbeURL = p.ID, "https://example.com/204"
		return s.RemoveOutbound(gone.ID)
	}))
	_, file, err := a.ExportBackup(ctx, backupPass)
	must(t, err)

	cfgB, _ := baseConfig(t)
	b, err := New(cfgB)
	must(t, err)
	_, err = b.ImportBackup(ctx, backupPass, file)
	must(t, err)
	if got, want := encoded(t, cfgB.Store), encoded(t, cfgA.Store); !bytes.Equal(got, want) {
		t.Errorf("the restored state differs from the one exported:\n%s\n---\n%s", got, want)
	}
	st := cfgB.Store.Read()
	if len(st.Outbounds) != 2 || st.NextOutboundID != 4 || st.FindNode(2).OutboundID != f.ID || st.Local.OutboundID != p.ID ||
		st.ProbeURL != "https://example.com/204" || st.FindOutbound(p.ID).Settings.Password != outPassword || st.FindOutbound(f.ID).Settings.ID != outUUID {
		t.Errorf("restored outbounds %+v, counter %d, check %q", st.Outbounds, st.NextOutboundID, st.ProbeURL)
	}
}

// A file with a route to an outbound it doesn't hold is refused whole, as one
// with a client on a node it doesn't hold is: the state it would make can't be
// valid, and guessing which way the node should leave is not the hub's to do.
func TestABackupWithARouteToNoOutboundIsRefused(t *testing.T) {
	ctx := context.Background()
	a, cfgA, _ := anInstall(t)
	_, _, err := a.ExportBackup(ctx, backupPass) // makes the CA's file whole
	must(t, err)
	bad := cfgA.Store.Read()
	bad.FindNode(1).OutboundID = 7
	snap, err := model.Encode(bad)
	must(t, err)
	file, err := backup.Export(snap, backupPass)
	must(t, err)

	cfgB, _ := baseConfig(t)
	b, err := New(cfgB)
	must(t, err)
	was := encoded(t, cfgB.Store)
	_, err = b.ImportBackup(ctx, backupPass, file)
	if !errors.Is(err, store.ErrBadSnapshot) {
		t.Fatalf("a route to no outbound: %v", err)
	}
	if !bytes.Equal(encoded(t, cfgB.Store), was) {
		t.Error("a refused backup changed the install")
	}
	// and the same for the router's entry
	bad = cfgA.Store.Read()
	bad.Local.OutboundID = 7
	snap, _ = model.Encode(bad)
	file, _ = backup.Export(snap, backupPass)
	if _, err := b.ImportBackup(ctx, backupPass, file); !errors.Is(err, store.ErrBadSnapshot) {
		t.Fatalf("the router's route to no outbound: %v", err)
	}
}
