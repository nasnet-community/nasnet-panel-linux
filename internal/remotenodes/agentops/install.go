// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

// InstallResult is what the manager keeps from an install. It comes back with
// an error too, as far as the install got: Changed says whether the server
// was touched, which is when removal has something to clean.
type InstallResult struct {
	Changed  bool // the install may have put something on the server
	Repaired bool // an agent of this hub was there already, and was put in order
	Arch     string
	OS       string // as the server names it, "Debian GNU/Linux 12 (bookworm)"

	AgentVersion string // as the running agent reports it
	XrayVersion  string // empty until Xray is pushed
	NodeUUID     string

	// TookOver says the server's own Xray was stopped and (as far as the install
	// got) taken away, which a failure afterwards doesn't put back.
	TookOver bool
	// XrayKept says that Xray's binary was the pinned build and stays where it
	// was, so Provision neither fetches nor sends one.
	XrayKept bool

	CertSerial    string
	CertNotBefore time.Time
	CertNotAfter  time.Time
}

// InstallAgent puts the agent on a server over SSH, with a certificate of the
// hub's CA, and proves from the bound address that it answers over the link
// the hub will use and runs the managed profile. It is the first half of
// adding a node; Provision, over that link, is the second.
//
// It looks before it touches anything: the server's architecture, systemd,
// the tools it needs, whether the agent's port and the entry port are free,
// and whether an agent or an Xray is there already. An agent this plugin did
// not install stops it (ErrServerHasAgent), whether it trusts another hub's CA
// or nobody vouches for it, since a node belongs to one hub. So does an Xray
// this plugin did not install (ErrServerHasXray): its binary at the standard
// path, an xray.service unit or a running xray process, since the install
// would take over its paths and its service. One that this hub's marker
// vouches for is a repair and is put back in order, with its certificate,
// config and binary replaced. A failure afterwards leaves what it had done,
// which UndoInstall and Uninstall take away.
//
// The login, port, user and host key to hold the server to come from login and
// node. A login is used for this call and not kept. A server this hub's marker
// shows is already another of its nodes (Env.LiveNode) is refused
// (ErrServerIsNode): it is the same server under another address.
//
// With opts.TakeOverXray an Xray of the server's own is not refused but taken
// over, after the steps that only look and fetch: its service is stopped, and
// it, its config and its drop-ins are removed, with no copy kept. The binary
// stays when it is the pinned build (the same version and checksum), and is
// replaced otherwise. The marker then records the Xray as this plugin's, so a
// later removal takes it off like any other node's. The plan has the step
// (InstallPlanFor) only when it was asked for.
func (e *Env) InstallAgent(ctx context.Context, node model.Node, login Login, opts InstallOptions, progress Progress) (InstallResult, error) {
	var res InstallResult
	in := &install{e: e, node: node, login: login, opts: opts, st: newStepper(progress), res: &res}
	err := in.run(ctx)
	if in.s != nil {
		in.s.close()
	}
	err = scrub(err, login.secrets())
	if err != nil {
		e.log().Warn("installing the agent failed", "node", node.ID, "host", node.Host, "error", err)
	} else {
		e.log().Info("installed the agent", "node", node.ID, "host", node.Host, "version", res.AgentVersion, "repaired", res.Repaired)
	}
	return res, err
}

type install struct {
	e     *Env
	node  model.Node
	login Login
	opts  InstallOptions
	st    *stepper
	res   *InstallResult

	s    *session
	host string
	cert *pki.Issued
	ag   *artifacts.Agent

	state    serverState
	repair   bool
	foreign  *foreignXray // the server's own Xray, which a takeover is to remove
	listen   map[int][]string
	ufwOpen  []int // the ports to open in ufw
	mk       marker
	nodeUUID string

	retries2   string // net.ipv4.tcp_retries2 as the server has it, "" if it can't be read
	kernelNote string // why the kernel setting was not made, which is no failure, for the step to show
}

func (in *install) run(ctx context.Context) error {
	if err := in.checkInput(); err != nil {
		return err
	}
	for _, step := range []func(context.Context) error{
		in.check, in.download, in.certificate, in.takeOver, in.prepare, in.upload, in.start, in.verify,
	} {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (in *install) checkInput() error {
	host, err := model.CleanHost(in.node.Host)
	if err != nil {
		return err
	}
	in.host = host
	for _, p := range []struct {
		what string
		port int
	}{{"agent port", in.node.AgentPort}, {"entry port", in.node.EntryPort}} {
		if err := model.ValidPort(p.port); err != nil {
			return fmt.Errorf("%s: %w", p.what, err)
		}
	}
	if in.node.ID == 0 {
		return &model.InvalidError{Msg: "a node needs an id"}
	}
	switch {
	case in.e.CA == nil:
		return ErrNoCA
	case in.e.Files == nil:
		return ErrNoFiles
	}
	return nil
}

// check is step one: sign in, and read what the server is before anything is
// changed. Nothing here writes.
func (in *install) check(ctx context.Context) error {
	done := in.st.begin(StepCheckServer)
	detail, err := in.inspect(ctx)
	return done(detail, err)
}

func (in *install) inspect(ctx context.Context) (string, error) {
	var err error
	if in.s, err = in.e.openSession(ctx, in.node, in.login); err != nil {
		return "", err
	}
	s := in.s
	if !s.sh.IsRoot() && !s.has(ctx, cmdSudo) {
		return "", newErr(ErrNeedsRoot, nil, "This login isn't root, and the server has no sudo. Sign in as root, or as a user that can use sudo.")
	}

	out, err := s.run(ctx, "Couldn't read what the server is", cmdUname)
	if err != nil {
		return "", err
	}
	osName, machine := parseUname(out)
	arch, ok := archOf(machine)
	switch {
	case osName != "Linux":
		return "", newErr(ErrUnsupportedHost, nil, "This server runs %s. The agent needs Linux.", orUnknown(osName))
	case !ok:
		return "", newErr(ErrUnsupportedHost, nil, "This server's CPU (%s) isn't supported. The agent comes for 64-bit x86 (amd64) and ARM (arm64).", orUnknown(machine))
	case !s.has(ctx, cmdSystemd):
		return "", newErr(ErrUnsupportedHost, nil, "This server doesn't run systemd, which the agent needs to start at boot.")
	case !s.has(ctx, cmdGzip):
		return "", newErr(ErrUnsupportedHost, nil, "This server has no gzip, which the agent is unpacked with.")
	case !s.has(ctx, cmdSHA256):
		return "", newErr(ErrUnsupportedHost, nil, "This server has no sha256sum, which the upload is checked with.")
	}
	in.res.Arch = arch
	if out, err := s.run(ctx, "", cmdOSRelease); err == nil {
		in.res.OS = strings.TrimSpace(out)
	}

	if in.state, err = s.readState(ctx); err != nil {
		return "", err
	}
	mine, other := in.state.ownership(in.e.CA.Certificate())
	switch {
	case other:
		return "", newErr(ErrServerHasAgent, nil, "This server already runs an agent that trusts another hub. A node belongs to one hub: use a server without one, or remove that agent first.")
	case !mine && (in.state.present[agentBin] || in.state.present[agentUnitPath]):
		return "", newErr(ErrServerHasAgent, nil, "This server already runs a nasnet-agent that Remote Nodes did not install, such as a Linux panel's node. A node belongs to one hub: use a server without one.")
	}
	// this hub's marker for another of its nodes: the same server under another
	// address, which a failed install would clean away
	if mine && in.state.marker != nil && in.state.marker.NodeID != in.node.ID && in.e.LiveNode != nil {
		if name, ok := in.e.LiveNode(in.state.marker.NodeID); ok {
			return "", &ServerIsNodeError{NodeID: in.state.marker.NodeID, Name: name}
		}
	}
	// only the marker vouches for an Xray: the CA file alone says nothing of it
	if in.state.marker == nil || !mine {
		if err := in.checkXray(ctx); err != nil {
			return "", err
		}
	}
	in.repair = mine
	in.res.Repaired = mine

	in.listen = s.listeners(ctx)
	if err := in.checkPorts(); err != nil {
		return "", err
	}
	if err := in.planFirewall(ctx); err != nil {
		return "", err
	}
	// what the kernel setting is now, for removal to give back
	out, _ = s.run(ctx, "", cmdReadRetries2)
	in.retries2 = retries2Value(out)
	in.plan()

	detail := arch
	if in.res.OS != "" {
		detail += " · " + in.res.OS
	}
	if in.repair {
		detail += " · agent already installed"
	}
	if in.foreign != nil {
		detail += " · " + in.foreign.planned()
	}
	return detail, nil
}

// checkXray stops the install on a server that runs an Xray of its own, which
// the install would take over: it writes Xray's binary, config and service at
// the standard paths, and removing the node would take that one away. It says
// what it found, and which build the binary is. When the install was asked to
// take the Xray over, it reads what that would remove and goes on instead.
func (in *install) checkXray(ctx context.Context) error {
	fx, err := in.lookForXray(ctx)
	if err != nil || fx == nil {
		return err
	}
	if !in.opts.TakeOverXray {
		return &fx.ServerHasXrayError
	}
	if err := in.readTakeover(ctx, fx); err != nil {
		return err
	}
	in.foreign = fx
	return nil
}

func orUnknown(s string) string {
	if s == "" {
		return "something else"
	}
	return s
}

// checkPorts stops the install when a port it needs is taken. A repair of
// this hub's own agent expects its own processes there: the agent on its port
// and Xray on the entry port. So does a takeover, for the Xray that is about to
// be stopped, which the server names as xray. Anything else named on one of
// them is in the way (a docker-proxy, say), and so is anything at all on a
// server with no agent of ours, and a holder the server won't name.
func (in *install) checkPorts() error {
	for _, p := range []struct {
		port      int
		role, own string
	}{
		{in.node.AgentPort, "agent", "nasnet-agent"},
		{in.node.EntryPort, "entry", "xray"},
	} {
		procs, busy := in.listen[p.port]
		if !busy {
			continue
		}
		var foreign []string
		for _, name := range procs {
			if name != p.own && !(name == "xray" && in.foreign != nil) {
				foreign = append(foreign, name)
			}
		}
		// a takeover only lets pass what it saw named: a holder it can't tell is not its Xray
		if len(foreign) == 0 && (in.repair || (in.foreign != nil && len(procs) > 0)) {
			continue
		}
		return &PortInUseError{Port: p.port, Role: p.role, Process: strings.Join(foreign, ", ")}
	}
	return nil
}

// planFirewall decides which ports to open in ufw. Only an active ufw is
// touched, and only for a port it doesn't allow already.
func (in *install) planFirewall(ctx context.Context) error {
	s := in.s
	if !s.has(ctx, cmdUFW) {
		return nil
	}
	out, err := s.root(ctx, "Couldn't read the firewall", cmdUFWStatus, nil)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "Status: active") {
		return nil
	}
	allowed := allowedByUFW(out)
	for _, p := range []int{in.node.AgentPort, in.node.EntryPort} {
		if !slices.Contains(allowed, p) {
			in.ufwOpen = append(in.ufwOpen, p)
		}
	}
	return nil
}

// plan works out what the marker will say. A flag is set for what the server
// didn't have, so that removal never takes what was someone else's. A repair
// keeps what the first install recorded: the Xray it pushed is there now, and
// would otherwise look like a foreign one.
func (in *install) plan() {
	old := marker{}
	if in.state.marker != nil && in.repair {
		old = *in.state.marker
	}
	absent := func(path string) bool { return !in.state.present[path] }
	// a repair keeps what the first install saw: the setting it reads now is the install's own
	retries2Was := old.TCPRetries2Was
	if retries2Was == "" {
		retries2Was = in.retries2
	}
	// an Xray that is to be taken over is the plugin's from then on: its binary
	// (kept or replaced), its unit and its config are what the install puts there
	takeover := in.foreign != nil
	in.mk = marker{
		Version:     markerVersion,
		NodeID:      in.node.ID,
		CASHA256:    caFingerprint(in.e.CA.Certificate()),
		InstalledAt: in.e.now().UTC().Truncate(time.Second),
		AgentPort:   in.node.AgentPort,
		EntryPort:   in.node.EntryPort,
		Agent:       true,
		XrayBinary:  old.XrayBinary || absent(xrayBin) || takeover,
		XrayUnit:    old.XrayUnit || absent(xrayUnitPath) || takeover,
		XrayConfig:  old.XrayConfig || absent(xrayConfPath) || takeover,
		Firewall:    union(old.Firewall, in.ufwOpen),

		XrayTakenOver: old.XrayTakenOver || takeover,
		XrayWas:       old.XrayWas,

		TCPRetries2Was: retries2Was,
	}
	if takeover && in.foreign.Version != "" {
		in.mk.XrayWas = in.foreign.Version
	}
	if !old.InstalledAt.IsZero() {
		in.mk.InstalledAt = old.InstalledAt
	}
}

// download is step two: the agent for the server's architecture, into memory.
// Nothing has been changed on the server yet, so a release that is too old or
// a network that fails costs nothing.
func (in *install) download(ctx context.Context) error {
	done := in.st.begin(StepDownloadAgent)
	ag, err := in.e.Files.Agent(ctx, in.res.Arch)
	if err != nil {
		return done("", plainErr(err))
	}
	in.ag = ag
	return done(fmt.Sprintf("%s · %s", ag.Version, byteSize(int64(len(ag.Gz)))), nil)
}

// certificate is step three: the node's own, for its address, from the CA.
func (in *install) certificate(ctx context.Context) error {
	done := in.st.begin(StepCertificate)
	is, err := in.e.CA.IssueNodeCert(in.node.ID, in.node.Name, in.host, in.e.now())
	if err != nil {
		return done("", plainErr(err))
	}
	in.cert = is
	in.res.CertSerial, in.res.CertNotBefore, in.res.CertNotAfter = is.Serial, is.NotBefore, is.NotAfter
	return done("Valid until "+is.NotAfter.Format("2006-01-02"), nil)
}

// prepare is step four: the marker first, so that anything after it can be
// taken away, then the directories, the config and the certificates. Each
// file comes from stdin into a file of its own that only root can read, and
// moves into place once whole. The key of the node's certificate goes to the
// node here and is dropped. The kernel setting for the tunnel comes last, and
// if the server won't take it the step still ends well, with the reason as its
// detail.
func (in *install) prepare(ctx context.Context) error {
	done := in.st.begin(StepPrepareServer)
	err := in.writeAll(ctx)
	if err != nil {
		return done("", err)
	}
	return done(in.kernelNote, nil)
}

func (in *install) writeAll(ctx context.Context) error {
	s := in.s
	in.res.Changed = true // from here on the server may be different, whatever happens to the next command
	if _, err := s.root(ctx, "Couldn't create "+dataDir, cmdDirs(dataDir), nil); err != nil {
		return err
	}
	if err := in.writeMarker(ctx); err != nil {
		return err
	}
	// Xray's config folder too: the agent checks a config by writing a file beside it
	if _, err := s.root(ctx, "Couldn't create the agent's folders", cmdDirs(confDir, logDir, xrayConfDir), nil); err != nil {
		return err
	}

	id, err := newUUID()
	if err != nil {
		return err
	}
	in.nodeUUID = id
	in.res.NodeUUID = id
	for _, f := range []struct {
		path, mode string
		data       []byte
	}{
		{configPath, "0600", renderAgentConfig(id, in.node.AgentPort, nodecfg.DefaultAPIPort)},
		{caPath, "0644", in.e.CA.CertPEM()},
		{certPath, "0644", in.cert.CertPEM},
		{keyPath, "0600", in.cert.KeyPEM},
	} {
		if err := s.write(ctx, f.path, f.mode, f.data); err != nil {
			return err
		}
	}
	clear(in.cert.KeyPEM) // it lives on the node now, and nowhere else
	in.cert.KeyPEM = nil
	return in.tuneKernel(ctx)
}

// tuneKernel makes the server give up a connection whose other end has gone
// quiet after about a minute and not fifteen. The tunnel's connection from the
// router is such a one when the router's address changes: the hub restarts
// Xray when it learns of that, and this is what bounds the time when it
// doesn't. The setting is made live and kept in a file that systemd reads at
// boot; removal puts it back.
//
// It helps and the node doesn't need it, so a server that won't take it (a
// container whose kernel settings are read-only) is no failed install: the
// reason goes to the step's detail and what was not made is left out. Only a
// job that is cancelled stops here.
func (in *install) tuneKernel(ctx context.Context) error {
	if in.retries2 == "" {
		in.kernelNote = "This server has no net.ipv4.tcp_retries2 setting that can be read, so it was left alone."
		return nil
	}
	err := in.keepRetries2(ctx)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	in.e.log().Warn("setting tcp_retries2 on the node failed; the install goes on", "node", in.node.ID, "error", err)
	in.kernelNote = sentence(err.Error()) + " The node works, and takes longer to drop a tunnel connection that has gone away."
	return nil
}

// keepRetries2 makes the value live first: a server that refuses it has
// nothing worth keeping across a reboot.
func (in *install) keepRetries2(ctx context.Context) error {
	s := in.s
	if _, err := s.root(ctx, "Couldn't set net.ipv4.tcp_retries2", cmdSetRetries2, nil); err != nil {
		return err
	}
	if _, err := s.root(ctx, "Couldn't make "+sysctlDir, cmdMkSysctlDir, nil); err != nil {
		return err
	}
	return s.write(ctx, sysctlPath, "0644", []byte(sysctlText))
}

func (in *install) writeMarker(ctx context.Context) error {
	return in.s.write(ctx, markerPath, "0644", in.mk.bytes())
}

// upload is step five: the gzipped agent streams through gzip on the server,
// whose own hash of what came out is compared with the release's. Only then
// is it made executable and moved into place.
func (in *install) upload(ctx context.Context) error {
	done := in.st.begin(StepUploadAgent)
	m := newMeter(in.st, StepUploadAgent, int64(len(in.ag.Gz)), in.e.now)
	if err := in.send(ctx, m); err != nil {
		return done("", err)
	}
	return done(m.summary()+" sent", nil)
}

func (in *install) send(ctx context.Context, m *meter) error {
	s := in.s
	if _, err := s.exec(ctx, uploadTimeout, "Couldn't upload the agent", func(c context.Context) (string, error) {
		return s.sh.Root(c, cmdUpload, m.reader(bytes.NewReader(in.ag.Gz)))
	}); err != nil {
		return err
	}
	out, err := s.root(ctx, "Couldn't check the upload", cmdHash, nil)
	if err != nil {
		return err
	}
	if f := strings.Fields(out); len(f) == 0 || !strings.EqualFold(f[0], in.ag.PlainSHA256) {
		s.root(ctx, "", "rm -f "+shq(agentUpload), nil)
		return newErr(ErrUploadCorrupt, nil, "The agent arrived damaged: its checksum on the server isn't the one in the release. Try again.")
	}
	if _, err := s.root(ctx, "Couldn't put the agent in place", cmdInstallBin, nil); err != nil {
		return err
	}
	if _, err := s.root(ctx, "The agent can't run on this server", cmdAgentRuns, nil); err != nil {
		return err
	}
	return nil
}

// start is step six: the units, the firewall and the service. The agent
// listening on its port is what shows it came up.
func (in *install) start(ctx context.Context) error {
	done := in.st.begin(StepStartAgent)
	return done("", in.bringUp(ctx))
}

func (in *install) bringUp(ctx context.Context) error {
	s := in.s
	if err := s.write(ctx, agentUnitPath, "0644", []byte(agentUnitText)); err != nil {
		return err
	}
	// the service the agent starts Xray through, where the server has none
	if in.mk.XrayUnit {
		if err := s.write(ctx, xrayUnitPath, "0644", []byte(xrayUnitText)); err != nil {
			return err
		}
	}
	for _, p := range in.ufwOpen {
		if _, err := s.root(ctx, fmt.Sprintf("Couldn't open port %d in the firewall", p), cmdUFWAllow(p), nil); err != nil {
			return err
		}
	}
	if _, err := s.root(ctx, "Couldn't start the agent", cmdStart, nil); err != nil {
		return err
	}
	return in.waitListening(ctx)
}

// waitListening waits for the service to be active with its port open. If
// it isn't in time, the server's log says why.
func (in *install) waitListening(ctx context.Context) error {
	wait := in.e.startWait
	if wait == 0 {
		wait = 30 * time.Second
	}
	s := in.s
	err := poll(ctx, wait, 300*time.Millisecond, func(ctx context.Context) (bool, error) {
		out, _ := s.run(ctx, "", cmdIsActive)
		if strings.TrimSpace(out) != "active" {
			return false, nil
		}
		_, up := s.listeners(ctx)[in.node.AgentPort]
		return up, nil
	})
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	msg := "The agent did not start on the server."
	if log, _ := s.root(ctx, "", cmdJournal, nil); strings.TrimSpace(log) != "" {
		msg += " Its log says:\n" + tail(log, 600)
	}
	return newErr(ErrAgentNotStarted, err, "%s", msg)
}

// tail is the last n bytes of s, cut at a line.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// verify is step seven: the hub's own link to the agent, from the bound
// address. A server whose firewall keeps the router out shows up here, and
// is said to, with the port to open.
func (in *install) verify(ctx context.Context) error {
	done := in.st.begin(StepCheckAgent)
	check := in.e.verify
	if check == nil {
		check = in.e.verifyAgent
	}
	v, err := check(ctx, in.node, in.ag)
	if err != nil {
		return done("", err)
	}
	in.res.AgentVersion, in.res.XrayVersion = v.Agent, v.Xray
	return done(v.Agent+", managed profile", nil)
}

// plainErr makes an error from another package a sentence the page can show,
// and keeps it matching what it was.
func plainErr(err error) error {
	if err == nil || ctxDone(err) {
		return err
	}
	return &opError{msg: sentence(tidyCause(err)), cause: err}
}

func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
