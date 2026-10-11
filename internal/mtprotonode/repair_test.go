package mtprotonode

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"
	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
	"github.com/nasnet-community/mtproto-remote-plugin/relayops/relaytest"
	"github.com/nasnet-community/mtproto-remote-plugin/release"
)

// The panel lost track of a cover change: its record names a site the agent no longer answers to.
// A repair adopts the box's site, and the running link must dial it from then on.
func TestARepairThatAdoptsTheBoxsCoverSiteMovesTheLinkToIt(t *testing.T) {
	k := newKeyAgent(t)
	db := sharedDB(t)
	st := NewStore(db)
	stale := k.row
	stale.Disguise = "stale.example.com"
	if err := st.Insert(context.Background(), &stale); err != nil {
		t.Fatal(err)
	}
	m, _, _ := panelOver(t, st, plainDialers(), time.Second)
	pub, _ := hex.DecodeString(k.row.RealityPub)
	sid, _ := hex.DecodeString(k.row.ShortID)
	var sid8 [8]byte
	copy(sid8[:], sid)
	ssh := &relaytest.SSH{FP: "SHA256:abc", Arch_: release.AMD64, Result: &installer.Result{
		AgentID: "agent-1", HubPort: k.row.HubPort, CapturePort: k.row.CapturePort, Domain: "example.com",
		Creds: installer.Creds{Pub: pub, ShortID: sid8, Token: k.token},
	}}
	svc := relayops.NewService(relayops.ServiceConfig{
		Store: NewRecordStore(st), Links: m.Links(), SSH: ssh, Bins: relaytest.Bins{},
		Admin: &relaytest.Admin{Hosts: map[uint]string{1: "127.0.0.1"}}, LinkWait: 5 * time.Second,
	})
	time.Sleep(300 * time.Millisecond)
	if linkUp(m) {
		t.Fatal("the stale cover name got through: the test proves nothing")
	}

	ctx := context.Background()
	jid, err := svc.StartRepair(ctx, 1, relayops.Login{Password: "pw", HostKey: "SHA256:abc"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if v := waitJobDone(t, svc, jid); v.State != relayops.JobOK {
		t.Fatalf("repair = %+v", v)
	}
	if !linkUp(m) {
		t.Fatal("the link is not up on the adopted cover site")
	}
	if r, _ := st.Get(ctx, 1); r.Disguise != "example.com" {
		t.Fatalf("the record still names %q", r.Disguise)
	}
	if v, _ := m.View(1); v.Lite.Disguise != "example.com" {
		t.Fatalf("the running node still has %q", v.Lite.Disguise)
	}

	// the tunnel names the site the box runs: nothing to check, the push keeps it
	k.ra.set(func(a *mtprotoAgent) { a.checked = nil })
	p := payload(u("alice", "alice", true))
	p.Settings = []byte(`{"tls_domain":"example.com"}`)
	if err := m.Pusher(1).ApplyCoreState(ctx, p); err != nil {
		t.Fatal(err)
	}
	if st := k.ra.lastState(); st.Disguise != "example.com" || len(k.ra.checked) != 0 {
		t.Fatalf("pushed %q after %d checks", st.Disguise, len(k.ra.checked))
	}
	// a tunnel still on the old site goes through the same check as any cover change; failing it, the box stays put
	k.ra.set(func(a *mtprotoAgent) { a.checkOK = false })
	p.Settings = []byte(`{"tls_domain":"stale.example.com"}`)
	if err := m.Pusher(1).ApplyCoreState(ctx, p); err == nil {
		t.Fatal("a cover site that failed its check was accepted")
	}
	if st := k.ra.lastState(); st.Disguise != "example.com" || len(k.ra.checked) != 1 {
		t.Fatalf("pushed %q after %d checks, want the box's site kept after one check", st.Disguise, len(k.ra.checked))
	}
	if v, _ := m.View(1); v.Lite.Disguise != "example.com" || v.CoverPending == "" || !linkUp(m) {
		t.Fatalf("after the failed check: disguise %q, pending %q, link up %v", v.Lite.Disguise, v.CoverPending, linkUp(m))
	}
}
