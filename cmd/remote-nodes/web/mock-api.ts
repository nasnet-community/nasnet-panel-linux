// A fake plugin API for `npm run dev:mock`: the whole page runs with sample data
// and no router, VPS or agent. Development only; never built into the page.
//
// It follows the contract in the plan (HTTP API, v1): config with the entry state,
// probe, jobs with their event stream, nodes (list, detail, install), and the
// subscription lists the node cards read. Other calls answer 501.
//
// Sample data matches the design boards: Node 1 works (3 of its 6 subscriptions
// are online), Node 2 can't be reached (its agent and Xray are due an update),
// Node 3 is installing, frozen at step 5 of 8 (62% through the Xray upload).
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
//   address contains "fail"    -> fails at "Start the agent" (dial timeout); nothing is kept
//   address contains "late"    -> fails at "Choose a handshake target"; the node is kept
//   address contains "poll"    -> the job's event stream answers 503, so the page polls
//   password "wrong"           -> the login is refused before any job starts
//   an existing address        -> refused: it is already a node here
//
// The e2e reset (POST /api/__reset) takes { fresh, nodes, maxNodes, entry, pace }:
//   fresh     the first-run state above
//   nodes     0 to 3: how many of the sample nodes stay (1 keeps only Node 1)
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
import type { AgentState, AppConfig, Entry, Job, JobKind, Node, NodeEvent, NodeSubscription, Subscription } from './src/lib/api'
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
const MAX_SUBSCRIPTIONS = 10

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
}

const split = (total: number) => {
  const up = Math.round(total * 0.25)
  return { up, down: Math.round(total) - up }
}

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
  return { id: nextSubId++, name, createdAt: Date.now() - 20 * DAY, onlineOn: null, conns: 0, lastSeen: null, today: 0, month: 0, on: new Set(), ...o }
}

function seed(o: { fresh?: boolean; nodes?: number; maxNodes?: number; entry?: string; pace?: number } = {}) {
  const now = Date.now()
  nodes = []
  subs = []
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

  const n3 = node({ id: 3, name: 'Node 3', host: '192.0.2.41', registered: true, createdAt: now - 62e3, installJob: 'seed-install-3' })

  const wanted = [n1, n2, n3].slice(0, keep)
  nodes = wanted
  nextNodeId = wanted.length + 1

  const names: [string, number[], Partial<MockSub>][] = [
    ['Sara', [1, 2], { onlineOn: 1, conns: 3, lastSeen: now, today: 1.2 * GB, month: 38 * GB }],
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
          { name: 'release', state: 'ok', detail: `v${AGENT}` },
          { name: 'verify', state: 'ok' },
          { name: 'upload', state: 'ok', detail: '18 MB' },
          { name: 'agent', state: 'ok', detail: 'Connected over mTLS' },
          { name: 'xray', state: 'running', detail: '62% · 8.4 of 13.6 MB' },
          { name: 'handshake', state: 'pending' },
          { name: 'config', state: 'pending' },
          { name: 'tunnel', state: 'pending' },
        ],
      },
      listeners: new Set(),
      noStream: false,
    })
  }
  const finished = (id: string, kind: JobKind, nodeId: number, target: string, ago: number, took: number): [string, MockJob] => [
    id,
    {
      view: { id, kind, state: 'ok', nodeId, target, startedAt: iso(now - ago - took), finishedAt: iso(now - ago), steps: [{ name: 'release', state: 'ok' }, { name: 'tunnel', state: 'ok' }] },
      listeners: new Set(),
      noStream: false,
    },
  ]
  if (keep >= 2) jobs.set(...finished('seed-node-2', 'install', 2, 'Node 2', 3 * DAY, 168e3))
  if (keep >= 1) jobs.set(...finished('seed-update-1', 'update-agent', 1, '', DAY, 41e3))
}

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
  return { jobId: j.id, step: i < 0 ? j.steps.filter((s) => s.state === 'ok').length + 1 : i + 1, steps: j.steps.length, label: step ? stepText(step.name) : '', percent: m ? Number(m[1]) : null }
}

// the English words the hub would send for a step; the page words steps itself
const STEP_TEXT: Record<string, string> = {
  release: 'Fetch the signed release',
  verify: 'Check signature and hashes',
  upload: 'Send the agent',
  agent: 'Start the agent',
  xray: `Send Xray ${XRAY}`,
  handshake: 'Choose a handshake target',
  config: 'Apply the config',
  tunnel: 'Start the tunnel',
}
const stepText = (name: string) => STEP_TEXT[name] ?? name

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
    installing: installingView(n),
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
    online: registered.some((n) => !n.down) ? s.onlineOn !== null : null,
    lastSeenAt: s.lastSeen ? iso(s.lastSeen) : null,
    todayUp: t.up,
    todayDown: t.down,
    monthUp: m.up,
    monthDown: m.down,
    nodes: registered.map((n) => ({ ...nodeSubView(s, n), nodeId: n.id })).map(({ id: _id, name: _name, ...rest }) => rest),
  }
}

function entryView(): Entry {
  const now = Date.now()
  const wireguard = { state: 'online' as const, kind: 'wireguard' as const, label: 'WireGuard' }
  const nothing = { state: 'missing' as const, kind: '' as const, label: '' }
  const base = { routerOS: { version: '7.24.5', min: '7.11', ok: true }, checkedAt: iso(now - 2 * MIN), stale: false }
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
      return { ...base, routerOS: { version: '7.9', min: '7.11', ok: false }, on: false, domestic: 'online', vpn: wireguard, bridge: 'stopped', routes: routes({ downloads: true, vpn: true, nodes: true }) }
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
    nodeCount: nodes.length,
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

export function startJob(kind: JobKind, steps: Step[], o: { nodeId?: number; target?: string; noStream?: boolean; end?: () => void; failed?: () => void } = {}) {
  const j: MockJob = {
    view: {
      id: hex(16),
      kind,
      state: 'running',
      // every step is listed from the start
      steps: steps.map((s) => ({ name: s.name, state: 'pending' as const })),
      nodeId: o.nodeId ?? 0,
      target: o.target ?? '',
      startedAt: iso(Date.now()),
    },
    listeners: new Set(),
    noStream: !!o.noStream,
  }
  jobs.set(j.view.id, j)
  const finish = (error?: string) => {
    o.end?.()
    if (error) o.failed?.()
    j.view.state = error ? 'failed' : 'ok'
    if (error) j.view.error = error
    j.view.finishedAt = iso(Date.now())
    emit(j)
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
        return finish(step.fail)
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

// The install is eight steps; the page words them (see stepLabel in src/lib/jobs.ts).
function install(b: Record<string, any>) {
  const host = String(b.host).trim()
  const name = String(b.name || host)
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
    agentVersion: '',
    xrayVersion: '',
  })
  nodes.push(n)
  installing.add(host)
  const late = host.includes('late')
  const id = startJob(
    'install',
    [
      { name: 'release', detail: `v${AGENT}`, ms: 500 },
      { name: 'verify' },
      { name: 'upload', detail: '18 MB', ms: 900, progress: { total: '18.0', unit: 'MB' } },
      {
        name: 'agent',
        detail: 'Connected over mTLS',
        ms: 600,
        fail: host.includes('fail') && `dial ${host}:8443: i/o timeout`,
        then: (j) => {
          // the node is saved once its agent answers
          Object.assign(n, { registered: true, agentVersion: AGENT, agentStart: Date.now() })
          j.view.nodeId = n.id
        },
      },
      { name: 'xray', detail: `Xray ${XRAY}`, ms: 1200, progress: { total: '13.6', unit: 'MB' }, then: () => (n.xrayVersion = XRAY) },
      { name: 'handshake', detail: n.handshakeTarget, fail: late && `${n.handshakeTarget} didn't answer from this node` },
      { name: 'config' },
      { name: 'tunnel', ms: 500, then: () => event(n, 'tunnel_up') },
    ],
    {
      nodeId: 0,
      target: name,
      noStream: host.includes('poll'),
      end: () => {
        installing.delete(host)
        delete n.installJob
      },
      // an install that died before the node was saved leaves nothing behind
      failed: () => {
        if (!n.registered) nodes.splice(nodes.indexOf(n), 1)
      },
    },
  )
  n.installJob = id
  return id
}

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
        if (subscriptionRoutes({ method, path, body, send, fail, subs, nodes, max: MAX_SUBSCRIPTIONS, make: sub, view: subView, nodeView: view })) return

        if (path === 'probe' && method === 'POST') {
          const host = String(body.host ?? '').trim()
          if (host.includes('offline')) return fail(502, 'node_unreachable', `could not connect to ${host}:${body.sshPort}: connection refused`)
          const here = nodes.find((n) => n.host === host && n.registered)
          // only a pinned key makes a host known
          const pinned = here?.hostKey ?? ''
          return send(200, {
            fingerprint: host.includes('changed') ? NEW_KEY : pinned || NEW_KEY,
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
          if (nodes.length >= maxNodes) return fail(409, 'node_limit', `this router runs at most ${maxNodes} nodes`)
          const host = String(body.host ?? '').trim()
          if (!host) return fail(400, 'bad_request', 'give the node address')
          if (nodes.some((n) => n.host === host)) return fail(409, 'bad_request', `${host} is already a node here`)
          if (installing.has(host)) return fail(409, 'busy', `an install to ${host} is still running`)
          const name = String(body.name ?? '').trim()
          if (name.length > 100) return fail(400, 'bad_request', 'the name must be at most 100 characters')
          if (nodes.some((n) => n.name === name)) return fail(409, 'name_taken', `a node named ${name} already exists`)
          if (!body.password && !body.privateKey) return fail(400, 'bad_request', 'give a password or a private key')
          if (!String(body.hostKey ?? '').startsWith('SHA256:')) return fail(400, 'bad_request', "confirm the node's host key first")
          const port = body.entryPort === undefined ? 443 : Number(body.entryPort)
          if (!Number.isInteger(port) || port < 1 || port > 65535 || port === Number(body.sshPort)) return fail(400, 'bad_request', 'the entry port must be 1 to 65535 and differ from the SSH port')
          // like the hub: the login is tried before the job starts
          await sleep(700)
          if (body.password === 'wrong') return fail(400, 'bad_request', 'ssh: unable to authenticate: the password or key was not accepted')
          return send(202, { jobId: install(body) })
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
