// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg_test

import (
	"context"
	"fmt"
	"net"
	"slices"
	"testing"
	"time"

	agentxray "github.com/nasnet-community/nasnet-panel-linux/internal/agent/xray"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

const uuidSub3 = "5a1d9e37-8c42-4b06-91f5-3e7b2c4d8a19"

// Switching a subscription on goes to the agent as AddUser, and a config push
// that only differs by that client is taken by the running Xray through its API
// instead of a restart, which would drop every connection and the tunnel.
func TestAgentAddsASubscriptionWithoutARestart(t *testing.T) {
	before, err := nodecfg.Build(sample())
	if err != nil {
		t.Fatal(err)
	}
	in := sample()
	in.Clients = append(in.Clients, nodecfg.Client{UUID: uuidSub3, Email: nodecfg.SubscriptionEmail(3)})
	after, err := nodecfg.Build(in)
	if err != nil {
		t.Fatal(err)
	}

	plan, reason := agentxray.PlanHotApply(before, after, "Xray 26.10.21")
	if plan == nil {
		t.Fatalf("the agent would restart Xray for a new subscription: %s", reason)
	}
	if len(plan.Users) != 1 || plan.Users[0].Tag != nodecfg.EntryTag || !slices.Equal(plan.Users[0].Emails, []string{nodecfg.SubscriptionEmail(3)}) {
		t.Fatalf("hot plan adds %+v, want %s to the entry", plan.Users, nodecfg.SubscriptionEmail(3))
	}
}

// Only the bridge's own client may register the tunnel. AddUser takes the
// reverse tag from the inbound's xcoreHub entry, so a config with one would
// hand it to every subscription added later.
func TestAgentGivesNewUsersNoReverseTag(t *testing.T) {
	raw, err := nodecfg.Build(sample())
	if err != nil {
		t.Fatal(err)
	}
	tag, err := agentxray.ReverseTagFromConfig(raw, nodecfg.EntryTag)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "" {
		t.Fatalf("users added to the entry would get reverse tag %q", tag)
	}
}

// freePorts returns n distinct loopback ports nothing is listening on.
func freePorts(t *testing.T, n int) []int {
	t.Helper()
	var ports []int
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Skipf("can't open a loopback port here: %v", err)
		}
		defer l.Close() // held until all are chosen, so none repeats
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports
}

// The agent's own client against a node running the config: the API inbound
// answers where the agent looks, users can be listed, added and removed, and
// the stats service is there.
func TestAgentClientWorksOnARunningNode(t *testing.T) {
	in := sample()
	ports := freePorts(t, 2)
	in.EntryPort, in.APIPort = ports[0], ports[1]
	raw := patchInput(t, in, func(cfg map[string]any) {
		// keep the test off the network's interfaces
		cfg["inbounds"].([]any)[1].(map[string]any)["listen"] = "127.0.0.1"
	})
	inst := load(t, raw)
	if err := inst.Start(); err != nil {
		t.Fatalf("Xray won't start the config: %v", err)
	}

	client := agentxray.NewLocalClient(fmt.Sprintf("127.0.0.1:%d", in.APIPort), 5*time.Second)
	t.Cleanup(func() { client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	emails := func() []string {
		t.Helper()
		var users []*agentxray.User
		var err error
		// the API server comes up a moment after Start returns
		for {
			if users, err = client.GetInboundUsers(ctx, nodecfg.EntryTag); err == nil || ctx.Err() != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil {
			t.Fatalf("listing the entry's users: %v", err)
		}
		var out []string
		for _, u := range users {
			out = append(out, u.Email)
		}
		slices.Sort(out)
		return out
	}

	want := []string{nodecfg.BridgeEmail(7), nodecfg.SubscriptionEmail(1), nodecfg.SubscriptionEmail(2)}
	slices.Sort(want)
	if got := emails(); !slices.Equal(got, want) {
		t.Fatalf("the entry has users %v, want %v", got, want)
	}

	if err := client.AddUser(ctx, nodecfg.EntryTag, nodecfg.SubscriptionEmail(3), uuidSub3, "vless", nodecfg.Flow, "", 0); err != nil {
		t.Fatalf("adding a subscription: %v", err)
	}
	if got := emails(); !slices.Contains(got, nodecfg.SubscriptionEmail(3)) || len(got) != 4 {
		t.Fatalf("after AddUser the entry has users %v", got)
	}
	if err := client.RemoveUser(ctx, nodecfg.EntryTag, nodecfg.SubscriptionEmail(3)); err != nil {
		t.Fatalf("removing a subscription: %v", err)
	}
	if got := emails(); !slices.Equal(got, want) {
		t.Fatalf("after RemoveUser the entry has users %v, want %v", got, want)
	}

	if _, err := client.QueryStats(ctx, "", false); err != nil {
		t.Errorf("the stats service doesn't answer: %v", err)
	}
	if _, err := client.GetAllOnlineUsers(ctx); err != nil {
		t.Errorf("the online list doesn't answer: %v", err)
	}
}
