// The mock's answers for outbounds and "Leaves through": the list, adding, editing and removing one, the
// tests, the address the health check fetches, and where each node and This router leaves through.
// mock-api.ts calls outboundApi() on every request. Development only; never built into the page.
//
// The sample data shows every state:
//   1 Frankfurt  VLESS · TCP · REALITY      Node 1 leaves through it      up, about 180 ms
//   2 Amsterdam  Trojan · WebSocket · TLS   Node 2, its fallback on       down, so on the router's VPN
//   3 Paris      VMess · WebSocket · TLS    This router                   down: connection refused
//   4 Helsinki   Shadowsocks (2022)         nothing                       not in use
//   5 Office     SOCKS, with a login        nothing                       not in use
// One in use reads "not checked yet" while entry is off (the bridge isn't running, as on the hub), and for
// a few seconds after something starts leaving through it.
//
// Tests: a saved outbound answers the way it is doing (up: a delay; down: its error). Unsaved settings,
// by their host: "fail" doesn't answer, "flaky" answers every other time, "notest" gets test_unavailable
// (no bridge program, as on a dev machine); anything else answers in 90 to 250 ms.
// Saving new settings: a host with "refuse" is refused as Xray refuses settings (bad_request), the way the
// hub's build check does; one with "down" is saved but doesn't answer once in use.
// Names follow the hub's rules: 1 to 40 characters, unique whatever their case. At most 16 outbounds.
//
// A reset takes { outbounds: false } for none, and { outboundLimit } for another limit.
// POST /api/__outbound { id, health: "up" | "down", error, delayMs } puts one in a state after a reset.
import type { Local, Node, Outbound, OutboundSettings, OutboundState } from './src/lib/api'
import { entryIsOn, isNode, nodes, stepPace } from './mock-api'
import { localView } from './mock-local'
import { detail } from './mock-node'

const iso = (ms: number) => new Date(ms).toISOString().replace(/\.\d+Z$/, 'Z')
const rand = (lo: number, hi: number) => lo + Math.floor(Math.random() * (hi - lo + 1))
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
const DAY = 86400e3
const MB = 1e6

const DEFAULT_URL = 'https://www.google.com/generate_204'
const SECONDS = 30
const MAX_NAME = 40

interface MockOutbound {
  id: number
  name: string
  fallback: boolean
  createdAt: number
  settings: OutboundSettings
  health: 'up' | 'down'
  delayMs: number
  error: string
  bytesUp: number
  bytesDown: number
  /** something started leaving through it: not checked until then */
  checkedFrom: number
}

let outs: MockOutbound[] = []
let nextId = 1
let limit = 16
let checkUrl = ''
let flaky = false
const nodeRoutes = new Map<number, number>()
let localRoute = 0

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const HOST = /^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/i
const IPV4 = /^(\d{1,3})(\.\d{1,3}){3}$/
const isIP = (s: string) => IPV4.test(s) || (s.includes(':') && /^[0-9a-f:.]+$/i.test(s))
const isName = (s: string) => HOST.test(s) && !/^\d+$/.test(s.split('.').pop() ?? '')

const blank: OutboundSettings = {
  protocol: 'vless',
  host: '',
  port: 443,
  id: '',
  password: '',
  username: '',
  method: '',
  flow: '',
  network: 'tcp',
  path: '',
  hostHeader: '',
  mode: '',
  security: 'none',
  serverName: '',
  fingerprint: '',
  alpn: [],
  publicKey: '',
  shortId: '',
  spiderX: '',
}

function make(name: string, settings: Partial<OutboundSettings>, o: Partial<MockOutbound> = {}): MockOutbound {
  return { id: nextId++, name, fallback: false, createdAt: Date.now() - 9 * DAY, settings: { ...blank, ...settings }, health: 'up', delayMs: rand(120, 260), error: '', bytesUp: 0, bytesDown: 0, checkedFrom: 0, ...o }
}

/** Back to the sample data, or to none. */
export function seedOutbounds(o: { outbounds?: boolean; outboundLimit?: number } = {}) {
  outs = []
  nextId = 1
  limit = o.outboundLimit ?? 16
  checkUrl = ''
  flaky = false
  nodeRoutes.clear()
  localRoute = 0
  if (o.outbounds === false) return
  outs = [
    make(
      'Frankfurt',
      { protocol: 'vless', host: 'de.example.com', id: 'b831381d-6324-4d53-ad4f-8cda48b30811', flow: 'xtls-rprx-vision', security: 'reality', serverName: 'www.example.com', fingerprint: 'chrome', publicKey: 'Hq3vT9mXb2KdR7wLpN5sYc8ZfA1eJ4uG6oQ0iVtBnMk', shortId: 'ab12' },
      { delayMs: 180, bytesUp: 1.4e9, bytesDown: 6.2e9 },
    ),
    make(
      'Amsterdam',
      { protocol: 'trojan', host: 'nl.example.net', password: 'a-trojan-password', network: 'ws', path: '/tr', security: 'tls', serverName: 'nl.example.net', fingerprint: 'chrome', alpn: ['http/1.1'] },
      { fallback: true, health: 'down', error: 'dial tcp 198.51.100.80:443: i/o timeout', bytesUp: 220 * MB, bytesDown: 910 * MB },
    ),
    make(
      'Paris',
      { protocol: 'vmess', host: 'fr.example.org', port: 8443, id: '6a1f02c4-5e7b-4c3d-9a8e-1f2b3c4d5e6f', method: 'auto', network: 'ws', path: '/vm', security: 'tls', serverName: 'fr.example.org', fingerprint: 'chrome' },
      { health: 'down', error: 'dial tcp 203.0.113.70:8443: connect: connection refused' },
    ),
    make('Helsinki', { protocol: 'shadowsocks', host: '198.51.100.61', port: 8388, method: '2022-blake3-aes-128-gcm', password: 'AAECAwQFBgcICQoLDA0ODw==' }),
    make('Office', { protocol: 'socks', host: '192.0.2.10', port: 1080, username: 'office', password: 'office-pass' }),
  ]
  nodeRoutes.set(1, 1)
  nodeRoutes.set(2, 2)
  localRoute = 3
}

// --- views ---------------------------------------------------------------------------------

/** Where a node leaves through: an outbound's id, or 0 for the router's VPN. */
export const nodeRoute = (id: number) => nodeRoutes.get(id) ?? 0
/** Where This router leaves through. */
export const localRouteOf = () => localRoute

const usedBy = (o: MockOutbound) => ({
  nodes: nodes.filter((n) => isNode(n) && n.registered && nodeRoutes.get(n.id) === o.id).map((n) => n.id),
  router: localRoute === o.id,
})
const inUse = (o: MockOutbound) => {
  const u = usedBy(o)
  return u.nodes.length > 0 || u.router
}

function stateView(o: MockOutbound): OutboundState {
  const now = Date.now()
  const bytes = { bytesUp: Math.round(o.bytesUp), bytesDown: Math.round(o.bytesDown) }
  if (!inUse(o)) return { status: 'idle', delayMs: 0, error: '', onFallback: false, lastTryAt: null, lastSeenAt: null, bytesUp: 0, bytesDown: 0 }
  if (!entryIsOn() || now < o.checkedFrom) return { status: 'unknown', delayMs: 0, error: '', onFallback: false, lastTryAt: null, lastSeenAt: null, ...bytes }
  if (o.health === 'up') return { status: 'up', delayMs: o.delayMs, error: '', onFallback: false, lastTryAt: iso(now - 4000), lastSeenAt: iso(now - 4000), ...bytes }
  return { status: 'down', delayMs: 0, error: o.error, onFallback: o.fallback, lastTryAt: iso(now - 4000), lastSeenAt: iso(now - 2 * 3600e3), ...bytes }
}

/** As the hub shows it: no id, no password. */
function view(o: MockOutbound): Outbound {
  const { id: secret, password: _password, ...rest } = o.settings
  return {
    ...rest,
    id: o.id,
    name: o.name,
    fallback: o.fallback,
    createdAt: iso(o.createdAt),
    secretHint: secret ? `${secret.slice(0, 4)}…${secret.slice(-4)}` : '',
    usedBy: usedBy(o),
    state: stateView(o),
  }
}

const checkView = () => ({ url: checkUrl, seconds: SECONDS, defaultUrl: DEFAULT_URL })

/** Moving numbers: what goes through the outbounds that answer. */
export function tickOutbounds() {
  for (const o of outs) {
    if (!inUse(o) || !entryIsOn()) continue
    if (o.health === 'up' || o.fallback) {
      o.bytesUp += rand(40_000, 160_000)
      o.bytesDown += rand(200_000, 900_000)
    }
    if (o.health === 'up') o.delayMs = Math.max(40, o.delayMs + rand(-12, 12))
  }
}

// --- the hub's checks ------------------------------------------------------------------------

/** What the hub says of a name, or null. */
function nameProblem(raw: unknown, except = 0): [number, string, string] | null {
  const name = typeof raw === 'string' ? raw.trim() : ''
  if (!name) return [400, 'bad_request', 'name: the name is required']
  if ([...name].length > MAX_NAME) return [400, 'bad_request', `name: the name can have at most ${MAX_NAME} characters`]
  if (/[\u0000-\u001f\u007f\u2028\u2029]/.test(name)) return [400, 'bad_request', 'name: the name must not contain control characters or line breaks']
  if (outs.some((o) => o.id !== except && o.name.toLowerCase() === name.toLowerCase())) return [409, 'name_taken', `an outbound named ${name} already exists`]
  return null
}

const SS = ['aes-128-gcm', 'aes-256-gcm', 'chacha20-poly1305', 'chacha20-ietf-poly1305', 'xchacha20-poly1305', 'xchacha20-ietf-poly1305']
const SS2022: Record<string, number> = { '2022-blake3-aes-128-gcm': 16, '2022-blake3-aes-256-gcm': 32, '2022-blake3-chacha20-poly1305': 32 }

/** The settings as the hub reads them, or the sentence it refuses them with (which says the field). */
function readSettings(raw: unknown): OutboundSettings | string {
  if (!raw || typeof raw !== 'object') return 'settings: missing'
  const s = { ...blank, ...(raw as Partial<OutboundSettings>) }
  const net = s.network || 'tcp'
  const sec = s.security || 'none'
  if (!['vless', 'vmess', 'trojan', 'shadowsocks', 'socks', 'http'].includes(s.protocol)) return `protocol: "${s.protocol}" is not vless, vmess, trojan, shadowsocks, socks or http`
  if (!s.host || !(isIP(s.host) || isName(s.host))) return `host: "${s.host}" is not an IP address or a host name`
  if (!Number.isInteger(s.port) || s.port < 1 || s.port > 65535) return 'port: missing'
  if (!['tcp', 'ws', 'httpupgrade', 'xhttp'].includes(net)) return `network: "${net}" is not tcp, ws, httpupgrade or xhttp`
  if (!['none', 'tls', 'reality'].includes(sec)) return `security: "${sec}" is not none, tls or reality`
  if ((s.protocol === 'vless' || s.protocol === 'vmess') && !UUID.test(s.id)) return 'id: is not a UUID'
  if ((s.protocol === 'trojan' || s.protocol === 'shadowsocks') && !s.password) return 'password: missing'
  if (s.protocol === 'shadowsocks') {
    const size = SS2022[s.method]
    if (size) {
      for (const part of s.password.split(':')) {
        let n = -1
        try {
          n = atob(part).length
        } catch {
          // not base64
        }
        if (n !== size) return `password: ${s.method} takes a ${size}-byte key as base64`
      }
    } else if (!SS.includes(s.method)) return `method: "${s.method}" is not a Shadowsocks cipher the bridge carries`
  }
  if ((s.protocol === 'socks' || s.protocol === 'http') && !s.username !== !s.password) return 'username: a user name and a password go together'
  if (sec !== 'none' && !isName(s.serverName)) return `server_name: "${s.serverName}" is not a host name`
  if (sec === 'reality' && !/^[A-Za-z0-9_-]{43}$/.test(s.publicKey)) return 'public_key: not a 32-byte base64url key'
  if (sec === 'reality' && s.shortId && !/^([0-9a-f]{2}){1,8}$/i.test(s.shortId)) return `short_id: "${s.shortId}" must be 2 to 16 hex digits, in pairs`
  return { ...s, network: net, security: sec }
}

/** Who leaves through it, the way the hub's refusal names them. */
function usersSentence(o: MockOutbound): string {
  const u = usedBy(o)
  const names = [...(u.router ? ['This router'] : []), ...u.nodes.map((id) => nodes.find((n) => n.id === id)?.name ?? `node ${id}`)]
  const list = names.length > 1 ? `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}` : names[0]
  return `${list} ${names.length > 1 ? 'leave' : 'leaves'} through ${o.name}; choose another way out for ${names.length > 1 ? 'them' : 'it'} first`
}

/** Put in use from now: "not checked yet" for a moment, as the bridge restarts. */
const startUsing = (id: number) => {
  const o = outs.find((x) => x.id === id)
  if (o) o.checkedFrom = Date.now() + 3000 * stepPace()
}

// --- the calls ------------------------------------------------------------------------------

interface Call {
  method: string
  path: string
  body: Record<string, any>
  send: (status: number, data: unknown) => void
  fail: (status: number, code: string, error: string, extra?: object) => void
}

/** Answers the call and returns true when it is one of these, else returns false and touches nothing. */
export async function outboundApi({ method, path, body, send, fail }: Call): Promise<boolean> {
  const reply = (status: number, data: unknown) => (send(status, data), true)
  const refuse = (status: number, code: string, error: string) => (fail(status, code, error), true)

  if (path === '__outbound' && method === 'POST') {
    const o = outs.find((x) => x.id === Number(body.id))
    if (!o) return refuse(404, 'not_found', 'no such outbound')
    if (body.health === 'up' || body.health === 'down') o.health = body.health
    if (typeof body.error === 'string') o.error = body.error
    if (typeof body.delayMs === 'number') o.delayMs = body.delayMs
    o.checkedFrom = 0
    return reply(200, { ok: true })
  }

  if (path === 'outbounds' && method === 'GET') return reply(200, { outbounds: outs.map(view), limit, check: checkView() })

  if (path === 'outbounds' && method === 'POST') {
    if (outs.length >= limit) return refuse(409, 'outbound_limit', `this router keeps at most ${limit} outbounds`)
    const bad = nameProblem(body.name)
    if (bad) return refuse(...bad)
    const s = readSettings(body.settings)
    if (typeof s === 'string') return refuse(400, 'bad_request', s)
    await sleep(250)
    if (s.host.includes('refuse')) return refuse(400, 'bad_request', 'Xray refused these settings: failed to build outbound handler: invalid stream settings')
    const o = make(String(body.name).trim(), s, { createdAt: Date.now(), fallback: body.fallback === true, health: s.host.includes('down') ? 'down' : 'up', error: s.host.includes('down') ? 'connection refused' : '' })
    outs.push(o)
    return reply(201, view(o))
  }

  if (path === 'outbounds/test' && method === 'POST') {
    const s = readSettings(body.settings)
    if (typeof s === 'string') return refuse(400, 'bad_request', s)
    if (s.host.includes('notest')) return refuse(503, 'test_unavailable', 'the bridge program is not available here')
    await sleep(500)
    if (s.host.includes('flaky')) flaky = !flaky
    if (s.host.includes('fail') || (s.host.includes('flaky') && !flaky)) return reply(200, { ok: false, error: `dial tcp ${s.host}:${s.port}: i/o timeout` })
    return reply(200, { ok: true, delayMs: rand(90, 250), status: 204 })
  }

  if (path === 'outbounds/check' && method === 'PUT') {
    const url = typeof body.url === 'string' ? body.url.trim() : ''
    if (url) {
      let ok = url.length <= 255
      try {
        const u = new URL(url)
        ok = ok && (u.protocol === 'http:' || u.protocol === 'https:') && !!u.hostname && !u.username && !u.hash
      } catch {
        ok = false
      }
      if (!ok) return refuse(400, 'bad_request', `url: "${url}" is not an http or https address`)
    }
    checkUrl = url
    return reply(200, checkView())
  }

  const m = path.match(/^outbounds\/(\d+)(\/test)?$/)
  if (m) {
    const o = outs.find((x) => x.id === Number(m[1]))
    if (!o) return refuse(404, 'not_found', 'no such outbound')
    if (m[2] && method === 'POST') {
      if (o.settings.host.includes('notest')) return refuse(503, 'test_unavailable', 'the bridge program is not available here')
      await sleep(500)
      return reply(200, o.health === 'up' ? { ok: true, delayMs: Math.max(40, o.delayMs + rand(-20, 30)), status: 204 } : { ok: false, error: o.error })
    }
    if (!m[2] && method === 'PATCH') {
      if (body.name !== undefined) {
        const bad = nameProblem(body.name, o.id)
        if (bad) return refuse(...bad)
      }
      if (body.fallback !== undefined && typeof body.fallback !== 'boolean') return refuse(400, 'bad_request', 'fallback: must be true or false')
      let s: OutboundSettings | undefined
      if (body.settings !== undefined) {
        const read = readSettings(body.settings)
        if (typeof read === 'string') return refuse(400, 'bad_request', read)
        if (read.host.includes('refuse')) return refuse(400, 'bad_request', 'Xray refused these settings: failed to build outbound handler: invalid stream settings')
        s = read
      }
      await sleep(200)
      if (body.name !== undefined) o.name = String(body.name).trim()
      if (typeof body.fallback === 'boolean') o.fallback = body.fallback
      if (s) {
        o.settings = s
        o.health = s.host.includes('down') ? 'down' : 'up'
        o.error = o.health === 'down' ? 'connection refused' : ''
      }
      // new settings on one in use restart the bridge, so it is checked again
      if ((s || typeof body.fallback === 'boolean') && inUse(o)) startUsing(o.id)
      return reply(200, view(o))
    }
    if (!m[2] && method === 'DELETE') {
      if (inUse(o)) return refuse(409, 'outbound_in_use', usersSentence(o))
      outs = outs.filter((x) => x !== o)
      return reply(204, {})
    }
  }

  const r = path.match(/^nodes\/(\d+)\/route$/)
  if (r && method === 'PUT') {
    const n = nodes.find((x) => x.id === Number(r[1]))
    if (!n || !isNode(n)) return refuse(404, 'not_found', 'no such node')
    if (!n.registered) return refuse(409, 'busy', "the node isn't installed yet")
    const id = Number(body.outboundId)
    if (!Number.isInteger(id) || (id !== 0 && !outs.some((o) => o.id === id))) return refuse(400, 'bad_request', 'outboundId: no such outbound')
    await sleep(300)
    if (id) nodeRoutes.set(n.id, id)
    else nodeRoutes.delete(n.id)
    if (id) startUsing(id)
    return reply(200, detail(n) satisfies Node)
  }

  if (path === 'local/route' && method === 'PUT') {
    const id = Number(body.outboundId)
    if (!Number.isInteger(id) || (id !== 0 && !outs.some((o) => o.id === id))) return refuse(400, 'bad_request', 'outboundId: no such outbound')
    await sleep(300)
    localRoute = id
    if (id) startUsing(id)
    return reply(200, localView() satisfies Local)
  }

  return false
}
