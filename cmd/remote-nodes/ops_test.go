// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/manager"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netroles"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
)

// A role's address is used when this machine has it, and otherwise the system
// chooses, which is what a laptop or a lab wants.
func TestARolesAddressIsUsedOnlyWhenAnInterfaceHoldsIt(t *testing.T) {
	ip := netip.MustParseAddr("192.168.50.18")
	if got := boundTo(netroles.Addr{Name: "ADDR_NODES", IP: ip, Local: true}); got != ip {
		t.Errorf("a held address: %v", got)
	}
	if got := boundTo(netroles.Addr{Name: "ADDR_NODES", IP: ip}); got.IsValid() {
		t.Errorf("an address no interface holds: %v", got)
	}
}

// connectOps is the operations with only a Connect, which says who made it.
type connectOps struct {
	manager.Ops
	name string
	log  *[]string
}

func (c connectOps) Connect(_ context.Context, n model.Node) (manager.Client, error) {
	*c.log = append(*c.log, c.name+" "+n.Name)
	return nil, errors.New(c.name + " has no link")
}

// The connector asks for the operations each time it connects, so that a link
// made after a backup replaced the CA is made under the new one.
func TestTheConnectorAsksForTheOperationsAtEachConnect(t *testing.T) {
	var log []string
	current := "first"
	ask := func() (manager.Ops, error) { return connectOps{name: current, log: &log}, nil }
	c := connectorOf(ask)
	n := model.Node{ID: 1, Name: "Tehran"}

	got, err := c.Connect(context.Background(), n)
	if got != nil || err == nil || err.Error() != "first has no link" {
		t.Fatalf("Connect = %v, %v", got, err)
	}
	current = "second" // the CA was replaced
	if _, err := c.Connect(context.Background(), n); err == nil || err.Error() != "second has no link" {
		t.Fatalf("Connect after the CA was replaced: %v", err)
	}
	if want := []string{"first Tehran", "second Tehran"}; fmt.Sprint(log) != fmt.Sprint(want) {
		t.Errorf("connects = %v, want %v", log, want)
	}

	// no operations, no link, and the reason is the plain one
	unusable := errors.New("the hub can't use its certificate authority: it has expired")
	got, err = connectorOf(func() (manager.Ops, error) { return nil, unusable }).Connect(context.Background(), n)
	if got != nil || !errors.Is(err, unusable) {
		t.Errorf("with no operations: %v, %v", got, err)
	}
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// With a CA that has run out the manager has no operations and the connector
// says why, in a sentence, without trying anything; with one that is good it
// dials, and a node that isn't there is an agent that doesn't answer.
func TestTheConnectorOfARealManagerAnswersForTheCAItHolds(t *testing.T) {
	dir := t.TempDir()
	expired, err := pki.NewCA(time.Now().AddDate(-11, 0, 0)) // made 11 years ago, good for 10
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(func(s *model.State) error {
		s.CACert, s.CAKey = string(expired.CertPEM()), string(expired.KeyPEM())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	coll, err := usage.Open(usage.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var mgr *manager.Manager
	mgr, err = manager.New(manager.Config{
		Store: st, Usage: coll, Log: quietLog(),
		Connector: connectorOf(func() (manager.Ops, error) { return mgr.Ops() }),
		NewOps:    opsFor(netip.Addr{}, nil, quietLog(), st),
	})
	if err != nil {
		t.Fatal(err)
	}
	if mgr.CA() != nil {
		t.Fatal("the manager took a CA that has run out")
	}
	closed := closedPort(t)
	n := model.Node{ID: 1, Name: "Tehran", Host: "127.0.0.1", AgentPort: closed}
	c, err := connectorOf(func() (manager.Ops, error) { return mgr.Ops() }).Connect(context.Background(), n)
	if c != nil || err == nil || !strings.Contains(err.Error(), "certificate authority") || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("Connect with a CA that has run out = %v, %v", c, err)
	}

	// another CA takes its place, as a backup restore does, and the connector dials
	good, err := pki.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(func(s *model.State) error {
		s.CACert, s.CAKey = string(good.CertPEM()), string(good.KeyPEM())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mgr.Sync()
	if mgr.CA() == nil {
		t.Fatalf("the manager didn't take the new CA: %v", mgr.CAError())
	}
	c, err = connectorOf(func() (manager.Ops, error) { return mgr.Ops() }).Connect(context.Background(), n)
	if c != nil || !errors.Is(err, agentops.ErrUnreachable) {
		t.Fatalf("Connect to a port nothing listens on = %v, %v", c, err)
	}
}

// closedPort is a loopback port that nothing listens on.
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback port here: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// The hub's calls to add and take out a node run on the real operations, over
// the real manager, job engine and state: a server that can't be reached shows
// as one, and an install that fails before it changed anything leaves no node.
func TestTheHubRunsTheNodeOperationsOnTheRealThing(t *testing.T) {
	addr, stop := start(t, testSettings(t, t.TempDir(), ""))
	defer stop()
	url := "http://" + addr
	port := closedPort(t)
	host := fmt.Sprintf(`"host":"127.0.0.1","sshPort":%d,"sshUser":"root"`, port)
	const key = "SHA256:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq"

	// a server that doesn't answer
	code, _, body := callHub(t, "POST", url+"/api/probe", "{"+host+"}")
	got := jsonOf(t, body)
	if code != 502 || got["code"] != "node_unreachable" || !strings.Contains(got["error"].(string), fmt.Sprint(port)) {
		t.Fatalf("probe = %d %s", code, body)
	}

	// an install on it fails at the first step, and says so as a sentence, with what was done about it
	code, _, body = callHub(t, "POST", url+"/api/nodes", fmt.Sprintf(`{%s,"password":"hunter2-MARKER","hostKey":%q,"name":"Closed","entryPort":8443}`, host, key))
	if code != 202 {
		t.Fatalf("install = %d %s", code, body)
	}
	job := waitJob(t, url, jsonOf(t, body)["jobId"].(string))
	errText, _ := job["error"].(string)
	if job["state"] != "failed" || job["kind"] != "install" || job["target"] != "Closed" || !strings.HasPrefix(errText, "Can't reach SSH on 127.0.0.1") ||
		!strings.HasSuffix(errText, "Nothing was changed on the server, and nothing was kept.") || job["code"] != "node_unreachable" {
		t.Fatalf("job = %v", job)
	}
	// what was asked, for another try, and none of the login's secrets
	if form, _ := job["install"].(map[string]any); form["name"] != "Closed" || form["host"] != "127.0.0.1" || form["sshPort"] != float64(port) || form["sshUser"] != "root" || form["entryPort"] != 8443.0 {
		t.Errorf("the job's form = %v", job["install"])
	}
	steps := job["steps"].([]any)
	if steps[0].(map[string]any)["state"] != "failed" || len(steps) != 16 {
		t.Errorf("steps = %v", steps)
	}
	// every step goes by a key, which the page words it by: main gives the engine the manager's
	keys := map[string]bool{}
	for i, s := range steps {
		key, _ := s.(map[string]any)["key"].(string)
		if key == "" || keys[key] {
			t.Errorf("step %d has the key %q, which is empty or the same as another's: %v", i, key, steps)
		}
		keys[key] = true
	}
	if first, last := steps[0].(map[string]any), steps[15].(map[string]any); first["key"] != "check_server" || first["name"] != "Check the server" || last["key"] != "discard_node" || last["state"] != "ok" {
		t.Errorf("first and last steps = %v, %v", first, last)
	}
	if strings.Contains(fmt.Sprint(job), "hunter2") {
		t.Errorf("the login shows in the job: %v", job)
	}

	// and no node is left behind
	if code, _, body = callHub(t, "GET", url+"/api/nodes/1", ""); code != 404 {
		t.Fatalf("node = %d %s", code, body)
	}
	if _, _, body = callHub(t, "GET", url+"/api/nodes", ""); string(body) != "[]\n" {
		t.Errorf("nodes = %s", body)
	}
	if _, _, body = callHub(t, "GET", url+"/api/config", ""); jsonOf(t, body)["nodeCount"] != 0.0 {
		t.Errorf("config = %s", body)
	}
}

// A state file that holds an install as running, from a hub that stopped
// while it ran, comes back as an unfinished install: no node, shown apart, and
// taken out with a login or by forgetting it.
func TestTheHubReadsAnInstallFromBeforeARestartAsUnfinished(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(func(s *model.State) error {
		_, err := s.AddNode(model.Node{Name: "Stopped", Host: "203.0.113.9", SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeInstalling, CreatedAt: time.Now()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	addr, stop := start(t, testSettings(t, dir, ""))
	defer stop()
	url := "http://" + addr

	_, _, body := callHub(t, "GET", url+"/api/nodes/1", "")
	n := jsonOf(t, body)
	unfinished, _ := n["unfinished"].(map[string]any)
	if n["registered"] != false || n["installing"] != nil || unfinished == nil || unfinished["error"] == "" {
		t.Fatalf("node = %s", body)
	}
	if _, _, body = callHub(t, "GET", url+"/api/config", ""); jsonOf(t, body)["nodeCount"] != 0.0 {
		t.Errorf("an unfinished install counts as a node: %s", body)
	}
	// taking it out needs a login, or force
	code, _, body := callHub(t, "POST", url+"/api/nodes/1/remove", "{}")
	if got := jsonOf(t, body); code != 409 || got["code"] != "needs_login" || got["needLogin"] != true {
		t.Fatalf("remove with no login = %d %s", code, body)
	}
	code, _, body = callHub(t, "POST", url+"/api/nodes/1/remove", `{"force":true}`)
	if code != 202 {
		t.Fatalf("remove = %d %s", code, body)
	}
	job := waitJob(t, url, jsonOf(t, body)["jobId"].(string))
	if job["state"] != "ok" || job["kind"] != "remove" || job["target"] != "Stopped" {
		t.Fatalf("job = %v", job)
	}
	if forget := job["steps"].([]any); len(forget) != 1 || forget[0].(map[string]any)["key"] != "forget_node" {
		t.Errorf("the steps of a removal that only forgets = %v", forget)
	}
	if _, _, body = callHub(t, "GET", url+"/api/nodes", ""); string(body) != "[]\n" {
		t.Errorf("nodes = %s", body)
	}
	// the calls that were refused for a node that is gone say it is
	for _, path := range []string{"/api/nodes/1/rotate-keys", "/api/nodes/1/renew-cert", "/api/nodes/1/check-updates"} {
		if code, _, body := callHub(t, "POST", url+path, ""); code != 404 || jsonOf(t, body)["code"] != "not_found" {
			t.Errorf("POST %s = %d %s", path, code, body)
		}
	}
}

// waitJob follows a job until it ends.
func waitJob(t *testing.T, url, id string) map[string]any {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		_, _, body := callHub(t, "GET", url+"/api/jobs/"+id, "")
		if j := jsonOf(t, body); j["state"] != "running" {
			return j
		}
	}
	t.Fatalf("job %s never ended", id)
	return nil
}

// An install asks the state whether the node a server's marker names is still
// here and set up: only then is the server the same node under another address.
func TestLiveNodeAnswersOnlyForASetUpNode(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var ready, half model.Node
	if err := st.Update(func(s *model.State) error {
		var err error
		if ready, err = s.AddNode(model.Node{Name: "ready", Host: "203.0.113.5", SSHPort: 22, SSHUser: "root", AgentPort: 9090, EntryPort: 443, State: model.NodeRegistered, CreatedAt: time.Now()}); err != nil {
			return err
		}
		half, err = s.AddNode(model.Node{Name: "half", Host: "203.0.113.6", SSHPort: 22, SSHUser: "root", AgentPort: 9090, EntryPort: 443, State: model.NodeInstalling, CreatedAt: time.Now()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	live := liveNode(st)
	if name, ok := live(ready.ID); !ok || name != "ready" {
		t.Errorf("a set-up node: %q %v", name, ok)
	}
	if _, ok := live(half.ID); ok {
		t.Error("an install that never finished counts as a node")
	}
	if _, ok := live(99); ok {
		t.Error("a node that isn't there counts")
	}
	if _, ok := liveNode(nil)(ready.ID); ok {
		t.Error("no state still answered")
	}
}
