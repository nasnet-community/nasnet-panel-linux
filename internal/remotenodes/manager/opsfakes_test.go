// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// A key the SSH library could have printed, and a second one.
const (
	fingerprintA = "SHA256:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq"
	fingerprintB = "SHA256:qponmlkjihgfedcbaZYXWVUTSRQPONMLKJIHGFEDCBA"
)

// fakeOps is the node operations with nothing behind them. It records each
// call and what it was given, answers what the test says, and can fail or
// wait at any step or call, by name: a step of a plan ("Upload the agent"), or
// a call ("Connect", "PushConfig"). A failure is for every time that point is
// reached, or for the nth with "name#n".
type fakeOps struct {
	mu  sync.Mutex
	net *fakeNet // where Connect gets the link to a node's fake agent

	fail  map[string]error
	hold  map[string]chan struct{} // a point waits for its channel to close
	count map[string]int           // how many times each point was reached

	calls []string

	probes     []string
	installs   []opsInstall
	uninstalls []opsInstall
	provisions []agentops.ProvisionInput
	pushes     []opsPush
	checks     []opsCheck
	updates    []opsUpdate
	renewals   []model.Node

	// A misbehaving library echoes what it was given in its errors: a failing
	// install repeats the login, and a failing provision the REALITY key.
	leakLogin bool
	leakKeys  bool

	// What the calls answer.
	fingerprint string
	install     agentops.InstallResult
	target      string // what an install chooses, when it isn't told
	check       agentops.HandshakeCheck
	checkFor    map[string]agentops.HandshakeCheck // by target, over check
	newAgent    string                             // what an agent update brings
	cert        agentops.CertInfo
}

type opsInstall struct {
	node  model.Node
	login agentops.Login
}

type opsPush struct {
	client Client
	in     nodecfg.Input
}

type opsCheck struct {
	client Client
	target string
}

type opsUpdate struct {
	kind   string // "agent" or "xray"
	client Client
	node   model.Node
}

func newFakeOps(net *fakeNet) *fakeOps {
	return &fakeOps{
		net: net, fail: map[string]error{}, hold: map[string]chan struct{}{}, count: map[string]int{},
		fingerprint: fingerprintA,
		install: agentops.InstallResult{
			Changed: true, Arch: "arm64", OS: "Debian GNU/Linux 12 (bookworm)", AgentVersion: "v1.3.0",
			CertSerial: "41", CertNotBefore: epoch.Add(-time.Hour), CertNotAfter: epoch.AddDate(1, 0, 0),
		},
		target:   "www.google.com:443",
		check:    agentops.HandshakeCheck{OK: true, TLS13: true, X25519: true, HandshakeBytes: 4200, LatencyMs: 38},
		checkFor: map[string]agentops.HandshakeCheck{},
		newAgent: "v1.3.1",
		cert:     agentops.CertInfo{Serial: "42", NotBefore: epoch, NotAfter: epoch.AddDate(1, 0, 0)},
	}
}

func (f *fakeOps) failAt(point string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[point] = err
}

func (f *fakeOps) holdAt(point string) (release func()) {
	ch := make(chan struct{})
	f.mu.Lock()
	f.hold[point] = ch
	f.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

func (f *fakeOps) note(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

// reach is where a call may wait, and fail.
func (f *fakeOps) reach(ctx context.Context, point string) error {
	f.mu.Lock()
	f.count[point]++
	nth := f.count[point]
	ch := f.hold[point]
	err := f.fail[point]
	if e := f.fail[fmt.Sprintf("%s#%d", point, nth)]; e != nil {
		err = e
	}
	f.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

// leaked is an error that has said too much.
type leaked struct {
	err   error
	spilt []string
}

func (e *leaked) Error() string { return e.err.Error() + " (" + strings.Join(e.spilt, " | ") + ")" }
func (e *leaked) Unwrap() error { return e.err }

// steps runs a plan the way agentops does: each step is running and then ok,
// and the one that fails says why, with whatever the caller says it spilt.
func (f *fakeOps) steps(ctx context.Context, plan []string, progress agentops.Progress, spilt ...string) error {
	for _, name := range plan {
		progress(agentops.Step{Name: name, State: agentops.StateRunning})
		if err := f.reach(ctx, name); err != nil {
			if len(spilt) > 0 {
				err = &leaked{err, spilt}
			}
			progress(agentops.Step{Name: name, State: agentops.StateFailed, Detail: err.Error()})
			return err
		}
		progress(agentops.Step{Name: name, State: agentops.StateOK, Detail: "done: " + name})
	}
	return nil
}

func (f *fakeOps) allCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// called is how many calls began with the words.
func (f *fakeOps) called(prefix string) int {
	n := 0
	for _, c := range f.allCalls() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeOps) Probe(ctx context.Context, host string, port int) (string, error) {
	f.note(fmt.Sprintf("Probe %s:%d", host, port))
	f.mu.Lock()
	f.probes = append(f.probes, fmt.Sprintf("%s:%d", host, port))
	fp := f.fingerprint
	f.mu.Unlock()
	if err := f.reach(ctx, "Probe"); err != nil {
		return "", err
	}
	return fp, nil
}

func (f *fakeOps) InstallAgent(ctx context.Context, node model.Node, login agentops.Login, progress agentops.Progress) (agentops.InstallResult, error) {
	f.note(fmt.Sprintf("InstallAgent %d", node.ID))
	f.mu.Lock()
	f.installs = append(f.installs, opsInstall{node, login})
	res, leak := f.install, f.leakLogin
	f.mu.Unlock()
	var spilt []string
	if leak {
		spilt = []string{login.Password, login.PrivateKey, login.Passphrase}
	}
	return res, f.steps(ctx, agentops.InstallPlan(), progress, spilt...)
}

func (f *fakeOps) Uninstall(ctx context.Context, node model.Node, login agentops.Login, progress agentops.Progress) error {
	f.note(fmt.Sprintf("Uninstall %d", node.ID))
	f.mu.Lock()
	f.uninstalls = append(f.uninstalls, opsInstall{node, login})
	f.mu.Unlock()
	return f.steps(ctx, agentops.UninstallPlan(), progress)
}

func (f *fakeOps) Connect(ctx context.Context, node model.Node) (Client, error) {
	f.note(fmt.Sprintf("Connect %d", node.ID))
	if err := f.reach(ctx, "Connect"); err != nil {
		return nil, err
	}
	return f.net.Connect(ctx, node)
}

func (f *fakeOps) Provision(ctx context.Context, c Client, in agentops.ProvisionInput, progress agentops.Progress) (agentops.ProvisionResult, error) {
	f.note(fmt.Sprintf("Provision %d", in.Node.ID))
	f.mu.Lock()
	f.provisions = append(f.provisions, in)
	target, check, leak := f.target, f.check, f.leakKeys
	f.mu.Unlock()
	if in.Target != "" {
		target = in.Target
	}
	var spilt []string
	if leak {
		spilt = []string{in.Keys.PrivateKey}
	}
	if err := f.steps(ctx, agentops.ProvisionPlan(), progress, spilt...); err != nil {
		return agentops.ProvisionResult{}, err
	}
	sni := in.SNI
	if sni == "" {
		sni = hostOf(target)
	}
	return agentops.ProvisionResult{
		HandshakeTarget: target, SNI: sni,
		PublicKey: in.Keys.PublicKey, PrivateKey: in.Keys.PrivateKey, ShortID: in.Keys.ShortID,
		AgentVersion: f.install.AgentVersion, XrayVersion: pin.Version, Check: check,
	}, nil
}

func (f *fakeOps) PushConfig(ctx context.Context, c Client, in nodecfg.Input) error {
	f.note("PushConfig")
	f.mu.Lock()
	f.pushes = append(f.pushes, opsPush{c, in})
	f.mu.Unlock()
	// the real one builds the config first, and refuses an input that can't make one
	if _, err := nodecfg.Build(in); err != nil {
		return err
	}
	err := f.reach(ctx, "PushConfig")
	f.mu.Lock()
	leak := f.leakKeys
	f.mu.Unlock()
	if err != nil && leak {
		err = &leaked{err, []string{in.PrivateKey}}
	}
	return err
}

func (f *fakeOps) CheckTarget(ctx context.Context, c Client, target string) (agentops.HandshakeCheck, error) {
	f.note("CheckTarget " + target)
	f.mu.Lock()
	f.checks = append(f.checks, opsCheck{c, target})
	chk, ok := f.checkFor[target]
	if !ok {
		chk = f.check
	}
	f.mu.Unlock()
	if err := f.reach(ctx, "CheckTarget"); err != nil {
		return agentops.HandshakeCheck{}, err
	}
	return chk, nil
}

func (f *fakeOps) UpdateAgent(ctx context.Context, c Client, node model.Node, progress agentops.Progress) (agentops.Versions, error) {
	f.note(fmt.Sprintf("UpdateAgent %d", node.ID))
	f.mu.Lock()
	f.updates = append(f.updates, opsUpdate{"agent", c, node})
	newAgent := f.newAgent
	f.mu.Unlock()
	if err := f.steps(ctx, agentops.UpdateAgentPlan(), progress); err != nil {
		return agentops.Versions{}, err
	}
	// the node's agent is the new one from now on, which its loop will read too
	ag := f.net.agent(node.ID)
	ag.set(func(a *fakeAgent) { a.agentVersion = newAgent })
	_, xrayLine := ag.versions()
	return agentops.Versions{Agent: newAgent, Xray: agentops.ParseXrayVersion(xrayLine)}, nil
}

func (f *fakeOps) UpdateXray(ctx context.Context, c Client, node model.Node, progress agentops.Progress) (agentops.Versions, error) {
	f.note(fmt.Sprintf("UpdateXray %d", node.ID))
	f.mu.Lock()
	f.updates = append(f.updates, opsUpdate{"xray", c, node})
	f.mu.Unlock()
	if err := f.steps(ctx, agentops.UpdateXrayPlan(), progress); err != nil {
		return agentops.Versions{}, err
	}
	ag := f.net.agent(node.ID)
	ag.set(func(a *fakeAgent) {
		a.xrayLine = "Xray " + pin.Version + " (Xray, Penetrates Everything.) 1b1647c (go1.26.1 linux/amd64)"
	})
	agentVersion, _ := ag.versions()
	return agentops.Versions{Agent: agentVersion, Xray: pin.Version}, nil
}

func (f *fakeOps) RenewCert(ctx context.Context, c Client, node model.Node) (agentops.CertInfo, error) {
	f.note(fmt.Sprintf("RenewCert %d", node.ID))
	f.mu.Lock()
	f.renewals = append(f.renewals, node)
	cert := f.cert
	f.mu.Unlock()
	if err := f.reach(ctx, "RenewCert"); err != nil {
		return agentops.CertInfo{}, err
	}
	return cert, nil
}

// versions is what the fake agent says it runs.
func (a *fakeAgent) versions() (agentVersion, xrayLine string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.agentVersion, a.xrayLine
}

// scriptedStore is the state with writes that fail when a test says so: the nth
// write from now on, or every one.
type scriptedStore struct {
	Store
	mu      sync.Mutex
	writes  int
	failAt  map[int]error // by the number of the write, counted from 1
	failAll error
}

func newScripted(st Store) *scriptedStore { return &scriptedStore{Store: st, failAt: map[int]error{}} }

func (f *scriptedStore) Update(fn func(*model.State) error) error {
	f.mu.Lock()
	f.writes++
	err := f.failAt[f.writes]
	if f.failAll != nil {
		err = f.failAll
	}
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return f.Store.Update(fn)
}

// failNext makes the write that comes n writes from now fail (1 is the next).
func (f *scriptedStore) failNext(n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAt[f.writes+n] = err
}

func (f *scriptedStore) setFailAll(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAll = err
}

// fakeDownloads is the project's releases: what the latest one carries for
// each architecture, and a count of the lookups and of the times what was
// downloaded was let go.
type fakeDownloads struct {
	mu       sync.Mutex
	versions map[string]string
	errs     map[string]error
	lookups  []string
	released int
	block    chan struct{} // a lookup waits until it is closed
}

func newDownloads() *fakeDownloads {
	return &fakeDownloads{versions: map[string]string{"arm64": "v1.3.0", "amd64": "v1.3.0"}, errs: map[string]error{}}
}

func (d *fakeDownloads) LatestAgent(ctx context.Context, arch string) (string, error) {
	d.mu.Lock()
	d.lookups = append(d.lookups, arch)
	v, err, block := d.versions[arch], d.errs[arch], d.block
	d.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return v, err
}

func (d *fakeDownloads) Release() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.released++
}

func (d *fakeDownloads) set(arch, version string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.versions[arch], d.errs[arch] = version, err
}

func (d *fakeDownloads) lookupCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.lookups)
}

func (d *fakeDownloads) releases() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.released
}

// opsRig is a manager with a healthy router, a fake bridge, a real job engine
// and the fake operations, in a synctest bubble.
type opsRig struct {
	*bridgeRig
	ops  *fakeOps
	dl   *fakeDownloads
	jobs *jobs.Engine
	// madeOps counts what NewOps was asked for, by the text of the CA
	madeOps []string
}

func newOpsRig(t *testing.T, mods ...func(*Config)) *opsRig {
	t.Helper()
	r := &opsRig{dl: newDownloads()}
	setup := func(c *Config) {
		r.jobs = jobs.New(jobs.Config{Now: c.Now, Log: c.Log})
		c.Jobs = r.jobs
		c.Downloads = r.dl
		c.NewOps = func(ca *pki.CA) Ops {
			r.madeOps = append(r.madeOps, string(ca.CertPEM()))
			return r.ops
		}
	}
	r.bridgeRig = newBridgeRig(t, append([]func(*Config){setup}, mods...)...)
	r.ops = newFakeOps(r.net)
	return r
}

// stop ends the jobs and then the manager, as the hub does.
func (r *opsRig) stop() {
	r.t.Helper()
	must(r.t, r.jobs.Close(context.Background()))
	r.rig.stop()
}

// finished is the job once it has ended: everything that can run has run.
func (r *opsRig) finished(id string) jobs.Job {
	r.t.Helper()
	synctest.Wait()
	j, ok := r.jobs.Get(id)
	if !ok {
		r.t.Fatalf("the engine doesn't know job %q", id)
	}
	if j.State == jobs.StateRunning {
		r.t.Fatalf("job %s is still running: %+v", id, j)
	}
	return j
}

// ok is a job that is expected to have worked.
func (r *opsRig) ok(id string) jobs.Job {
	r.t.Helper()
	j := r.finished(id)
	if j.State != jobs.StateOK {
		r.t.Fatalf("job %s ended %s: %s\nsteps %+v", id, j.State, j.Error, j.Steps)
	}
	return j
}

// failed is a job that is expected to have failed, with its error.
func (r *opsRig) failed(id string) jobs.Job {
	r.t.Helper()
	j := r.finished(id)
	if j.State != jobs.StateFailed {
		r.t.Fatalf("job %s ended %s, want it failed\nsteps %+v", id, j.State, j.Steps)
	}
	return j
}

// stepNames are the names of a job's steps, in order.
func stepNames(j jobs.Job) []string {
	out := make([]string, len(j.Steps))
	for i, s := range j.Steps {
		out[i] = s.Name
	}
	return out
}

// stepStates are the states of a job's steps, by name.
func stepStates(j jobs.Job) map[string]jobs.StepState {
	out := map[string]jobs.StepState{}
	for _, s := range j.Steps {
		out[s.Name] = s.State
	}
	return out
}

// concat joins plans into one list.
func concat(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}
