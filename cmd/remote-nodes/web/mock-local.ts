// The mock's answers for the router's own entry, "This router": what it is and how it is doing, the calls
// that change it, its list of subscriptions and their links. mock-api.ts calls localApi() on every request.
// Development only; never built into the page.
//
// In the mock's own sets the router is place 0: a subscription is on it when `on` has 0, and has a
// connection there when `onlineOn` is 0. Nothing here is a job, as on the hub.
//
// The sample data has it working, with five subscriptions on it and two of them connected. A reset picks
// another state with { local }:
//   "off"          switched off, as on a first run (what the e2e fixture resets to unless a test asks otherwise)
//   "starting"     switched on a moment ago: the router has not opened the port yet (it does after a few seconds)
//   "no-port"      the router never opened the port
//   "no-address"   no IP Cloud name and no name of its own, so no link can be made
//   "bridge-down"  the router's Xray is not running
//   "target"       the router found no Reality target that works
//   "nat"          working, behind another router
// Whatever the state, entry being off (reset with { entry }) pauses it, as on the hub.
//
// Turning it on, and asking for a new port, take a few seconds of "starting" here where the router takes
// up to a minute. Reality targets follow the node mock: one containing "fail" or "big" doesn't pass.
import { randomUUID } from 'node:crypto'
import type { HandshakeCheck, Link, Local, LocalReason, LocalState, NodeEvent, NodeSubscription, SubscriptionLocal, Traffic } from './src/lib/api'
import { entryIsOn, stepPace, stoppedOf, subs, XRAY_VERSION, type MockSub } from './mock-api'

const DAY = 86400e3
const H = 3600e3
const MB = 1e6
const GB = 1e9
const iso = (ms: number) => new Date(ms).toISOString().replace(/\.\d+Z$/, 'Z')

/** The router as a place in the mock's sets of node ids. */
export const ROUTER = 0

export type LocalMode = 'off' | 'on' | 'starting' | 'no-port' | 'no-address' | 'bridge-down' | 'target' | 'nat'

interface State {
  enabled: boolean
  mode: LocalMode
  ownHost: string
  cloudName: string
  publicAddr: string
  domesticAddr: string
  port: number | null
  /** when the router opens the port it was asked for; 0 when none is waited for */
  portAt: number
  target: string
  /** a server name of its own; '' is the target's */
  sni: string
  publicKey: string
  shortId: string
  /** when its Xray started */
  startedAt: number
  events: NodeEvent[]
  rate: { up: number; down: number }
  today: { up: number; down: number }
  month: { up: number; down: number }
}

const CLOUD = 'a1b2c3d4e5f6.sn.mynetname.net'
const randomKey = () => {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_'
  return Array.from({ length: 43 }, () => chars[Math.floor(Math.random() * chars.length)]).join('')
}
const randomHex = (n: number) => Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('')
const randomPort = (not: number | null) => {
  for (;;) {
    const p = 20000 + Math.floor(Math.random() * 40000)
    if (p !== not) return p
  }
}

let s: State = blank()

function blank(): State {
  return {
    enabled: false,
    mode: 'off',
    ownHost: '',
    cloudName: CLOUD,
    publicAddr: '198.51.100.24',
    domesticAddr: '198.51.100.24',
    port: null,
    portAt: 0,
    target: '',
    sni: '',
    publicKey: '',
    shortId: '',
    startedAt: 0,
    events: [],
    rate: { up: 0, down: 0 },
    today: { up: 0, down: 0 },
    month: { up: 0, down: 0 },
  }
}

const event = (kind: string, detail = '', at = Date.now()) => {
  s.events.push({ at: iso(at), kind, detail })
  s.events.sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
}

/** Sets the entry up for a reset. `mode` left out is the sample data: working. */
export function seedLocal(mode: LocalMode = 'on') {
  const now = Date.now()
  s = blank()
  s.mode = mode
  if (mode === 'off') return
  s.enabled = true
  s.publicKey = 'Hq3vT9mXb2KdR7wLpN5sYc8ZfA1eJ4uG6oQ0iVtBnMk'
  s.shortId = '9f3a1c7e'
  s.target = 'www.example.com:443'
  s.port = 41873
  s.startedAt = now - 6 * DAY - 4 * H
  s.rate = { up: 96_000, down: 640_000 }
  s.today = { up: 0.3 * GB, down: 1.0 * GB }
  s.month = { up: 38 * GB, down: 212 * GB }
  event('local_on', '', now - 6 * DAY - 4 * H)
  event('port_opened', '41873', now - 6 * DAY - 4 * H + 40e3)
  event('subscription_on', 'Sara', now - 2 * H)
  if (mode === 'starting') {
    s.port = null
    s.portAt = now + 4000 * stepPace()
    s.events = [{ at: iso(now), kind: 'local_on', detail: '' }]
  }
  if (mode === 'no-port') s.port = null
  if (mode === 'no-address') s.cloudName = ''
  if (mode === 'target') s.target = ''
  if (mode === 'nat') s.domesticAddr = '192.168.1.20'
}

/** The subscriptions the sample data has on the router, and the two connected to it. Called after the subscriptions are made. */
export function seedLocalSubscriptions() {
  if (!s.enabled) return
  const now = Date.now()
  for (const name of ['Sara', 'Ali', 'Mina', 'Parsa', 'Dana']) subs.find((x) => x.name === name)?.on.add(ROUTER)
  const online = (name: string, conns: number) => {
    const x = subs.find((y) => y.name === name)
    if (x) Object.assign(x, { onlineOn: ROUTER, conns, lastSeen: now })
  }
  online('Mina', 2)
  online('Parsa', 1)
}

// --- how it is doing ---------------------------------------------------------------

function settle() {
  if (s.portAt && Date.now() >= s.portAt) {
    s.port = randomPort(s.port)
    s.portAt = 0
    if (s.mode === 'starting') s.mode = 'on'
    if (!s.startedAt) s.startedAt = Date.now()
    event('port_opened', String(s.port))
  }
}

function stateOf(): { state: LocalState; reason: LocalReason; detail: string } {
  settle()
  const is = (state: LocalState, reason: LocalReason = '', detail = '') => ({ state, reason, detail })
  if (!s.enabled) return is('off')
  if (!s.target) return s.mode === 'target' ? is('attention', 'target_failed', 'none of the 6 sites tried works as a Reality target from this router') : is('starting', 'choosing_target')
  if (!entryIsOn()) return is('attention', 'entry_off')
  if (s.portAt) return is('starting', 'waiting_router')
  if (s.port === null) return is('attention', 'no_port')
  if (!s.ownHost && !s.cloudName) return is('attention', 'no_address')
  if (s.mode === 'bridge-down') return is('attention', 'bridge_down')
  return is('working')
}

const working = () => stateOf().state === 'working'
const host = () => s.ownHost || s.cloudName

const enabledHere = () => subs.filter((x) => x.on.has(ROUTER))
const onlineHere = () => enabledHere().filter((x) => x.onlineOn === ROUTER)

export const localBrief = () => ({ enabled: s.enabled, state: stateOf().state })
export const localEnabled = () => s.enabled
/** The entry answers, so who is connected to it is known. */
export const localLive = () => working()

export function localView(): Local {
  const st = stateOf()
  const live = st.state === 'working'
  const online = onlineHere()
  return {
    enabled: s.enabled,
    ...st,
    host: host(),
    hostSource: s.ownHost ? 'own' : s.cloudName ? 'cloud' : '',
    ownHost: s.ownHost,
    cloudName: s.cloudName,
    publicAddr: s.enabled ? s.publicAddr : '',
    domesticAddr: s.enabled ? s.domesticAddr : '',
    behindNat: s.enabled && s.publicAddr !== s.domesticAddr,
    port: s.enabled ? s.port : null,
    handshakeTarget: s.target,
    // as the hub sends it: empty unless it has a server name of its own
    sni: s.sni,
    sniCustom: !!s.sni,
    publicKey: s.publicKey,
    shortId: s.shortId,
    subscriptionCount: enabledHere().length,
    usage: {
      live,
      online: live ? online.length : 0,
      connections: live ? online.reduce((n, x) => n + x.conns, 0) : 0,
      rateUp: live ? Math.round(s.rate.up) : 0,
      rateDown: live ? Math.round(s.rate.down) : 0,
      todayUp: Math.round(s.today.up),
      todayDown: Math.round(s.today.down),
      monthUp: Math.round(s.month.up),
      monthDown: Math.round(s.month.down),
      updatedAt: live ? iso(Date.now() - 2000) : null,
    },
    xrayVersion: XRAY_VERSION,
    uptimeSec: live && s.startedAt ? Math.floor((Date.now() - s.startedAt) / 1000) : 0,
    events: s.events.slice(0, 50),
  }
}

const split = (total: number) => {
  const up = Math.round(total * 0.25)
  return { up, down: Math.round(total) - up }
}

/** A subscription's row on the entry, for the subscription's own view; null while the entry is off. */
export function subscriptionLocal(x: MockSub): SubscriptionLocal | null {
  if (!s.enabled) return null
  const enabled = x.on.has(ROUTER)
  const known = enabled && working()
  const online = x.onlineOn === ROUTER
  const today = enabled ? split(x.today * 0.3) : { up: 0, down: 0 }
  const month = split(x.month * 0.3)
  return {
    enabled,
    online: known ? online : null,
    connections: known ? (online ? x.conns : 0) : null,
    lastSeenAt: x.lastSeen ? iso(x.lastSeen) : null,
    todayUp: today.up,
    todayDown: today.down,
    monthUp: month.up,
    monthDown: month.down,
  }
}

function listView(x: MockSub): NodeSubscription {
  const row = subscriptionLocal(x)
  return {
    id: x.id,
    name: x.name,
    enabled: x.on.has(ROUTER),
    stopped: stoppedOf(x),
    trafficLimit: x.limit,
    trafficUsed: x.used,
    expiresAt: x.endsAt === null ? null : iso(x.endsAt),
    online: row?.online ?? null,
    connections: row?.connections ?? null,
    lastSeenAt: row?.lastSeenAt ?? null,
    todayUp: row?.todayUp ?? 0,
    todayDown: row?.todayDown ?? 0,
    monthUp: row?.monthUp ?? 0,
    monthDown: row?.monthDown ?? 0,
  }
}

function traffic(): Traffic {
  const stepSec = 15
  const step = stepSec * 1000
  const last = Math.floor(Date.now() / step)
  const samples: Traffic['samples'] = []
  if (working()) {
    for (let k = last - 60; k <= last; k++) {
      // the same reading for the same step every time it is asked, so the line scrolls instead of jumping
      const wave = (seed: number) => 0.5 + 0.5 * Math.sin(k / 5 + seed) * Math.cos(k / 17 + seed * 3)
      samples.push({ at: iso(k * step), down: Math.round((0.3 + 0.5 * wave(1)) * MB), up: Math.round((0.05 + 0.08 * wave(2)) * MB) })
    }
  }
  return { stepSec, samples }
}

/** The numbers move while it works, like a node's. */
export function tickLocal() {
  if (!working()) return
  s.rate = { up: 60_000 + Math.random() * 80_000, down: 400_000 + Math.random() * 500_000 }
  s.today = { up: s.today.up + s.rate.up * 2.5, down: s.today.down + s.rate.down * 2.5 }
  s.month = { up: s.month.up + s.rate.up * 2.5, down: s.month.down + s.rate.down * 2.5 }
}

// --- links ---------------------------------------------------------------------------

// by subscription object, so a reset (which makes new ones) starts every link afresh
const ids = new WeakMap<object, string>()
const linkId = (x: MockSub, replace = false) => {
  if (replace || !ids.has(x)) ids.set(x, randomUUID())
  return ids.get(x)!
}

const vless = (id: string, name: string) =>
  `vless://${id}@${host()}:${s.port}?encryption=none&flow=xtls-rprx-vision&security=reality&sni=${s.sni || s.target.split(':')[0]}&fp=chrome&pbk=${s.publicKey}&sid=${s.shortId}&type=tcp#${encodeURIComponent(name)}`

// --- the calls -----------------------------------------------------------------------

const TARGET = /^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?:\d{1,5}$/
const HOST = /^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z](?:[a-z0-9-]*[a-z0-9])?$/

function handshake(target: string): HandshakeCheck {
  if (target.includes('fail'))
    return { ok: false, tls13: false, x25519: false, handshakeBytes: 0, latencyMs: 0, error: `${target} did not complete a TLS 1.3 handshake: remote error: tls: protocol version not supported` }
  if (target.includes('big')) return { ok: false, tls13: true, x25519: true, handshakeBytes: 9632, latencyMs: 71, error: 'the handshake is 9.4 KB, over the 8 KB limit' }
  return { ok: true, tls13: true, x25519: true, handshakeBytes: 4200, latencyMs: 38, error: '' }
}

export interface LocalCall {
  method: string
  /** without the leading slash and the query: "local/settings" */
  path: string
  body: Record<string, any>
  send: (status: number, data: unknown) => void
  fail: (status: number, code: string, error: string, extra?: object) => void
  /** the subscription as the list shows it, for the switch's answer */
  view: (x: MockSub) => unknown
}

const OFF = 'This router is switched off.'

/** Answers the call and returns true when it is one of the entry's, else returns false and touches nothing. */
export function localApi({ method, path, body, send, fail, view }: LocalCall): boolean {
  const reply = (status: number, data: unknown) => (send(status, data), true)
  const refuse = (status: number, code: string, error: string, extra?: object) => (fail(status, code, error, extra), true)

  if (path === 'local' && method === 'GET') return reply(200, localView())
  if (path === 'local/subscriptions' && method === 'GET') return reply(200, subs.map(listView))
  if (path === 'local/traffic' && method === 'GET') return reply(200, traffic())

  if (path === 'local' && method === 'PUT') {
    if (typeof body.enabled !== 'boolean') return refuse(400, 'bad_request', 'enabled must be true or false.')
    if (body.enabled && !s.enabled) {
      s.enabled = true
      s.mode = 'on'
      // keys are made the first time, and kept through off and on
      if (!s.publicKey) Object.assign(s, { publicKey: randomKey(), shortId: randomHex(8) })
      if (!s.target) s.target = 'www.example.com:443'
      // the port is kept through off and on too; the router needs a moment to open it again
      if (s.port === null) s.portAt = Date.now() + 2500 * stepPace()
      s.startedAt = Date.now()
      event('local_on')
    } else if (!body.enabled && s.enabled) {
      s.enabled = false
      s.portAt = 0
      for (const x of subs) if (x.onlineOn === ROUTER) Object.assign(x, { onlineOn: null, conns: 0 })
      event('local_off')
    }
    return reply(200, localView())
  }

  if (path === 'local/settings' && method === 'PATCH') {
    const { host: name, handshakeTarget, sni } = body
    if (name === undefined && handshakeTarget === undefined && sni === undefined) return refuse(400, 'bad_request', 'There is nothing to change.')
    if (name !== undefined && name !== '' && !(typeof name === 'string' && HOST.test(name))) return refuse(400, 'bad_request', 'Give a host name, such as home.example.com.')
    if (sni !== undefined && sni !== '' && !(typeof sni === 'string' && HOST.test(sni))) return refuse(400, 'bad_request', 'Give a host name, such as cdn.example.com.')
    if (handshakeTarget !== undefined && handshakeTarget !== '') {
      if (typeof handshakeTarget !== 'string' || !TARGET.test(handshakeTarget)) return refuse(400, 'bad_request', 'give the target as host:port, such as www.example.com:443')
      const result = handshake(handshakeTarget)
      if (!result.ok) return refuse(409, 'reality_target_failed', result.error, { errorInfo: { target: handshakeTarget, reason: result.error } })
    }
    if (name !== undefined && name !== s.ownHost) {
      s.ownHost = name
      event('host_changed', name)
    }
    if (handshakeTarget !== undefined && handshakeTarget !== s.target) {
      // left to the router, it finds one at once here
      s.target = handshakeTarget || 'www.example.org:443'
      s.sni = ''
      if (s.mode === 'target') s.mode = 'on'
      event('target_changed', s.target)
    }
    if (sni !== undefined) s.sni = sni === s.target.split(':')[0] ? '' : sni
    return reply(200, localView())
  }

  if (path === 'local/new-port' && method === 'POST') {
    if (!s.enabled) return refuse(409, 'local_off', OFF)
    s.portAt = Date.now() + 2500 * stepPace()
    event('port_requested')
    return reply(200, localView())
  }

  if (path === 'local/rotate-keys' && method === 'POST') {
    Object.assign(s, { publicKey: randomKey(), shortId: randomHex(8) })
    event('keys_rotated')
    return reply(200, localView())
  }

  if (path === 'local/handshake-check' && method === 'POST') {
    const target = body.target
    if (typeof target !== 'string' || !TARGET.test(target)) return refuse(400, 'bad_request', 'give the target as host:port, such as www.example.com:443')
    return reply(200, handshake(target))
  }

  const m = path.match(/^subscriptions\/(\d+)\/local(?:\/(link|replace-link))?$/)
  if (!m) return false
  const x = subs.find((y) => y.id === Number(m[1]))
  if (!x) return refuse(404, 'not_found', 'no such subscription')

  if (!m[2] && method === 'PUT') {
    if (typeof body.enabled !== 'boolean') return refuse(400, 'bad_request', 'enabled must be true or false.')
    if (!s.enabled) return refuse(409, 'local_off', OFF)
    if (body.enabled) x.on.add(ROUTER)
    else {
      x.on.delete(ROUTER)
      if (x.onlineOn === ROUTER) Object.assign(x, { onlineOn: null, conns: 0 })
    }
    event(body.enabled ? 'subscription_on' : 'subscription_off', x.name)
    return reply(200, view(x))
  }

  if ((m[2] === 'link' && method === 'GET') || (m[2] === 'replace-link' && method === 'POST')) {
    if (!s.enabled) return refuse(409, 'local_off', OFF)
    // a link exists while the subscription is on for the entry
    if (!x.on.has(ROUTER)) return refuse(404, 'not_found', 'no link for this subscription on this router')
    if (!host() || s.port === null) return refuse(409, 'no_address', 'This router has no name or no port to put in a link yet.')
    const out: Link = { nodeId: 0, nodeName: '', local: true, vless: vless(linkId(x, m[2] === 'replace-link'), x.name) }
    return reply(200, out)
  }

  return false
}
