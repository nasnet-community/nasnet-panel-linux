// SPDX-License-Identifier: AGPL-3.0-or-later

package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// Every client the hub talks to a node with can be handed to this package.
var (
	_ Agent = agent.NodeClient(nil)
	_ Agent = (*agent.Client)(nil)
	_ Agent = (*agent.ReverseClient)(nil)
)

const (
	uuidA = "2f8e3f0b-3a38-4d27-9b0e-5c1d6a7e8f01"
	uuidB = "9c4b7a52-61de-4f3a-a8c7-0d2e1b3f4a65"
	uuidC = "d0b6f1c8-7e25-4a9d-b4f3-8a1c2e5d6b70"
)

var (
	sub1   = nodecfg.SubscriptionEmail(1)
	sub2   = nodecfg.SubscriptionEmail(2)
	sub3   = nodecfg.SubscriptionEmail(3)
	bridge = nodecfg.BridgeEmail(7)
)

// How a call reaches the agent decides the words around the agent's answer.
type route int

const (
	viaGRPC    route = iota // *agent.Client and the embedded client: "failed to add user: <message>"
	viaReverse              // *agent.ReverseClient: "<method> failed: <message>"
)

// What Xray says, whichever way the question arrives. The real agent test
// holds these sentences against a running Xray.
const (
	xrayExists  = "User %s already exists."
	xrayMissing = "User %s not found."
)

// xrayMissingInbound is what it says about an inbound it doesn't have, with
// the package prefix it adds and the tag in the text.
const xrayMissingInbound = "app/proxyman/command: failed to get handler: %[1]s > app/proxyman/inbound: handler not found: %[1]s"

type addCall struct {
	tag, email, uuid, protocol, flow, encryption string
	level                                        int32
}

// fakeNode behaves like the entry inbound of a node as far as clients go: Xray
// takes an email once, compares emails without regard to case, and refuses to
// remove one it doesn't have. Its errors are worded the way the agent and the
// three clients word them.
type fakeNode struct {
	mu    sync.Mutex
	route route
	// the clients, in the order they came
	emails []string
	uuids  map[string]string
	// showUUID makes ListUsers send the UUID too, which the real agent doesn't.
	showUUID bool

	adds    []addCall
	removes []string
	listed  int

	failAdd    map[string]error
	failRemove map[string]error
	failList   error
	// before runs first in every add and remove, so a test can cancel a context
	// part-way through.
	before func()
}

func newNode(emailsAndUUIDs ...string) *fakeNode {
	n := &fakeNode{uuids: map[string]string{}, failAdd: map[string]error{}, failRemove: map[string]error{}}
	for i := 0; i < len(emailsAndUUIDs); i += 2 {
		n.emails = append(n.emails, emailsAndUUIDs[i])
		n.uuids[emailsAndUUIDs[i]] = emailsAndUUIDs[i+1]
	}
	return n
}

func (n *fakeNode) find(email string) int {
	return slices.IndexFunc(n.emails, func(e string) bool { return strings.EqualFold(e, email) })
}

// asked words the agent's failure for a call, as it arrives at the hub: xray
// is Xray's own error, package prefix and all.
func (n *fakeNode) asked(fullMethod, action, email, xray string) error {
	msg := fmt.Sprintf("Failed to %s user: failed to %s user %s: rpc error: code = Unknown desc = %s", action, action, email, xray)
	if n.route == viaReverse {
		return fmt.Errorf("%s failed: %s", fullMethod, msg)
	}
	return fmt.Errorf("failed to %s user: %s", action, msg)
}

func (n *fakeNode) AddUser(ctx context.Context, tag, email, uuid, protocol, flow, encryption string, level int32) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.before != nil {
		n.before()
	}
	n.adds = append(n.adds, addCall{tag, email, uuid, protocol, flow, encryption, level})
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := n.failAdd[email]; err != nil {
		return err
	}
	if n.find(email) >= 0 {
		return n.asked(pb.NodeAgent_AddUser_FullMethodName, "add", email, "proxy/vless: "+fmt.Sprintf(xrayExists, email))
	}
	n.emails = append(n.emails, email)
	n.uuids[email] = uuid
	return nil
}

func (n *fakeNode) RemoveUser(ctx context.Context, tag, email string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.before != nil {
		n.before()
	}
	n.removes = append(n.removes, email)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := n.failRemove[email]; err != nil {
		return err
	}
	if tag != nodecfg.EntryTag {
		// what Xray says when the node has no such inbound: "not found" too, but
		// not about the user
		return n.asked(pb.NodeAgent_RemoveUser_FullMethodName, "remove", email, fmt.Sprintf(xrayMissingInbound, tag))
	}
	i := n.find(email)
	if i < 0 {
		return n.asked(pb.NodeAgent_RemoveUser_FullMethodName, "remove", email, "proxy/vless: "+fmt.Sprintf(xrayMissing, email))
	}
	delete(n.uuids, n.emails[i])
	n.emails = slices.Delete(n.emails, i, i+1)
	return nil
}

func (n *fakeNode) ListUsers(ctx context.Context, tag string) ([]*pb.UserInfo, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.listed++
	if n.failList != nil {
		return nil, n.failList
	}
	var out []*pb.UserInfo
	for _, e := range n.emails {
		u := &pb.UserInfo{Email: e}
		if n.showUUID {
			u.Uuid = n.uuids[e]
		}
		out = append(out, u)
	}
	// Xray lists its clients in no particular order
	slices.Reverse(out)
	return out, nil
}

// has is the node's clients as a sorted list.
func (n *fakeNode) has() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := slices.Clone(n.emails)
	slices.Sort(out)
	return out
}

func (n *fakeNode) calls() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.adds) + len(n.removes) + n.listed
}

func sorted(s ...string) []string { slices.Sort(s); return s }

func TestEnableAddsTheClientTheWayTheNodeExpects(t *testing.T) {
	n := newNode()
	if err := Enable(context.Background(), n, Client{Email: sub1, UUID: uuidA}); err != nil {
		t.Fatal(err)
	}
	want := addCall{tag: "entry", email: sub1, uuid: uuidA, protocol: "vless", flow: "xtls-rprx-vision", encryption: "none", level: 0}
	if len(n.adds) != 1 || n.adds[0] != want {
		t.Fatalf("AddUser got %+v, want %+v", n.adds, want)
	}
}

// The flow in AddUser, in the links and in the node's config are one value.
func TestFlowMatchesTheNodeConfig(t *testing.T) {
	if DefaultFlow != nodecfg.Flow {
		t.Fatalf("clients are added with flow %q but the node's config says %q", DefaultFlow, nodecfg.Flow)
	}
}

func TestEnableAndDisableAreSafeToRepeat(t *testing.T) {
	for name, r := range map[string]route{"grpc": viaGRPC, "reverse": viaReverse} {
		t.Run(name, func(t *testing.T) {
			n := newNode()
			n.route = r
			ctx := context.Background()
			c := Client{Email: sub1, UUID: uuidA}

			for i := 0; i < 3; i++ {
				if err := Enable(ctx, n, c); err != nil {
					t.Fatalf("enable #%d: %v", i+1, err)
				}
			}
			if got := n.has(); !slices.Equal(got, []string{sub1}) {
				t.Fatalf("node has %v after enabling three times", got)
			}
			for i := 0; i < 3; i++ {
				if err := Disable(ctx, n, sub1); err != nil {
					t.Fatalf("disable #%d: %v", i+1, err)
				}
			}
			if got := n.has(); len(got) != 0 {
				t.Fatalf("node has %v after disabling", got)
			}
			// off and on again is the same client
			must(t, Enable(ctx, n, c))
			if n.uuids[sub1] != uuidA {
				t.Fatalf("the client came back with %s", n.uuids[sub1])
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Enable leaves a client that is there alone, whatever credential it has: the
// agent can't say. Replace is how a credential changes.
func TestEnableKeepsTheClientTheNodeHas(t *testing.T) {
	n := newNode(sub1, uuidA)
	must(t, Enable(context.Background(), n, Client{Email: sub1, UUID: uuidB}))
	if n.uuids[sub1] != uuidA {
		t.Fatalf("Enable changed the credential to %s", n.uuids[sub1])
	}
}

func TestReplaceSwapsTheCredential(t *testing.T) {
	ctx := context.Background()

	n := newNode(sub1, uuidA, bridge, uuidC)
	must(t, Replace(ctx, n, Client{Email: sub1, UUID: uuidB}))
	if n.uuids[sub1] != uuidB || !slices.Equal(n.has(), sorted(sub1, bridge)) {
		t.Fatalf("after Replace the node has %v with %s", n.has(), n.uuids[sub1])
	}
	if n.removes[0] != sub1 || len(n.adds) != 1 || n.adds[0].uuid != uuidB {
		t.Fatalf("calls: removes %v, adds %+v", n.removes, n.adds)
	}

	// a node that never had the client gets it
	n = newNode()
	must(t, Replace(ctx, n, Client{Email: sub2, UUID: uuidB}))
	if !slices.Equal(n.has(), []string{sub2}) {
		t.Fatalf("node has %v", n.has())
	}

	// the add failing leaves the node without it, and says why
	n = newNode(sub1, uuidA)
	cause := errors.New("connection reset")
	n.failAdd[sub1] = cause
	err := Replace(ctx, n, Client{Email: sub1, UUID: uuidB})
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), sub1) {
		t.Fatalf("err = %v", err)
	}
	if len(n.has()) != 0 {
		t.Fatalf("node has %v, want none", n.has())
	}
	// and Enable puts it back
	delete(n.failAdd, sub1)
	must(t, Enable(ctx, n, Client{Email: sub1, UUID: uuidB}))
	if n.uuids[sub1] != uuidB {
		t.Fatal("the client is not back")
	}
}

// The words the idempotence check goes by, as every client delivers them.
func TestTheAgentsWordsAreMatchedOnTheSentenceAboutTheEmail(t *testing.T) {
	exists := fmt.Sprintf(xrayExists, sub1)
	missing := fmt.Sprintf(xrayMissing, sub1)
	wrap := func(action, what string) []error {
		msg := fmt.Sprintf("Failed to %s user: failed to %s user %s: rpc error: code = Unknown desc = proxy/vless: %s", action, action, sub1, what)
		return []error{
			fmt.Errorf("failed to %s user: %s", action, msg),
			fmt.Errorf("%s failed: %s", pb.NodeAgent_AddUser_FullMethodName, msg),
			errors.New(what),
			errors.New(strings.ToUpper(what)),
		}
	}
	for _, err := range wrap("add", exists) {
		if !alreadyThere(err, sub1) {
			t.Errorf("%q isn't taken as 'already there'", err)
		}
		if alreadyThere(err, sub2) {
			t.Errorf("%q is taken as an answer about %s", err, sub2)
		}
		if alreadyGone(err, sub1) {
			t.Errorf("%q is taken as 'already gone'", err)
		}
	}
	for _, err := range wrap("remove", missing) {
		if !alreadyGone(err, sub1) {
			t.Errorf("%q isn't taken as 'already gone'", err)
		}
		if alreadyGone(err, sub2) {
			t.Errorf("%q is taken as an answer about %s", err, sub2)
		}
		if alreadyThere(err, sub1) {
			t.Errorf("%q is taken as 'already there'", err)
		}
	}
	for _, err := range []error{
		nil,
		errors.New("handler not found: entry"),
		fmt.Errorf("failed to remove user: Failed to remove user: failed to remove user %s: rpc error: code = Unknown desc = %s", sub1, fmt.Sprintf(xrayMissingInbound, "entry")),
		errors.New("rpc error: code = Unavailable desc = connection error"),
		context.DeadlineExceeded,
		errors.New("not found"),
		errors.New("failed to remove user: proxy is not a UserManager"),
	} {
		if alreadyGone(err, sub1) || alreadyThere(err, sub1) {
			t.Errorf("%v is taken as an idempotent answer", err)
		}
	}
}

func TestOtherFailuresAreReportedWithTheEmail(t *testing.T) {
	ctx := context.Background()
	cause := errors.New("rpc error: code = Unavailable desc = connection error")

	n := newNode()
	n.failAdd[sub1] = cause
	err := Enable(ctx, n, Client{Email: sub1, UUID: uuidA})
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), sub1) {
		t.Fatalf("Enable error = %v", err)
	}
	if strings.Contains(err.Error(), uuidA) {
		t.Fatalf("the error carries the credential: %v", err)
	}

	n = newNode(sub2, uuidB)
	n.failRemove[sub2] = cause
	err = Disable(ctx, n, sub2)
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), sub2) {
		t.Fatalf("Disable error = %v", err)
	}

	// an inbound the node doesn't have is a fault, not a removed client
	err = n.RemoveUser(ctx, "nope", sub2)
	if err == nil || alreadyGone(err, sub2) {
		t.Fatalf("a missing inbound reads as a removed client: %v", err)
	}

	// a context that is done is an error too, not a quiet success
	done, cancel := context.WithCancel(ctx)
	cancel()
	if err := Enable(done, newNode(), Client{Email: sub1, UUID: uuidA}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Enable on a cancelled context: %v", err)
	}
	if err := Disable(done, newNode(sub1, uuidA), sub1); !errors.Is(err, context.Canceled) {
		t.Fatalf("Disable on a cancelled context: %v", err)
	}
}

// The bridge's client carries the tunnel, and a stray one isn't ours to touch.
func TestOnlySubscriptionClientsAreManaged(t *testing.T) {
	ctx := context.Background()
	for _, email := range []string{
		bridge, "", "alice@example.com", "sub-0@remote-nodes", "sub-01@remote-nodes", "sub-1@remote-nodes.example",
		"SUB-1@REMOTE-NODES", "sub-@remote-nodes", "sub-1@reverse", "xsub-1@remote-nodes",
	} {
		n := newNode(email, uuidA)
		if err := Enable(ctx, n, Client{Email: email, UUID: uuidA}); err == nil {
			t.Errorf("Enable accepted %q", email)
		}
		if err := Disable(ctx, n, email); err == nil {
			t.Errorf("Disable accepted %q", email)
		}
		if err := Replace(ctx, n, Client{Email: email, UUID: uuidB}); err == nil {
			t.Errorf("Replace accepted %q", email)
		}
		if _, err := Reconcile(ctx, n, []Client{{Email: email, UUID: uuidA}}); err == nil {
			t.Errorf("Reconcile accepted %q as wanted", email)
		}
		if n.calls() != 0 {
			t.Errorf("%q: the agent was called %d times", email, n.calls())
		}
	}
}

func TestAClientNeedsAUUID(t *testing.T) {
	ctx := context.Background()
	for _, bad := range []string{"", "not-a-uuid", uuidA[:35], uuidA + "0"} {
		n := newNode()
		err := Enable(ctx, n, Client{Email: sub1, UUID: bad})
		if err == nil || n.calls() != 0 {
			t.Errorf("uuid %q: err = %v, calls = %d", bad, err, n.calls())
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Errorf("the error repeats the credential: %v", err)
		}
		if _, err := Reconcile(ctx, n, []Client{{Email: sub1, UUID: bad}}); err == nil || n.calls() != 0 {
			t.Errorf("Reconcile with uuid %q: err = %v, calls = %d", bad, err, n.calls())
		}
	}
}

func TestReconcileAddsWhatIsMissingAndRemovesWhatIsExtra(t *testing.T) {
	n := newNode(sub1, uuidA, sub2, uuidB, bridge, uuidC, "legacy@example.com", uuidC)
	d, err := Reconcile(context.Background(), n, []Client{{sub1, uuidA}, {sub3, uuidC}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.Added, []string{sub3}) || !slices.Equal(d.Removed, []string{sub2}) || len(d.Replaced) != 0 {
		t.Fatalf("diff = %+v", d)
	}
	if want := sorted(bridge, "legacy@example.com"); !slices.Equal(d.Foreign, want) {
		t.Fatalf("foreign = %v, want %v", d.Foreign, want)
	}
	if want := sorted(sub1, sub3, bridge, "legacy@example.com"); !slices.Equal(n.has(), want) {
		t.Fatalf("node has %v, want %v", n.has(), want)
	}
	// the one that was right was not touched
	for _, e := range append(append([]string{}, n.removes...), emailsOf(n.adds)...) {
		if e == sub1 {
			t.Fatalf("%s was changed although it was right", sub1)
		}
	}
	if len(n.adds) != 1 || n.adds[0].email != sub3 || n.adds[0].uuid != uuidC {
		t.Fatalf("adds = %+v", n.adds)
	}
}

func emailsOf(adds []addCall) []string {
	var out []string
	for _, a := range adds {
		out = append(out, a.email)
	}
	return out
}

func TestReconcileWithNothingToDoDoesNothing(t *testing.T) {
	n := newNode(sub1, uuidA, sub2, uuidB, bridge, uuidC)
	d, err := Reconcile(context.Background(), n, []Client{{sub2, uuidB}, {sub1, uuidA}})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Added)+len(d.Removed)+len(d.Replaced) != 0 || !slices.Equal(d.Foreign, []string{bridge}) {
		t.Fatalf("diff = %+v", d)
	}
	if len(n.adds)+len(n.removes) != 0 || n.listed != 1 {
		t.Fatalf("adds %d, removes %d, lists %d", len(n.adds), len(n.removes), n.listed)
	}
}

// The sweep takes off subscription clients only: the bridge, and anything that
// merely looks a little like one, stays.
func TestReconcileNeverTouchesTheBridgeOrLookAlikes(t *testing.T) {
	keep := []string{
		bridge, nodecfg.BridgeEmail(1), "sub-0@remote-nodes", "sub-007@remote-nodes", "sub-1@remote-nodes.example",
		"sub-3@Remote-Nodes", "alice@example.com", "", "sub-@remote-nodes", "sub-1@reverse",
	}
	var args []string
	for _, e := range keep {
		args = append(args, e, uuidC)
	}
	args = append(args, sub1, uuidA, sub2, uuidB)
	n := newNode(args...)

	d, err := Reconcile(context.Background(), n, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := sorted(sub1, sub2); !slices.Equal(sorted(d.Removed...), want) {
		t.Fatalf("removed %v, want %v", d.Removed, want)
	}
	if want := sorted(keep...); !slices.Equal(d.Foreign, want) {
		t.Fatalf("foreign = %q, want %q", d.Foreign, want)
	}
	if got, want := n.has(), sorted(keep...); !slices.Equal(got, want) {
		t.Fatalf("node has %q, want %q", got, want)
	}
	for _, r := range n.removes {
		if _, ok := nodecfg.ParseSubscriptionEmail(r); !ok {
			t.Errorf("RemoveUser was called for %q", r)
		}
	}
}

func TestReconcileReplacesAClientWhoseUUIDChanged(t *testing.T) {
	n := newNode(sub1, uuidA, sub2, uuidB)
	n.showUUID = true
	d, err := Reconcile(context.Background(), n, []Client{{sub1, uuidC}, {sub2, strings.ToUpper(uuidB)}})
	if err != nil {
		t.Fatal(err)
	}
	// the same UUID in capitals is the same UUID
	if !slices.Equal(d.Replaced, []string{sub1}) || len(d.Added)+len(d.Removed) != 0 {
		t.Fatalf("diff = %+v", d)
	}
	if n.uuids[sub1] != uuidC || n.uuids[sub2] != uuidB {
		t.Fatalf("uuids on the node: %v", n.uuids)
	}
	if slices.Contains(n.removes, sub2) || slices.Contains(emailsOf(n.adds), sub2) {
		t.Fatalf("%s was touched although its UUID was right", sub2)
	}
}

// The real agent lists the email and the level, no UUID. A changed UUID can't
// be seen then and is not guessed at.
func TestReconcileDoesNotGuessWhenTheAgentSendsNoUUID(t *testing.T) {
	n := newNode(sub1, uuidA)
	n.showUUID = false
	d, err := Reconcile(context.Background(), n, []Client{{sub1, uuidB}})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Added)+len(d.Removed)+len(d.Replaced) != 0 || len(n.adds)+len(n.removes) != 0 {
		t.Fatalf("diff = %+v, adds %v, removes %v", d, n.adds, n.removes)
	}
}

func TestReconcileOneFailureDoesNotStopTheRest(t *testing.T) {
	// 1 is extra and refuses to go; 2 is wanted and refuses to be added; 3 and
	// 4 are wanted and go through
	sub4 := nodecfg.SubscriptionEmail(4)
	n := newNode(sub1, uuidA, bridge, uuidC)
	failAdd := errors.New("rpc error: code = DeadlineExceeded desc = context deadline exceeded")
	failRemove := errors.New("rpc error: code = Unavailable desc = connection error")
	n.failAdd[sub2] = failAdd
	n.failRemove[sub1] = failRemove

	d, err := Reconcile(context.Background(), n, []Client{{sub2, uuidB}, {sub3, uuidC}, {sub4, uuidA}})
	if err == nil {
		t.Fatal("two calls failed and Reconcile reports none")
	}
	if !errors.Is(err, failAdd) || !errors.Is(err, failRemove) {
		t.Fatalf("the joined error lost a cause: %v", err)
	}
	text := err.Error()
	if !strings.Contains(text, "add "+sub2) || !strings.Contains(text, "remove "+sub1) {
		t.Fatalf("the error doesn't say which emails failed: %v", err)
	}
	if strings.Contains(text, sub3) || strings.Contains(text, sub4) {
		t.Fatalf("the error blames emails that went through: %v", err)
	}
	for _, uuid := range []string{uuidA, uuidB, uuidC} {
		if strings.Contains(text, uuid) {
			t.Fatalf("the error carries a credential: %v", err)
		}
	}
	if !slices.Equal(d.Added, []string{sub3, sub4}) || len(d.Removed) != 0 {
		t.Fatalf("diff = %+v, want only what went through", d)
	}
	if got, want := n.has(), sorted(sub1, sub3, sub4, bridge); !slices.Equal(got, want) {
		t.Fatalf("node has %v, want %v", got, want)
	}
}

// A replace that loses its add has taken the client off the node, and the diff
// says so.
func TestReconcileReportsAReplaceThatLostItsAdd(t *testing.T) {
	n := newNode(sub1, uuidA)
	n.showUUID = true
	cause := errors.New("connection reset")
	n.failAdd[sub1] = cause
	d, err := Reconcile(context.Background(), n, []Client{{sub1, uuidB}})
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v", err)
	}
	if !slices.Equal(d.Removed, []string{sub1}) || len(d.Replaced) != 0 || len(n.has()) != 0 {
		t.Fatalf("diff = %+v, node has %v", d, n.has())
	}
}

func TestReconcileFailsCleanlyWhenTheNodeCannotBeListed(t *testing.T) {
	n := newNode(sub2, uuidB)
	cause := errors.New("rpc error: code = Unavailable desc = connection error")
	n.failList = cause
	d, err := Reconcile(context.Background(), n, []Client{{sub1, uuidA}})
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v", err)
	}
	if len(d.Added)+len(d.Removed)+len(d.Replaced)+len(d.Foreign) != 0 || len(n.adds)+len(n.removes) != 0 {
		t.Fatalf("diff = %+v after a failed listing", d)
	}
}

// A done context ends the work and is reported once, not once per client.
func TestReconcileStopsWhenTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := newNode(sub1, uuidA)
	calls := 0
	n.before = func() {
		if calls++; calls == 2 {
			cancel()
		}
	}
	want := []Client{{sub2, uuidB}, {sub3, uuidC}, {nodecfg.SubscriptionEmail(4), uuidA}, {nodecfg.SubscriptionEmail(5), uuidB}}
	d, err := Reconcile(ctx, n, want)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if got := strings.Count(err.Error(), context.Canceled.Error()); got != 1 {
		t.Fatalf("the cancellation is reported %d times: %v", got, err)
	}
	// the first went through, the second met the cancellation, nothing was tried after it
	if !slices.Equal(d.Added, []string{sub2}) {
		t.Fatalf("added %v", d.Added)
	}
	if len(n.adds) != 2 || len(n.removes) != 0 {
		t.Fatalf("adds %d, removes %d: the work went on after the context was done", len(n.adds), len(n.removes))
	}

	// a context that is done before the first change is reported on its own
	done, stop := context.WithCancel(context.Background())
	stop()
	n = newNode(sub1, uuidA)
	d, err = Reconcile(done, n, []Client{{sub2, uuidB}})
	if !errors.Is(err, context.Canceled) || len(n.adds)+len(n.removes) != 0 || len(d.Added) != 0 {
		t.Fatalf("err = %v, adds %d, removes %d", err, len(n.adds), len(n.removes))
	}
}

func TestReconcileRefusesAWantThatMakesNoSense(t *testing.T) {
	n := newNode(sub1, uuidA)
	if _, err := Reconcile(context.Background(), n, []Client{{sub2, uuidB}, {sub2, uuidC}}); err == nil {
		t.Fatal("a client wanted twice was accepted")
	}
	if n.calls() != 0 {
		t.Fatalf("the agent was called %d times", n.calls())
	}
}

// Ten subscriptions on three nodes is the cap; a sweep with all of them stays
// at one listing and the calls it needs.
func TestReconcileAtTheCap(t *testing.T) {
	var want []Client
	for i := 1; i <= 10; i++ {
		want = append(want, Client{Email: nodecfg.SubscriptionEmail(i), UUID: fmt.Sprintf("2f8e3f0b-3a38-4d27-9b0e-5c1d6a7e%04x", i)})
	}
	n := newNode(bridge, uuidC)
	d, err := Reconcile(context.Background(), n, want)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Added) != 10 || n.listed != 1 || len(n.has()) != 11 {
		t.Fatalf("added %d, listed %d, node has %d", len(d.Added), n.listed, len(n.has()))
	}
	// the order the caller gave
	for i, e := range d.Added {
		if e != want[i].Email {
			t.Fatalf("added[%d] = %s, want %s", i, e, want[i].Email)
		}
	}
	// and a second pass finds nothing to do
	d, err = Reconcile(context.Background(), n, want)
	if err != nil || len(d.Added)+len(d.Removed)+len(d.Replaced) != 0 {
		t.Fatalf("second pass: %+v, %v", d, err)
	}
}
