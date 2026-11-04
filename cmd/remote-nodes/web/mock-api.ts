// A fake plugin API for `npm run dev:mock`: the whole page runs with sample data
// and no router, VPS or agent. Development only; never built into the page.
//
// It follows the contract in the plan (HTTP API, v1): config with the entry state,
// probe, jobs with their event stream, nodes (list, detail, install), and the
// subscription lists the node cards read. Other calls answer 501.
//
// Sample data matches the design boards: Node 1 works (3 of its 6 subscriptions
// are online), Node 2 can't be reached (its agent and Xray are due an update),
// Node 3 is installing, frozen at step 10 of 15 (62% through the Xray upload).
// Seven subscriptions, the entry is on (WireGuard). The router is at its cap of
// 3 nodes; remove one (or reset with { nodes: 2 }) to add another. The job tray
// starts with that install and two finished jobs. Usage moves every few seconds.
//
// `npm run dev:mock:fresh` starts from the first-run state instead: no nodes, no
// subscriptions, the entry off because there is no Domestic link and no VPN.
//
// Add-node scenarios, picked by the address you type:
//   address contains "offline" -> the host key check can't connect
//   address contains "changed" -> the host key differs from last time
//   address contains "fail"    -> fails at "Check the agent" (dial timeout), then cleans the server up
//   address contains "unreach" -> the same, and the job has the code node_unreachable, which the page words
//   address contains "late"    -> fails at "Choose a Reality target", then cleans the server up
//   address contains "notarget"-> the node chose among its sites and none worked: fails at "Choose a Reality target"
//                                 with the code reality_target_failed (and the number tried), then cleans the server up
//   handshakeTarget containing "fail" or "big" (a refused TLS 1.3 handshake, or one over 8 KB) -> fails at
//                                 "Choose a Reality target" with the code reality_target_failed, the target and the
//                                 node's reason, then cleans the server up; any other target is used as it is
//   address contains "hasxray" -> refused at "Check the server": it runs an Xray of its own (server_has_xray),
//                                 version 26.9.1 and not the pinned build ("hasxraysame": the pinned build);
//                                 with takeOverXray in the request the install takes it over and goes on
//                                 ("notstopped" too: the first takeover finds an Xray still running, xray_not_stopped,
//                                 and the next try works, as if the person had stopped it)
//   address contains "hasagent"-> refused at "Check the server": it runs another agent (server_has_agent)
//   address contains "isnode"  -> refused at "Check the server": it is Node 1 already (server_is_node)
//   address contains "portbusy"-> refused at "Check the server" while the entry port is 443: nginx has it (port_in_use)
//   address contains "agentbusy"-> refused at "Check the server": the agent's port is held (port_in_use, role agent)
//   address contains "nosudo"  -> refused at "Check the server" unless the user is root (needs_root)
//   address contains "keymoved"-> refused at "Check the server" until the key the server shows now is trusted (host_key_changed)
//   password "wrong"           -> refused at "Check the server": the server doesn't accept the login (login_refused)
//   address contains "stuck"   -> fails at "Upload the agent", and the cleanup fails too: the install is
//                                 kept as unfinished, which only Clean up or Forget takes out
//   (as the hub does, a failed install leaves no node behind unless its cleanup failed too)
//   address contains "poll"    -> the job's event stream answers 503, so the page polls
//   an existing address        -> refused: it is already a node here
//
// The e2e reset (POST /api/__reset) takes { fresh, nodes, maxNodes, entry, pace, unfinished }:
//   fresh     the first-run state above
//   nodes     0 to 3: how many of the sample nodes stay (1 keeps only Node 1)
//   unfinished true: an install that didn't finish beside them ("Old install", 192.0.2.77, id 9)
//   maxNodes  the router's cap (3)
//   entry     "off", "domestic-offline", "vpn-offline", "stale" or "old-routeros"
//   pace      how long install steps take, as a multiple of the usual (1)
// Every call but the job event stream needs the page's X-RemoteNodes-UI header, as the hub
// does; the stream can't carry one (EventSource), so it is refused only when the browser says
// the request came from another site. GET /licenses, which the Notices link opens in a new
// tab, is plain text and needs none, as on the hub; the text is a short sample.
import type { IncomingMessage, ServerResponse } from 'node:http'
import type { Connect, Plugin } from 'vite'
import { nodeApi } from './mock-node'
import type { AgentState, AppConfig, Entry, ErrorInfo, Job, JobKind, Node, NodeEvent, NodeSubscription, Subscription } from './src/lib/api'
import { subscriptionRoutes } from './mock-subscriptions'

const DAY = 86400e3
const H = 3600e3
const MIN = 60e3
const iso = (ms: number) => new Date(ms).toISOString().replace(/\.\d+Z$/, 'Z')
const hex = (n: number) => Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('')
const rand = (lo: number, hi: number) => lo + Math.floor(Math.random() * (hi - lo + 1))
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
const GB = 1e9
const MB = 1e6

const VERSION = '0.1.0'
// what an update installs: the versions this plugin pins
const AGENT = '1.3.0'
const XRAY = '26.10.21'
const NEW_KEY = 'SHA256:u3Rq0Q1uV3Jc0mYw7b6o8xk3j2m9Pq5c1f0aZyXwVb8'
const OLD_KEY = 'SHA256:Zk9wP2mA7sQ1tR4yU6iO8pLhJgFdSaXcVbNmQwErTyU'
const MAX_SUBSCRIPTIONS = 10
/** The longest a name may be, in characters, and what the hub says of one that is longer. */
export const MAX_NAME = 64
export const tooLong = (name: string) => [...name].length > MAX_NAME
export const NAME_TOO_LONG = `The name can have at most ${MAX_NAME} characters.`

export interface MockNode {
  id: number
  name: string
  host: string
  sshPort: number
  sshUser: string
  /** the key pinned at install */
  hostKey: string
  /** false until the install has saved the node */
  registered: boolean
  createdAt: number
  entryPort: number
  handshakeTarget: string
  arch: string
  agentVersion: string
  xrayVersion: string
  /** the router can't reach the agent */
  down: boolean
  lastSeen: number
  agentStart: number
  /** the install job, while it runs */
  installJob?: string
  /** an install that stopped and wasn't cleaned up: no node, and only Clean up or Forget work */
  failure?: { step?: { key: string; name: string }; error: string; cleanup: string; at: number }
  rate: { up: number; down: number }
  today: { up: number; down: number }
  month: { up: number; down: number }
  events: NodeEvent[]
}

export interface MockSub {
  id: number
  name: string
  createdAt: number
  /** the node it has connections on now, if any */
  onlineOn: number | null
  conns: number
  lastSeen: number | null
  today: number
  month: number
  /** the nodes it is switched on for */
  on: Set<number>
  /** the traffic limit in bytes, 0 for none; `used` is what it has moved since its count was reset */
  limit: number
  used: number
  /** when it ends, if it does */
  endsAt: number | null
}

/** Why the hub keeps a subscription off the nodes, if it does: the date wins over the limit. */
export function stoppedOf(s: MockSub, now = Date.now()): 'limit' | 'expired' | '' {
  if (s.endsAt !== null && s.endsAt <= now) return 'expired'
  return s.limit > 0 && s.used >= s.limit ? 'limit' : ''
}

const split = (total: number) => {
  const up = Math.round(total * 0.25)
  return { up, down: Math.round(total) - up }
}

// The steps the hub reports: its English name, and the key it sends beside it, which the page words
// the step by (internal/remotenodes/manager/stepkeys.go has the real table).
export const STEPS = {
  checkServer: { key: 'check_server', name: 'Check the server' },
  downloadAgent: { key: 'download_agent', name: 'Download the agent' },
  createCertificate: { key: 'create_certificate', name: 'Create the certificate' },
  prepareServer: { key: 'prepare_server', name: 'Prepare the server' },
  uploadAgent: { key: 'upload_agent', name: 'Upload the agent' },
  startAgent: { key: 'start_agent', name: 'Start the agent' },
  checkAgent: { key: 'check_agent', name: 'Check the agent' },
  connectAgent: { key: 'connect_agent', name: 'Connect to the agent' },
  downloadXray: { key: 'download_xray', name: 'Download Xray' },
  uploadXray: { key: 'upload_xray', name: 'Upload Xray' },
  checkXray: { key: 'check_xray', name: 'Check Xray' },
  chooseTarget: { key: 'choose_target', name: 'Choose a Reality target' },
  createKeys: { key: 'create_keys', name: 'Create the keys' },
  applyConfig: { key: 'apply_config', name: 'Apply the configuration' },
  saveNode: { key: 'save_node', name: 'Save the node' },
  stopAgent: { key: 'stop_agent', name: 'Stop the agent' },
  removeAgent: { key: 'remove_agent', name: 'Remove the agent' },
  restartAgent: { key: 'restart_agent', name: 'Restart the agent' },
  renewCert: { key: 'renew_cert', name: 'Renew the certificate' },
  forgetNode: { key: 'forget_node', name: 'Forget the node' },
  cleanServer: { key: 'clean_server', name: 'Clean up the server' },
  discardNode: { key: 'discard_node', name: 'Discard the node' },
  takeOverXray: { key: 'take_over_xray', name: "Take over the server's Xray" },
} as const

export let nodes: MockNode[] = []
export let subs: MockSub[] = []
let nextNodeId = 1
/** The next node id, or `id` kept clear of later installs when it is given. */
export const claimNodeId = (id?: number) => (id === undefined ? nextNodeId++ : ((nextNodeId = Math.max(nextNodeId, id + 1)), id))
let nextSubId = 1
let maxNodes = 3
let entryMode = 'on'
let pace = 1

export function node(o: Partial<MockNode> & Pick<MockNode, 'id' | 'name' | 'host'>): MockNode {
  const now = Date.now()
  return {
    sshPort: 22,
    sshUser: 'root',
    hostKey: `SHA256:${hex(43)}`,
    registered: true,
    createdAt: now - 9 * DAY,
    entryPort: 443,
    handshakeTarget: 'www.example.com:443',
    arch: 'amd64',
    agentVersion: AGENT,
    xrayVersion: XRAY,
    down: false,
    lastSeen: now,
    agentStart: now - 6 * DAY - 4 * H,
    rate: { up: 0, down: 0 },
    today: { up: 0, down: 0 },
    month: { up: 0, down: 0 },
    events: [],
    ...o,
  }
}

export const event = (n: MockNode, kind: string, detail = '', at = Date.now()) => {
  n.events.push({ at: iso(at), kind, detail })
  n.events.sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
}

function sub(name: string, o: Partial<MockSub> = {}): MockSub {
  const month = o.month ?? 0
  return { id: nextSubId++, name, createdAt: Date.now() - 20 * DAY, onlineOn: null, conns: 0, lastSeen: null, today: 0, month, on: new Set(), limit: 0, used: month, endsAt: null, ...o }
}

function seed(o: { fresh?: boolean; nodes?: number; maxNodes?: number; entry?: string; pace?: number; unfinished?: boolean } = {}) {
  const now = Date.now()
  nodes = []
  subs = []
  stoppedElsewhere.clear()
  nextNodeId = 1
  nextSubId = 1
  maxNodes = o.maxNodes ?? 3
  pace = o.pace ?? 1
  entryMode = o.fresh ? 'off' : (o.entry ?? 'on')
  jobs.clear()
  installing.clear()
  if (o.fresh) return

  const keep = o.nodes ?? 3
  const n1 = node({
    id: 1,
    name: 'Node 1',
    host: '203.0.113.24',
    hostKey: 'SHA256:p2OiwWbYoEAsmmYWEMy1L12ya7lUDzuHYODmtJ18R/w',
    rate: { up: 0.3 * MB, down: 1.2 * MB },
    today: { up: 1.0 * GB, down: 3.1 * GB },
    month: { up: 28 * GB, down: 94 * GB },
  })
  event(n1, 'tunnel_up', '', n1.agentStart + 2000)
  event(n1, 'agent_updated', '1.2.0 to 1.3.0', now - DAY)
  event(n1, 'tunnel_up', 'after 2 s', now - 2 * H)

  const n2 = node({
    id: 2,
    name: 'Node 2',
    host: '198.51.100.7',
    hostKey: 'SHA256:FkImfecZh+m0r8bQdJoD77DxMU9mViXNaqX/kA9ZISU',
    agentVersion: '1.2.0',
    xrayVersion: '26.10.18',
    down: true,
    lastSeen: now - 22 * MIN,
    createdAt: now - 3 * DAY,
  })
  event(n2, 'tunnel_up', '', n2.createdAt + 3 * MIN)
  event(n2, 'node_unreachable', 'no answer over the Domestic link', n2.lastSeen)

  const n3 = node({ id: 3, name: 'Node 3', host: '192.0.2.41', registered: false, createdAt: now - 62e3, installJob: 'seed-install-3' })

  const wanted = [n1, n2, n3].slice(0, keep)
  nodes = wanted
  nextNodeId = wanted.length + 1
  if (o.unfinished) {
    nodes.push(stuck({ id: 9, name: 'Old install', host: '192.0.2.77', hostKey: 'SHA256:Old1nstallKeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA', createdAt: now - 2 * H }, now - 2 * H + 90e3))
    nextNodeId = 10
  }

  const names: [string, number[], Partial<MockSub>][] = [
    ['Sara', [1, 2], { onlineOn: 1, conns: 3, lastSeen: now, today: 1.2 * GB, month: 38 * GB, limit: 100 * GB, used: 62 * GB, endsAt: now + 41 * DAY }],
    ['Ali', [1], { onlineOn: 1, conns: 2, lastSeen: now, today: 640 * MB, month: 21 * GB }],
    ['Reza', [1, 2], { onlineOn: 1, conns: 4, lastSeen: now, today: 1.9 * GB, month: 52 * GB }],
    ['Mina', [1], { lastSeen: now - 3 * H, today: 310 * MB, month: 9.4 * GB }],
    ['Parsa', [1], { lastSeen: now - DAY, month: 1.1 * GB }],
    ['Dana', [1, 2], { lastSeen: now - 2 * DAY, month: 600 * MB }],
    ['Kian', [2], { lastSeen: now - 25 * MIN, today: 210 * MB, month: 5.2 * GB }],
  ]
  subs = names.map(([name, on, rest]) => sub(name, { ...rest, on: new Set(on.filter((id) => wanted.some((n) => n.id === id))) }))

  // what the tray shows from before: Node 3's install, still going, and two finished jobs
  if (wanted.includes(n3)) {
    jobs.set('seed-install-3', {
      view: {
        id: 'seed-install-3',
        kind: 'install',
        state: 'running',
        nodeId: 3,
        target: 'Node 3',
        startedAt: iso(now - 62e3),
        steps: [
          { ...STEPS.checkServer, state: 'ok', detail: 'amd64 · Debian GNU/Linux 12 (bookworm)' },
          { ...STEPS.downloadAgent, state: 'ok', detail: `v${AGENT} · 18 MB` },
          { ...STEPS.createCertificate, state: 'ok', detail: 'Valid until 2027-10-31' },
          { ...STEPS.prepareServer, state: 'ok' },
          { ...STEPS.uploadAgent, state: 'ok', detail: '18.0 MB sent' },
          { ...STEPS.startAgent, state: 'ok' },
          { ...STEPS.checkAgent, state: 'ok', detail: `v${AGENT}, managed profile` },
          { ...STEPS.connectAgent, state: 'ok' },
          { ...STEPS.downloadXray, state: 'ok', detail: `${XRAY} · 13.6 MB` },
          { ...STEPS.uploadXray, state: 'running', detail: '62% · 8.4 of 13.6 MB' },
          { ...STEPS.checkXray, state: 'pending' },
          { ...STEPS.chooseTarget, state: 'pending' },
          { ...STEPS.createKeys, state: 'pending' },
          { ...STEPS.applyConfig, state: 'pending' },
          { ...STEPS.saveNode, state: 'pending' },
        ],
      },
      listeners: new Set(),
      noStream: false,
    })
  }
  const finished = (id: string, kind: JobKind, nodeId: number, target: string, ago: number, took: number): [string, MockJob] => [
    id,
    {
      view: { id, kind, state: 'ok', nodeId, target, startedAt: iso(now - ago - took), finishedAt: iso(now - ago), steps: [{ ...STEPS.checkServer, state: 'ok' }, { ...STEPS.saveNode, state: 'ok' }] },
      listeners: new Set(),
      noStream: false,
    },
  ]
  if (keep >= 2) jobs.set(...finished('seed-node-2', 'install', 2, 'Node 2', 3 * DAY, 168e3))
  if (keep >= 1) jobs.set(...finished('seed-update-1', 'update-agent', 1, '', DAY, 41e3))
}

/** An install that stopped at the agent's upload and whose cleanup couldn't reach the server either. */
function stuck(o: Partial<MockNode> & Pick<MockNode, 'id' | 'name' | 'host'>, at: number): MockNode {
  return node({
    ...o,
    registered: false,
    agentVersion: '',
    xrayVersion: '',
    failure: {
      step: STEPS.uploadAgent,
      error: "Couldn't upload the agent: the connection broke.",
      cleanup: `Can't reach SSH on ${o.host} port 22 (dial tcp ${o.host}:22: i/o timeout). Check the address and the port, and that the server's firewall lets the router in.`,
      at,
    },
  })
}

/** A record that is a node: set up, or with its install running. */
export const isNode = (n: MockNode) => !n.failure

// --- views ---

const reachable = (n: MockNode) => !n.down && n.registered

/** Subscriptions switched on at a node, those with a connection there now, and their connections. */
function present(n: MockNode) {
  const enabled = subs.filter((s) => s.on.has(n.id))
  const online = enabled.filter((s) => s.onlineOn === n.id)
  return { enabled: enabled.length, online: online.length, connections: online.reduce((sum, s) => sum + s.conns, 0) }
}

function tunnelView(n: MockNode): Node['tunnel'] {
  const base = { since: null, rttMs: 0, streams: 0, carried: 0, bytesToSubs: 0, bytesFromSubs: 0, dialFailures: 0 }
  if (n.installJob || !n.registered) return { ...base, state: 'off', reason: '' }
  if (n.down) return { ...base, state: 'off', reason: 'agent_down' }
  if (n.xrayVersion !== XRAY) return { ...base, state: 'off', reason: 'xray_mismatch' }
  if (entryMode !== 'on' && entryMode !== 'stale') return { ...base, state: 'off', reason: 'entry_off' }
  const here = present(n)
  return { ...base, state: 'up', since: iso(n.agentStart), rttMs: 23, streams: here.connections, carried: 412 * here.online, bytesToSubs: 1.9e12, bytesFromSubs: 412 * GB, reason: '' }
}

function agentView(n: MockNode): AgentState | null {
  if (!n.registered || n.installJob) return null
  // a down node still shows its last report
  const at = n.down ? n.lastSeen : Date.now() - 1000
  return {
    version: n.agentVersion,
    xrayVersion: n.xrayVersion,
    uptimeSec: Math.floor((at - n.agentStart) / 1000),
    rssBytes: 22_300_000,
    reportedAt: iso(at),
    xray: { running: true, restarts: 0, uptimeSec: Math.floor((at - n.agentStart) / 1000), lastExit: '', crashLoop: false },
    settingsApplied: true,
    lastPushError: '',
  }
}

function installingView(n: MockNode): Node['installing'] {
  const j = n.installJob ? jobs.get(n.installJob)?.view : undefined
  if (!j || j.state !== 'running') return undefined
  const i = j.steps.findIndex((s) => s.state === 'running')
  const step = j.steps[i]
  const m = step?.detail ? /^(\d{1,3})%/.exec(step.detail) : null
  return {
    jobId: j.id,
    step: i < 0 ? j.steps.filter((s) => s.state === 'ok').length + 1 : i + 1,
    steps: j.steps.length,
    key: step?.key ?? '',
    label: step?.name ?? '',
    percent: m ? Number(m[1]) : null,
  }
}

export function view(n: MockNode, detail = false): Node {
  const live = reachable(n) && !n.installJob
  const here = present(n)
  const out: Node = {
    id: n.id,
    name: n.name,
    host: n.host,
    sshPort: n.sshPort,
    sshUser: n.sshUser,
    registered: n.registered,
    createdAt: iso(n.createdAt),
    entryPort: n.entryPort,
    handshakeTarget: n.handshakeTarget,
    sni: n.handshakeTarget.split(':')[0],
    sniCustom: false,
    publicKey: 'x7QnLw4Rk2VtB9yD0sEoUj3HaFcN6mXz8PqIg1KvTbA',
    shortId: 'a1b2c3d4',
    arch: n.arch,
    agentVersion: n.installJob ? '' : n.agentVersion,
    xrayVersion: n.installJob ? '' : n.xrayVersion,
    lastSeenAt: n.installJob ? null : n.down ? iso(n.lastSeen) : iso(Date.now() - 3000),
    subscriptionCount: here.enabled,
    tunnel: tunnelView(n),
    agent: agentView(n),
    cert: { notAfter: iso(n.createdAt + 365 * DAY), validDays: Math.round((n.createdAt + 365 * DAY - Date.now()) / DAY), renewsAt: iso(n.createdAt + 335 * DAY) },
    updates: { agent: n.agentVersion !== AGENT, xray: n.xrayVersion !== XRAY, agentVersion: AGENT, xrayVersion: XRAY },
    usage: {
      live,
      online: live ? here.online : 0,
      connections: live ? here.connections : 0,
      rateUp: live ? Math.round(n.rate.up) : 0,
      rateDown: live ? Math.round(n.rate.down) : 0,
      todayUp: Math.round(n.today.up),
      todayDown: Math.round(n.today.down),
      monthUp: Math.round(n.month.up),
      monthDown: Math.round(n.month.down),
      updatedAt: live ? iso(Date.now() - 2000) : null,
    },
    installing: n.failure ? undefined : installingView(n),
  }
  if (n.failure) {
    const f = n.failure
    out.unfinished = { stepKey: f.step?.key ?? '', step: f.step?.name ?? '', error: f.error, cleanup: f.cleanup, at: iso(f.at) }
  }
  if (detail) out.events = n.events.slice(0, 50)
  return out
}

const subOn = (s: MockSub, n: MockNode) => s.on.has(n.id)

function nodeSubView(s: MockSub, n: MockNode): NodeSubscription {
  const enabled = subOn(s, n)
  const known = reachable(n) && !n.installJob
  const online = s.onlineOn === n.id
  const today = enabled ? split(s.today * (n.id === 1 ? 1 : 0.2)) : { up: 0, down: 0 }
  const month = split(s.month * (n.id === 1 ? 1 : 0.2))
  return {
    id: s.id,
    name: s.name,
    enabled,
    stopped: stoppedOf(s),
    trafficLimit: s.limit,
    trafficUsed: s.used,
    expiresAt: s.endsAt === null ? null : iso(s.endsAt),
    online: enabled && known ? online : null,
    connections: enabled && known ? (online ? s.conns : 0) : null,
    lastSeenAt: s.lastSeen ? iso(s.lastSeen) : null,
    todayUp: today.up,
    todayDown: today.down,
    monthUp: month.up,
    monthDown: month.down,
  }
}

function subView(s: MockSub): Subscription {
  const t = split(s.today)
  const m = split(s.month)
  const registered = nodes.filter((n) => n.registered)
  return {
    id: s.id,
    name: s.name,
    createdAt: iso(s.createdAt),
    trafficLimit: s.limit,
    trafficUsed: s.used,
    expiresAt: s.endsAt === null ? null : iso(s.endsAt),
    stopped: stoppedOf(s),
    online: registered.some((n) => !n.down) ? s.onlineOn !== null : null,
    lastSeenAt: s.lastSeen ? iso(s.lastSeen) : null,
    todayUp: t.up,
    todayDown: t.down,
    monthUp: m.up,
    monthDown: m.down,
    nodes: registered.map((n) => ({ ...nodeSubView(s, n), nodeId: n.id })).map(({ id: _id, name: _name, stopped: _s, trafficLimit: _l, trafficUsed: _u, expiresAt: _e, ...rest }) => rest),
  }
}

function entryView(): Entry {
  const now = Date.now()
  const wireguard = { state: 'online' as const, kind: 'wireguard' as const, label: 'WireGuard' }
  const nothing = { state: 'missing' as const, kind: '' as const, label: '' }
  const base = { routerOS: { version: '7.24.5', min: '7.20', ok: true }, checkedAt: iso(now - 2 * MIN), stale: false }
  const routes = (ready: { downloads: boolean; vpn: boolean; nodes: boolean }): Entry['routes'] => [
    { address: '192.168.50.16', usedFor: 'downloads', via: 'foreign', ready: ready.downloads },
    { address: '192.168.50.17', usedFor: 'entry-exit', via: 'vpn', ready: ready.vpn },
    { address: '192.168.50.18', usedFor: 'nodes', via: 'domestic', ready: ready.nodes },
  ]
  switch (entryMode) {
    case 'off':
      return { ...base, on: false, domestic: 'missing', vpn: nothing, bridge: 'stopped', routes: routes({ downloads: true, vpn: false, nodes: false }) }
    case 'domestic-offline':
      return { ...base, on: false, domestic: 'offline', vpn: wireguard, bridge: 'stopped', routes: routes({ downloads: true, vpn: true, nodes: false }) }
    case 'vpn-offline':
      return { ...base, on: false, domestic: 'online', vpn: { ...wireguard, state: 'offline' }, bridge: 'waiting', routes: routes({ downloads: true, vpn: false, nodes: true }) }
    case 'old-routeros':
      return { ...base, routerOS: { version: '7.9', min: '7.20', ok: false }, on: false, domestic: 'online', vpn: wireguard, bridge: 'stopped', routes: routes({ downloads: true, vpn: true, nodes: true }) }
    case 'stale':
      return { ...base, checkedAt: iso(now - 20 * MIN), stale: true, on: true, domestic: 'online', vpn: wireguard, bridge: 'running', routes: routes({ downloads: true, vpn: true, nodes: true }) }
    default:
      return { ...base, on: true, domestic: 'online', vpn: wireguard, bridge: 'running', routes: routes({ downloads: true, vpn: true, nodes: true }) }
  }
}

export function configView(): AppConfig {
  return {
    version: VERSION,
    commit: 'a1b2c3d',
    maxNodes,
    nodeCount: nodes.filter(isNode).length,
    maxSubscriptions: MAX_SUBSCRIPTIONS,
    subscriptionCount: subs.length,
    entry: entryView(),
    backup: { lastExportAt: null },
    xray: XRAY,
  }
}

// Moving numbers. Started by the dev server only, so importing this file (as
// vite.config.ts does) never keeps a `vite build` process alive.
function tick() {
  for (const n of nodes) {
    if (!reachable(n) || n.installJob) continue
    n.rate = { up: rand(150_000, 450_000), down: rand(800_000, 1_800_000) }
    n.today = { up: n.today.up + n.rate.up * 2.5, down: n.today.down + n.rate.down * 2.5 }
    n.month = { up: n.month.up + n.rate.up * 2.5, down: n.month.down + n.rate.down * 2.5 }
  }
}

// --- jobs ---

interface MockJob {
  view: Job
  listeners: Set<(j: Job) => void>
  /** the job's event stream answers 503, so the page has to poll */
  noStream: boolean
}
const jobs = new Map<string, MockJob>()
/** Hosts with an install still running. */
const installing = new Set<string>()

export interface Step {
  key: string
  name: string
  detail?: string
  ms?: number
  /** fails the step and the job with this message */
  fail?: string | false
  /** runs when the step succeeds */
  then?: (j: MockJob) => void
  /** reports its progress in the detail, like an upload */
  progress?: { total: string; unit: string }
}

const snapshot = (j: MockJob): Job => ({ ...j.view, steps: j.view.steps.map((s) => ({ ...s })) })
const emit = (j: MockJob) => j.listeners.forEach((l) => l(snapshot(j)))

/** What a job does after a step failed: more steps (a cleanup), and the error and code it ends with. */
export interface Recovery {
  steps: Step[]
  /** the job's error, from the failed step's text and whether the steps worked */
  error: (cause: string, worked: boolean) => string
  code?: string
  /** the facts that go with the code */
  info?: ErrorInfo
}

export function startJob(
  kind: JobKind,
  steps: Step[],
  o: { nodeId?: number; target?: string; noStream?: boolean; end?: () => void; install?: Job['install']; recover?: (failed: Step) => Recovery } = {},
) {
  const j: MockJob = {
    view: {
      id: hex(16),
      kind,
      state: 'running',
      // every step is listed from the start
      steps: steps.map((s) => ({ key: s.key, name: s.name, state: 'pending' as const })),
      nodeId: o.nodeId ?? 0,
      target: o.target ?? '',
      startedAt: iso(Date.now()),
      install: o.install,
    },
    listeners: new Set(),
    noStream: !!o.noStream,
  }
  jobs.set(j.view.id, j)
  const finish = (error?: string, code?: string, info?: ErrorInfo) => {
    o.end?.()
    j.view.state = error ? 'failed' : 'ok'
    if (error) j.view.error = error
    if (code) j.view.code = code
    if (info) j.view.errorInfo = info
    j.view.finishedAt = iso(Date.now())
    emit(j)
  }
  // like the hub: what comes after a failure is added at the end, after the steps never reached
  const recover = async (failed: Step) => {
    const r = o.recover?.(failed)
    if (!r) return finish(failed.fail || 'failed')
    let worked = true
    for (const step of r.steps) {
      j.view.steps.push({ key: step.key, name: step.name, state: 'pending' })
      if (!(await runStep(step))) {
        worked = false
        break
      }
    }
    finish(r.error(failed.fail || '', worked), r.code, r.info)
  }
  // runs one step; false when it failed
  const runStep = async (step: Step) => {
    const s = j.view.steps.find((x) => x.name === step.name)!
    s.state = 'running'
    emit(j)
    await sleep((step.ms ?? 380) * pace)
    if (step.fail) {
      Object.assign(s, { state: 'failed', detail: step.fail })
      emit(j)
      return false
    }
    Object.assign(s, { state: 'ok', detail: step.detail })
    step.then?.(j)
    emit(j)
    return true
  }
  void (async () => {
    await sleep(300 * pace)
    for (const step of steps) {
      const s = j.view.steps.find((x) => x.name === step.name)!
      s.state = 'running'
      emit(j)
      const ms = (step.ms ?? 380) * pace
      if (step.progress) {
        const { total, unit } = step.progress
        for (let p = 8; p < 100; p += 15) {
          s.detail = `${p}% · ${((p / 100) * Number(total)).toFixed(1)} of ${total} ${unit}`
          emit(j)
          await sleep(ms / 7)
        }
      } else await sleep(ms)
      if (step.fail) {
        Object.assign(s, { state: 'failed', detail: step.fail })
        emit(j)
        return recover(step)
      }
      Object.assign(s, { state: 'ok', detail: step.detail ?? (step.progress ? `${step.progress.total} ${step.progress.unit}` : undefined) })
      step.then?.(j)
      emit(j)
    }
    finish()
  })()
  return j.view.id
}

function streamJob(req: IncomingMessage, res: ServerResponse, j: MockJob) {
  res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' })
  const write = (v: Job) => {
    res.write(`data: ${JSON.stringify(v)}\n\n`)
    if (v.state !== 'running') {
      j.listeners.delete(write)
      res.end()
    }
  }
  j.listeners.add(write)
  req.on('close', () => j.listeners.delete(write))
  write(snapshot(j))
}

// What the hub says after an install failed, as manager/install.go words it.
const NOTHING_CHANGED = 'Nothing was changed on the server, and nothing was kept.'
const CLEANED = 'The server was cleaned up and nothing was kept.'
const KEPT = 'Cleaning up the server failed too, so the install is kept as unfinished: clean it up or forget it on the Nodes tab.'
const TOOK_OVER = "The server's own Xray was stopped and removed, as you asked, and is not restored."
const xrayHere = (version: string, same: boolean) =>
  `This server already runs an Xray that Remote Nodes did not install (/usr/local/bin/xray, an xray.service unit). It is Xray ${version}, ${same ? 'the same build as' : 'not the same build as'} this plugin's. Use a server without one, or take it over.`
const AGENT_HERE = "This server already runs a nasnet-agent that Remote Nodes did not install, such as a Linux panel's node. A node belongs to one hub: use a server without one."
/** How many sites the node looks at when it chooses a Reality target itself (agentops.DefaultTargets). */
const SITES_TRIED = 8
const STILL_RUNNING = 'An Xray is still running on this server after its service was stopped, for example in a container. Stop it there, or use another server. Its service was started again.'

/** Hosts whose takeover has found an Xray still running once: the next try finds none, as if the person had stopped it. */
const stoppedElsewhere = new Set<string>()

/** The node's reason for refusing a target, in the hub's words, and '' for one it can use (the same triggers as mock-node's check). */
const whyNot = (target: string) =>
  target.includes('fail')
    ? "It doesn't use TLS 1.3."
    : target.includes('big')
      ? 'Its side of the handshake is 9632 bytes, and Xray reads at most 8 KB of it. A site with a smaller certificate would do.'
      : ''

/**
 * The Reality target a new node is asked to use, read as the hub reads one: host or host:port, 443 when the port is
 * left out, the host in lower case; '' for none. A string is an error, in the hub's words.
 */
export function cleanTarget(raw: unknown): { target: string } | { error: string } {
  if (raw === undefined || raw === null) return { target: '' }
  if (typeof raw !== 'string') return { error: 'handshakeTarget must be a string' }
  const s = raw.trim()
  if (!s) return { target: '' }
  if (/:\/\/|[/\s]/.test(s)) return { error: 'The Reality target must look like www.example.com:443.' }
  const m = /^\[?([^\]]+?)\]?(?::(\d+))?$/.exec(s)
  const host = m?.[1].toLowerCase() ?? ''
  if (!host || !/^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$/.test(host)) return { error: 'The Reality target must look like www.example.com:443.' }
  const port = m?.[2] === undefined ? 443 : Number(m[2])
  if (!Number.isInteger(port) || port < 1 || port > 65535) return { error: 'The Reality target has no valid port.' }
  // an address can't be the name in the hello, and an install has no other name to give
  if (/^\d+\.\d+\.\d+\.\d+$/.test(host) || host.includes(':')) return { error: 'The Reality target has to be a host name, not an address.' }
  return { target: `${host}:${port}` }
}

/** Why an install is stopped at one of its first steps, and what the hub says of it. */
interface Refusal {
  cause: string
  code: string
  info?: ErrorInfo
  /** "check" for "Check the server", "takeover" for the takeover's step */
  at: 'check' | 'takeover'
}

// The install is the hub's fifteen steps, and sixteen when the server's own Xray is to be taken over. The node is
// saved at the start, as installing, and the job has its id from then on; it is set up at the last step. An install
// that fails takes off the server what it put there and forgets the node; one that fails before it changed anything
// only forgets it. When the cleanup fails too, the record is kept as an unfinished install.
function install(b: Record<string, any>, asked = '') {
  const host = String(b.host).trim()
  const name = String(b.name || host)
  const has = (word: string) => host.includes(word)
  const takeOver = b.takeOverXray === true
  const n = node({
    id: nextNodeId++,
    name,
    host,
    sshPort: Number(b.sshPort) || 22,
    sshUser: String(b.sshUser || 'root'),
    hostKey: String(b.hostKey),
    registered: false,
    createdAt: Date.now(),
    entryPort: Number(b.entryPort) || 443,
    // the node chooses when none is asked for
    ...(asked ? { handshakeTarget: asked } : {}),
    agentVersion: '',
    xrayVersion: '',
  })
  nodes.push(n)
  installing.add(host)
  const late = has('late')
  // the node's own check of a target it was asked to use, which fails the install at its step; when it chose among
  // its sites itself and none worked there is no one target to name
  const why = asked ? whyNot(asked) : ''
  const refused: { target: string; reason: string } | null = why ? { target: asked, reason: why } : null
  const noneWork = !asked && has('notarget')
  const targetFailure = refused
    ? `The site ${refused.target} doesn't work as a Reality target from this server: ${refused.reason.charAt(0).toLowerCase()}${refused.reason.slice(1)}`
    : noneWork
      ? `None of the ${SITES_TRIED} sites tried works as a Reality target from this server.`
      : false

  // what the server is, which the install finds out at its first step
  const xrayThere = has('hasxray')
  const same = xrayThere && has('same')
  const xrayVersion = same ? XRAY : '26.9.1'
  let refusal: Refusal | null = null
  if (xrayThere && !takeOver) {
    refusal = { cause: xrayHere(xrayVersion, same), code: 'server_has_xray', info: { found: ['binary', 'unit'], xrayVersion, ...(same ? { sameBuild: true } : {}) }, at: 'check' }
  } else if (has('hasagent')) {
    refusal = { cause: AGENT_HERE, code: 'server_has_agent', at: 'check' }
  } else if (has('isnode')) {
    refusal = {
      cause: 'This server is already the node "Node 1" here, under another address. Change that node instead of adding the server again.',
      code: 'server_is_node',
      info: { nodeId: 1, nodeName: 'Node 1' },
      at: 'check',
    }
  } else if (has('agentbusy')) {
    refusal = { cause: 'TCP port 9090, which the agent needs, is already in use on the server by python3. Free it, or pick another port for the node.', code: 'port_in_use', info: { port: 9090, role: 'agent', process: 'python3' }, at: 'check' }
  } else if (has('portbusy') && n.entryPort === 443) {
    refusal = { cause: 'TCP port 443, which the entry needs, is already in use on the server by nginx. Free it, or pick another port for the node.', code: 'port_in_use', info: { port: 443, role: 'entry', process: 'nginx' }, at: 'check' }
  } else if (b.password === 'wrong') {
    refusal = { cause: "The server didn't accept the login. Check the user name and the password or key.", code: 'login_refused', at: 'check' }
  } else if (has('nosudo') && n.sshUser !== 'root') {
    refusal = { cause: "This login isn't root, and the server has no sudo. Sign in as root, or as a user that can use sudo.", code: 'needs_root', at: 'check' }
  } else if (has('keymoved') && b.hostKey !== NEW_KEY) {
    refusal = {
      cause: `The SSH host key of ${host} has changed: it shows ${NEW_KEY}, and ${n.hostKey} is on record. The server may have been set up again, or something sits between it and the router.`,
      code: 'host_key_changed',
      info: { pinnedKey: n.hostKey, shownKey: NEW_KEY },
      at: 'check',
    }
  } else if (takeOver && xrayThere && has('notstopped') && !stoppedElsewhere.has(host)) {
    stoppedElsewhere.add(host)
    refusal = { cause: STILL_RUNNING, code: 'xray_not_stopped', at: 'takeover' }
  }
  // a takeover that has gone through: the server's own Xray is gone, whatever happens next
  const took = takeOver && xrayThere && refusal === null

  const forget = () => {
    const i = nodes.indexOf(n)
    if (i >= 0) nodes.splice(i, 1)
  }
  // the steps before "Prepare the server" only look at the server, and a takeover that stopped has changed nothing
  const looked = new Set<string>([STEPS.checkServer.name, STEPS.downloadAgent.name, STEPS.createCertificate.name, STEPS.takeOverXray.name])
  const recover = (failed: Step): Recovery => {
    if (looked.has(failed.name) && !took) {
      return { steps: [{ ...STEPS.discardNode, ms: 150, then: forget }], error: (cause) => `${cause} ${NOTHING_CHANGED}`, code: refusal?.code, info: refusal?.info }
    }
    const fails = has('stuck')
    return {
      steps: [
        { ...STEPS.cleanServer, ms: 500, fail: fails && `Can't reach SSH on ${host} port 22 (dial tcp ${host}:22: i/o timeout). Check the address and the port, and that the server's firewall lets the router in.` },
        { ...STEPS.discardNode, ms: 150, then: forget },
      ],
      error: (cause, worked) => {
        if (!worked) n.failure = { step: STEPS[stepOf(failed)], error: cause, cleanup: `Can't reach SSH on ${host} port 22 (dial tcp ${host}:22: i/o timeout). Check the address and the port, and that the server's firewall lets the router in.`, at: Date.now() }
        return `${cause} ${worked ? CLEANED : KEPT}${took ? ' ' + TOOK_OVER : ''}`
      },
      code: targetFailure ? 'reality_target_failed' : has('unreach') ? 'node_unreachable' : undefined,
      info: targetFailure || took ? { ...(refused ? refused : noneWork ? { tried: SITES_TRIED } : {}), ...(took ? { tookOver: true } : {}) } : undefined,
    }
  }
  const takeoverStep: Step[] = takeOver
    ? [
        {
          ...STEPS.takeOverXray,
          detail: !xrayThere
            ? "There is no Xray of the server's own"
            : same
              ? `Xray ${XRAY} was stopped, and its service, 1 drop-in, 1 config file removed. It is the pinned build, so its binary stays.`
              : `Xray ${xrayVersion} was stopped, and its service, 1 drop-in, 1 config file, its binary removed. Xray ${XRAY} goes on later.`,
          ms: 400,
          fail: refusal?.at === 'takeover' && refusal.cause,
        },
      ]
    : []
  const id = startJob(
    'install',
    [
      { ...STEPS.checkServer, detail: 'amd64 · Debian GNU/Linux 12 (bookworm)', ms: 300, fail: refusal?.at === 'check' && refusal.cause },
      { ...STEPS.downloadAgent, detail: `v${AGENT} · 18 MB`, ms: 400 },
      { ...STEPS.createCertificate, detail: 'Valid until 2027-10-31', ms: 200 },
      ...takeoverStep,
      { ...STEPS.prepareServer, ms: 200 },
      {
        ...STEPS.uploadAgent,
        detail: '18.0 MB sent',
        ms: 900,
        progress: { total: '18.0', unit: 'MB' },
        fail: has('stuck') && "Couldn't upload the agent: the connection broke.",
      },
      { ...STEPS.startAgent, ms: 300 },
      {
        ...STEPS.checkAgent,
        detail: `v${AGENT}, managed profile`,
        ms: 300,
        fail: (has('fail') || has('unreach')) && `The agent doesn't answer on TCP port 9090 (dial tcp ${host}:9090: i/o timeout). The server's firewall (or the provider's) may block it.`,
      },
      { ...STEPS.connectAgent, ms: 200 },
      // the pinned build that the takeover kept is neither fetched nor sent
      same && took
        ? { ...STEPS.downloadXray, detail: 'Not needed', ms: 100 }
        : { ...STEPS.downloadXray, detail: `${XRAY} · 13.6 MB`, ms: 300 },
      same && took
        ? { ...STEPS.uploadXray, detail: 'Not needed', ms: 100 }
        : { ...STEPS.uploadXray, detail: '13.6 MB sent', ms: 1200, progress: { total: '13.6', unit: 'MB' } },
      { ...STEPS.checkXray, detail: `Xray ${XRAY}`, ms: 200 },
      { ...STEPS.chooseTarget, detail: n.handshakeTarget, ms: 300, fail: targetFailure || (late && `${n.handshakeTarget} didn't answer from this node`) },
      { ...STEPS.createKeys, ms: 200 },
      { ...STEPS.applyConfig, ms: 300 },
      {
        ...STEPS.saveNode,
        ms: 200,
        then: () => {
          Object.assign(n, { registered: true, agentVersion: AGENT, xrayVersion: XRAY, agentStart: Date.now() })
          event(n, 'tunnel_up')
        },
      },
    ],
    {
      nodeId: n.id,
      target: name,
      noStream: has('poll'),
      install: { name, host, sshPort: n.sshPort, sshUser: n.sshUser, entryPort: n.entryPort, ...(asked ? { handshakeTarget: asked } : {}) },
      recover,
      end: () => {
        installing.delete(host)
        delete n.installJob
      },
    },
  )
  n.installJob = id
  return id
}

/** The STEPS entry a step is, by its name. */
const stepOf = (s: Step) => (Object.keys(STEPS) as (keyof typeof STEPS)[]).find((k) => STEPS[k].name === s.name) ?? 'checkServer'

// --- the licence text ---

// What the hub serves at /licenses: the files of its licence directory, each under a heading. Much shorter.
const RULE = '#'.repeat(64)
const LICENSES =
  ['LICENSE', 'NOTICES', 'web-notices.txt']
    .map((name) => `${RULE}\n# ${name}\n${RULE}\n\nSample licence text for ${name}. The hub serves the files of its licence directory here.\n`)
    .join('\n')

// only the exact path, as the hub's route is: not /licenses/ or what is under it
function licensesRoute(req: Connect.IncomingMessage, res: ServerResponse, next: Connect.NextFunction) {
  const here = new URL(req.originalUrl ?? req.url ?? '/', 'http://mock').pathname === '/licenses'
  if (!here || (req.method !== 'GET' && req.method !== 'HEAD')) return next()
  res.setHeader('Content-Type', 'text/plain; charset=utf-8')
  res.setHeader('Cache-Control', 'no-store')
  res.setHeader('X-Content-Type-Options', 'nosniff')
  res.end(req.method === 'HEAD' ? undefined : LICENSES)
}

// --- the middleware ---

export function mockApi(): Plugin {
  return {
    name: 'mock-api',
    configureServer(server) {
      const ticker = setInterval(tick, 2500)
      server.httpServer?.on('close', () => clearInterval(ticker))
      server.middlewares.use('/licenses', licensesRoute)
      server.middlewares.use('/api', async (req, res) => {
        const chunks: Buffer[] = []
        for await (const c of req) chunks.push(c as Buffer)
        const body: Record<string, any> = chunks.length ? JSON.parse(Buffer.concat(chunks).toString() || '{}') : {}
        const method = req.method ?? 'GET'
        const path = (req.url ?? '').split('?')[0].replace(/^\//, '')
        const send = (status: number, data: unknown) => {
          res.statusCode = status
          res.setHeader('Content-Type', 'application/json')
          res.end(JSON.stringify(data))
        }
        const fail = (status: number, code: string, error: string, extra: object = {}) => send(status, { code, error, ...extra })
        await sleep(rand(80, 220))

        // Like the hub: only the page's own calls get in.
        const stream = method === 'GET' && /^jobs\/[^/]+\/events$/.test(path)
        const site = req.headers['sec-fetch-site']
        if (stream) {
          if (site && site !== 'same-origin' && site !== 'none') return fail(403, 'forbidden', 'requests from other sites are refused')
        } else if (path !== '__reset' && path !== 'health' && req.headers['x-remotenodes-ui'] !== '1') {
          return fail(403, 'forbidden', 'requests from other sites are refused')
        }

        if (await nodeApi({ method, path, body, send, fail, res })) return

        // Test hook: back to the sample data, or to a first run.
        if (path === '__reset' && method === 'POST') {
          seed(body)
          return send(200, { ok: true })
        }
        if (path === 'health') return send(200, { ok: true, version: VERSION, commit: 'a1b2c3d', writes: 0 })
        if (path === 'config' && method === 'GET') return send(200, configView())
        if (subscriptionRoutes({ method, path, body, send, fail, subs, nodes, max: MAX_SUBSCRIPTIONS, maxName: MAX_NAME, make: sub, view: subView, nodeView: view })) return

        if (path === 'probe' && method === 'POST') {
          const host = String(body.host ?? '').trim()
          if (host.includes('offline')) return fail(502, 'node_unreachable', `could not connect to ${host}:${body.sshPort}: connection refused`)
          // an unfinished install keeps its pinned key too, which its cleanup is held to
          const here = nodes.find((n) => n.host === host && (n.registered || n.failure))
          // only a pinned key makes a host known
          const pinned = here?.hostKey ?? ''
          // "keymoved": what it showed a moment ago, which the install then finds it no longer shows
          const first = host.includes('keymoved') ? OLD_KEY : pinned || NEW_KEY
          return send(200, {
            fingerprint: host.includes('changed') ? NEW_KEY : first,
            status: host.includes('changed') ? 'changed' : pinned ? 'known' : 'new',
          })
        }

        if (path === 'jobs' && method === 'GET') {
          const list = [...jobs.values()]
            .map((j) => snapshot(j))
            .sort((a, b) => Date.parse(b.startedAt) - Date.parse(a.startedAt))
            .slice(0, 20)
          return send(200, list)
        }
        const jm = path.match(/^jobs\/([^/]+)(\/events)?$/)
        if (jm && method === 'GET') {
          const j = jobs.get(decodeURIComponent(jm[1]))
          if (!j) return fail(404, 'not_found', 'no such job')
          if (!jm[2]) return send(200, snapshot(j))
          if (j.noStream) return fail(503, 'internal', 'job streams are not available here')
          return streamJob(req, res, j)
        }

        if (path === 'nodes' && method === 'GET') return send(200, nodes.map((n) => view(n)))
        if (path === 'nodes' && method === 'POST') {
          // an unfinished install is no node, and doesn't count
          if (nodes.filter(isNode).length >= maxNodes) return fail(409, 'node_limit', `this router runs at most ${maxNodes} nodes`)
          const host = String(body.host ?? '').trim()
          if (!host) return fail(400, 'bad_request', 'give the node address')
          if (installing.has(host)) return fail(409, 'busy', `an install to ${host} is still running`)
          if (nodes.some((n) => n.host === host && n.failure)) return fail(400, 'bad_request', "An install on that server didn't finish. Clean it up or forget it on the Nodes tab first.")
          if (nodes.some((n) => n.host === host)) return fail(409, 'bad_request', `${host} is already a node here`)
          const name = String(body.name ?? '').trim()
          if (tooLong(name)) return fail(400, 'bad_request', NAME_TOO_LONG)
          if (nodes.some((n) => n.name === name && isNode(n))) return fail(409, 'name_taken', `a node named ${name} already exists`)
          if (!body.password && !body.privateKey) return fail(400, 'bad_request', 'give a password or a private key')
          if (!String(body.hostKey ?? '').startsWith('SHA256:')) return fail(400, 'bad_request', "confirm the node's host key first")
          const port = body.entryPort === undefined ? 443 : Number(body.entryPort)
          if (!Number.isInteger(port) || port < 1 || port > 65535 || port === Number(body.sshPort)) return fail(400, 'bad_request', 'the entry port must be 1 to 65535 and differ from the SSH port')
          // a Reality target is read before anything starts, as the hub reads one
          const asked = cleanTarget(body.handshakeTarget)
          if ('error' in asked) return fail(400, 'bad_request', asked.error)
          await sleep(300)
          return send(202, { jobId: install(body, asked.target) })
        }

        const m = path.match(/^nodes\/(\d+)(?:\/(.*))?$/)
        if (m) {
          const n = nodes.find((x) => x.id === Number(m[1]))
          if (!n) return fail(404, 'not_found', 'no such node')
          const rest = m[2] ?? ''
          if (rest === '' && method === 'GET') return send(200, view(n, true))
          if (rest === 'subscriptions' && method === 'GET') return send(200, subs.map((s) => nodeSubView(s, n)))
        }
        if (path === 'subscriptions' && method === 'GET') return send(200, subs.map(subView))

        return fail(501, 'internal', `the mock doesn't answer ${method} /api/${path} yet`)
      })
    },
  }
}

seed({ fresh: process.env.MOCK === 'fresh' })
