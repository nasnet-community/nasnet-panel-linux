// Reads a share link or an Xray outbound JSON into the settings the hub takes. Ported from the
// Linux panel's web-panel/src/lib/link-parser.ts, then narrowed to what the bridge carries: anything
// else is refused with a reason the page words, never guessed at. It never throws.

import { MAX_OUTBOUND_NAME, type OutboundNetwork, type OutboundProtocol, type OutboundSecurity, type OutboundSettings } from './api'

/** Why a paste can't be used. `name` is the thing that isn't carried, as the link or JSON calls it. */
export type ReasonCode =
  | 'empty'
  | 'not_link'
  | 'many_links'
  | 'web_address'
  | 'bad_link'
  | 'bad_json'
  | 'no_outbound'
  | 'many_servers'
  | 'no_host'
  | 'bad_host'
  | 'bad_port'
  | 'no_id'
  | 'bad_id'
  | 'no_password'
  | 'login_half'
  | 'protocol'
  | 'transport'
  | 'security'
  | 'tcp_http'
  | 'ss_plugin'
  | 'xhttp_extra'
  | 'reality_transport'
  | 'vmess_alterid'
  | 'vmess_reality'
  | 'cipher'
  | 'flow'
  | 'vision_transport'
  | 'vless_encryption'
  | 'plain_tcp'
  | 'http_transport'
  | 'no_server_name'
  | 'no_public_key'
  | 'value'

/** Something read that the admin should know, which doesn't stop the outbound being used. */
export type NoticeCode = 'insecure' | 'vmess_cipher'

export interface Note<C extends string> {
  code: C
  name?: string
}

export type Parsed =
  | { ok: true; settings: OutboundSettings; name: string; notices: Note<NoticeCode>[]; from: 'link' | 'json' }
  | { ok: false; reason: Note<ReasonCode> }

export interface ParseOptions {
  /** the admin said a pasted http:// or https:// address is a proxy, not a web page */
  httpProxy?: boolean
}

class Refused extends Error {
  constructor(readonly note: Note<ReasonCode>) {
    super(note.code)
  }
}

const refuse = (code: ReasonCode, name?: string): never => {
  throw new Refused(name === undefined ? { code } : { code, name })
}

/** Reads `input`; never throws. */
export function parseOutbound(input: string, opts: ParseOptions = {}): Parsed {
  try {
    return parse(input, opts)
  } catch (e) {
    return { ok: false, reason: e instanceof Refused ? e.note : { code: 'bad_link' } }
  }
}

const SCHEME = /^([a-z][a-z0-9+.-]*):\/\//i

/** An http:// or https:// address, which is a proxy only when the admin says so. */
export const isWebAddress = (text: string) => /^https?:\/\//i.test(text.trim())

function parse(input: string, opts: ParseOptions): Parsed {
  const text = input.trim()
  if (!text) refuse('empty')
  if (text.startsWith('{') || text.startsWith('[')) return parseJson(text)
  const words = text.split(/\s+/)
  if (words.filter((w) => SCHEME.test(w)).length > 1) refuse('many_links')
  const m = text.match(SCHEME)
  if (!m) refuse('not_link')
  const scheme = m![1].toLowerCase()
  switch (scheme) {
    case 'vless':
    case 'trojan':
      return fromLink(vlessTrojan(text, scheme))
    case 'vmess':
      // a long base64 is sometimes wrapped over lines
      return fromLink(vmess(text.replace(/\s+/g, '')))
    case 'ss':
      return fromLink(shadowsocks(text))
    case 'socks':
    case 'socks5':
    case 'socks5h':
      return fromLink(proxy(text, 'socks'))
    case 'http':
    case 'https':
      if (!opts.httpProxy) refuse('web_address')
      return fromLink(proxy(text, 'http', scheme === 'https'))
    case 'wireguard':
    case 'wg':
      return refuse('protocol', 'WireGuard')
    case 'hysteria':
    case 'hysteria2':
    case 'hy2':
      return refuse('protocol', 'Hysteria')
    case 'tuic':
      return refuse('protocol', 'TUIC')
    case 'ssr':
      return refuse('protocol', 'ShadowsocksR')
    default:
      return refuse('protocol', scheme)
  }
}

// --- what a link or a JSON says, before the rules -------------------------------------------

/** Everything a link or a JSON can say, as it says it. finish() turns it into settings or a refusal. */
interface Raw {
  protocol: OutboundProtocol
  host: string
  /** '' when the link left it out: the protocol's usual port is used */
  port: string
  id?: string
  password?: string
  username?: string
  method?: string
  flow?: string
  encryption?: string
  alterId?: string
  network?: string
  headerType?: string
  path?: string
  hostHeader?: string
  mode?: string
  extra?: boolean
  security?: string
  sni?: string
  fingerprint?: string
  alpn?: string[]
  insecure?: boolean
  publicKey?: string
  shortId?: string
  spiderX?: string
  plugin?: string
  /** its remark or tag, for the name */
  name: string
}

const DEFAULT_PORT: Record<OutboundProtocol, number> = { vless: 443, vmess: 443, trojan: 443, shadowsocks: 8388, socks: 1080, http: 80 }

// --- links --------------------------------------------------------------------------------

interface LinkParts {
  userinfo: string
  host: string
  port: string
  query: string
  hash: string
}

// A split by hand instead of `new URL`: a password may hold "@", "/", "+" or "=", which the URL
// parser treats as the end of the authority or re-encodes. The last "@" before the query ends the
// userinfo.
function splitLink(link: string): LinkParts {
  let rest = link.slice(link.indexOf('://') + 3)
  let hash = ''
  const h = rest.indexOf('#')
  if (h !== -1) {
    hash = rest.slice(h + 1)
    rest = rest.slice(0, h)
  }
  let query = ''
  const q = rest.indexOf('?')
  if (q !== -1) {
    query = rest.slice(q + 1)
    rest = rest.slice(0, q)
  }
  let userinfo = ''
  const at = rest.lastIndexOf('@')
  if (at !== -1) {
    userinfo = rest.slice(0, at)
    rest = rest.slice(at + 1)
  }
  const slash = rest.indexOf('/')
  if (slash !== -1) rest = rest.slice(0, slash)
  const { host, port } = hostPort(rest)
  return { userinfo, host, port, query, hash }
}

/** host[:port], with an IPv6 address in brackets. */
function hostPort(s: string): { host: string; port: string } {
  if (s.startsWith('[')) {
    const close = s.indexOf(']')
    if (close === -1) refuse('bad_host')
    const after = s.slice(close + 1)
    return { host: s.slice(1, close), port: after.startsWith(':') ? after.slice(1) : '' }
  }
  const colon = s.lastIndexOf(':')
  // more than one colon and no brackets: an IPv6 address with no port
  if (colon === -1 || s.indexOf(':') !== colon) return { host: s, port: '' }
  return { host: s.slice(0, colon), port: s.slice(colon + 1) }
}

function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}

/**
 * A link's query, looked up ignoring case, "-" and "_" (generators disagree on allowInsecure and
 * allowinsecure). Values are percent-decoded, but a "+" stays a "+": it is part of keys and paths.
 */
function params(query: string): (...names: string[]) => string {
  const norm = (s: string) => s.toLowerCase().replace(/[-_]/g, '')
  const map = new Map<string, string>()
  for (const pair of query.split('&')) {
    if (!pair) continue
    const eq = pair.indexOf('=')
    const k = norm(safeDecode(eq === -1 ? pair : pair.slice(0, eq)))
    if (!map.has(k)) map.set(k, safeDecode(eq === -1 ? '' : pair.slice(eq + 1)))
  }
  return (...names) => {
    for (const n of names) {
      const v = map.get(norm(n))
      if (v) return v
    }
    return ''
  }
}

const truthy = (v: unknown) => v === true || v === 1 || (typeof v === 'string' && ['1', 'true', 'yes'].includes(v.trim().toLowerCase()))

/** "h2,http/1.1", once or twice percent-encoded. */
function splitAlpn(v: string): string[] {
  return v
    .replace(/%2C/gi, ',')
    .split(',')
    .map((s) => safeDecode(s.trim()))
    .filter(Boolean)
}

/** Standard or URL-safe base64, padded or not, as UTF-8 text; null when it isn't base64. */
export function decodeBase64(s: string): string | null {
  const b = s.replace(/\s+/g, '').replace(/-/g, '+').replace(/_/g, '/').replace(/=+$/, '')
  if (!b || !/^[A-Za-z0-9+/]*$/.test(b) || b.length % 4 === 1) return null
  try {
    const bin = atob(b + '='.repeat((4 - (b.length % 4)) % 4))
    return new TextDecoder('utf-8', { fatal: true }).decode(Uint8Array.from(bin, (c) => c.charCodeAt(0)))
  } catch {
    return null
  }
}

function fromLink(raw: Raw): Parsed {
  return finish(raw, 'link')
}

/** vless://id@host:port?type=…&security=…#remark, and trojan://password@… the same way. */
function vlessTrojan(link: string, protocol: 'vless' | 'trojan'): Raw {
  const p = splitLink(link)
  const q = params(p.query)
  const auth = safeDecode(p.userinfo)
  return {
    protocol,
    host: p.host,
    port: p.port,
    ...(protocol === 'vless' ? { id: auth, flow: q('flow'), encryption: q('encryption') } : { password: auth }),
    network: q('type', 'network'),
    headerType: q('headerType'),
    path: q('path'),
    hostHeader: q('host'),
    mode: q('mode'),
    extra: hasExtra(q('extra')),
    // trojan began as TLS only, so a link that doesn't say means TLS
    security: q('security') || (protocol === 'trojan' ? 'tls' : 'none'),
    sni: q('sni', 'peer', 'serverName'),
    fingerprint: q('fp', 'fingerprint'),
    alpn: splitAlpn(q('alpn')),
    insecure: truthy(q('allowInsecure')) || truthy(q('insecure')),
    publicKey: q('pbk', 'publicKey'),
    shortId: q('sid', 'shortId'),
    spiderX: q('spx', 'spiderX'),
    name: safeDecode(p.hash),
  }
}

/** xhttp's extra block, as a link carries it (JSON text); an empty one is no block. */
function hasExtra(v: string): boolean {
  if (!v.trim()) return false
  try {
    const x = JSON.parse(v)
    return !(x && typeof x === 'object' && Object.keys(x).length === 0)
  } catch {
    return true
  }
}

/** vmess://base64(JSON), the form v2rayN made common. */
function vmess(link: string): Raw {
  let body = link.slice(link.indexOf('://') + 3)
  let remark = ''
  const h = body.indexOf('#')
  if (h !== -1) {
    remark = safeDecode(body.slice(h + 1))
    body = body.slice(0, h)
  }
  const text = decodeBase64(body)
  if (text === null) return refuse('bad_link')
  let j: Record<string, unknown>
  try {
    j = JSON.parse(text)
  } catch {
    return refuse('bad_link')
  }
  if (!j || typeof j !== 'object' || Array.isArray(j)) return refuse('bad_link')
  const net = str(j.net).toLowerCase()
  return {
    protocol: 'vmess',
    host: str(j.add),
    port: str(j.port),
    id: str(j.id),
    alterId: str(j.aid),
    method: str(j.scy),
    network: net,
    // v2rayN keeps the header type in "type", and xhttp's mode there too
    headerType: net === 'xhttp' || net === 'splithttp' ? '' : str(j.type),
    mode: str(j.mode) || (net === 'xhttp' || net === 'splithttp' ? str(j.type) : ''),
    path: str(j.path),
    hostHeader: str(j.host),
    extra: typeof j.extra === 'string' ? hasExtra(j.extra) : isFull(j.extra),
    security: str(j.tls) || 'none',
    sni: str(j.sni),
    fingerprint: str(j.fp),
    alpn: splitAlpn(str(j.alpn)),
    insecure: truthy(j.allowInsecure) || truthy(j.insecure),
    publicKey: str(j.pbk),
    shortId: str(j.sid),
    spiderX: str(j.spx),
    name: str(j.ps) || remark,
  }
}

/**
 * ss://base64url(method:password)@host:port/?plugin=…#remark (SIP002, the userinfo percent-encoded
 * instead for 2022 ciphers), and the older ss://base64(method:password@host:port)#remark.
 */
function shadowsocks(link: string): Raw {
  let body = link.slice(link.indexOf('://') + 3)
  let remark = ''
  const h = body.indexOf('#')
  if (h !== -1) {
    remark = safeDecode(body.slice(h + 1))
    body = body.slice(0, h)
  }
  let query = ''
  const qi = body.indexOf('?')
  if (qi !== -1) {
    query = body.slice(qi + 1)
    body = body.slice(0, qi)
  }
  body = body.replace(/\/+$/, '')
  if (!body.includes('@')) {
    const whole = decodeBase64(safeDecode(body))
    if (whole === null || !whole.includes('@')) return refuse('bad_link')
    body = whole
  }
  const at = body.lastIndexOf('@')
  const { host, port } = hostPort(body.slice(at + 1))
  // new URL turns base64 padding into %3D, so it is decoded first; a base64 string has no ":"
  let user = safeDecode(body.slice(0, at))
  if (!user.includes(':')) {
    const decoded = decodeBase64(user)
    if (decoded === null || !decoded.includes(':')) return refuse('bad_link')
    user = decoded
  }
  // the first ":" only: a 2022 password may be "server key:user key"
  const colon = user.indexOf(':')
  const q = params(query)
  return {
    protocol: 'shadowsocks',
    host,
    port,
    method: user.slice(0, colon).toLowerCase(),
    password: user.slice(colon + 1),
    plugin: q('plugin'),
    network: q('type', 'network'),
    security: q('security'),
    name: remark,
  }
}

/** socks://user:pass@host:port, v2rayN's socks://base64(user:pass)@…, and http(s)://user:pass@host:port as a proxy. */
function proxy(link: string, protocol: 'socks' | 'http', tls = false): Raw {
  const p = splitLink(link)
  let user = safeDecode(p.userinfo)
  if (user && !user.includes(':')) {
    const decoded = decodeBase64(user)
    if (decoded !== null && decoded.includes(':')) user = decoded
  }
  const colon = user.indexOf(':')
  return {
    protocol,
    host: p.host,
    port: p.port || (tls ? '443' : ''),
    username: colon === -1 ? user : user.slice(0, colon),
    password: colon === -1 ? '' : user.slice(colon + 1),
    security: tls ? 'tls' : 'none',
    name: safeDecode(p.hash),
  }
}

// --- Xray JSON ------------------------------------------------------------------------------

const NOT_PROXY = new Set(['freedom', 'blackhole', 'dns', 'loopback'])

const str = (v: unknown): string => (typeof v === 'string' ? v.trim() : typeof v === 'number' && Number.isFinite(v) ? String(v) : '')
const obj = (v: unknown): Record<string, unknown> => (v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {})
const isFull = (v: unknown) => (Array.isArray(v) ? v.length > 0 : Object.keys(obj(v)).length > 0)

/** One outbound object, a whole config, or a list of outbounds: the first proxy outbound is read. */
function parseJson(text: string): Parsed {
  let v: unknown
  try {
    v = JSON.parse(text)
  } catch {
    return refuse('bad_json')
  }
  const top = obj(v)
  const list: unknown[] = Array.isArray(v) ? v : Array.isArray(top.outbounds) ? top.outbounds : [v]
  const found = list.find((o) => typeof obj(o).protocol === 'string' && !NOT_PROXY.has(str(obj(o).protocol).toLowerCase()))
  if (!found) return refuse('no_outbound')
  const o = obj(found)
  return finish(fromJson(o, str(top.remarks) || str(o.tag)), 'json')
}

function fromJson(o: Record<string, unknown>, name: string): Raw {
  const kind = str(o.protocol).toLowerCase()
  const protocol: OutboundProtocol =
    kind === 'vless' || kind === 'vmess' || kind === 'trojan' || kind === 'socks' || kind === 'http' ? kind
    : kind === 'shadowsocks' || kind === 'ss' ? 'shadowsocks'
    : kind === 'wireguard' ? refuse('protocol', 'WireGuard')
    : kind.startsWith('hysteria') ? refuse('protocol', 'Hysteria')
    : refuse('protocol', str(o.protocol))
  const s = obj(o.settings)

  // the server and its one user: vnext (vless, vmess), servers (the others), or the newer flat form
  let server = s
  let user = s
  if (Array.isArray(s.vnext)) {
    if (s.vnext.length !== 1) refuse('many_servers')
    server = obj(s.vnext[0])
    const users = Array.isArray(server.users) ? server.users : []
    if (users.length !== 1) refuse('many_servers')
    user = obj(users[0])
  } else if (Array.isArray(s.servers)) {
    if (s.servers.length !== 1) refuse('many_servers')
    server = obj(s.servers[0])
    user = server
    if (protocol === 'socks' || protocol === 'http') {
      const users = Array.isArray(server.users) ? server.users : []
      if (users.length > 1) refuse('many_servers')
      user = obj(users[0])
    }
  }

  const st = obj(o.streamSettings)
  const network = str(st.network).toLowerCase()
  const security = str(st.security).toLowerCase()
  const tcp = obj(st.tcpSettings ?? st.rawSettings)
  const ws = obj(st.wsSettings)
  const hu = obj(st.httpupgradeSettings)
  const xh = obj(st.xhttpSettings ?? st.splithttpSettings)
  const transport = network === 'ws' ? ws : network === 'httpupgrade' ? hu : network === 'xhttp' || network === 'splithttp' ? xh : {}
  const tls = obj(st.tlsSettings)
  const reality = obj(st.realitySettings)
  const sec = security === 'reality' ? reality : tls
  const alpn = Array.isArray(tls.alpn) ? tls.alpn.map(str).filter(Boolean) : splitAlpn(str(tls.alpn))

  return {
    protocol,
    host: str(server.address),
    port: str(server.port),
    id: protocol === 'vless' || protocol === 'vmess' ? str(user.id) : undefined,
    flow: str(user.flow),
    encryption: str(user.encryption),
    alterId: str(user.alterId),
    method: protocol === 'vmess' ? str(user.security) : protocol === 'shadowsocks' ? str(server.method).toLowerCase() : undefined,
    password: protocol === 'trojan' || protocol === 'shadowsocks' ? str(server.password) : protocol === 'socks' || protocol === 'http' ? str(user.pass ?? user.password) : undefined,
    username: protocol === 'socks' || protocol === 'http' ? str(user.user ?? user.username) : undefined,
    network,
    headerType: str(obj(tcp.header).type),
    path: str(transport.path),
    hostHeader: str(transport.host) || str(obj(transport.headers).Host) || str(obj(transport.headers).host),
    mode: str(transport.mode),
    extra: isFull(transport.extra),
    security: security || 'none',
    sni: str(sec.serverName),
    fingerprint: str(sec.fingerprint),
    alpn: security === 'tls' ? alpn : [],
    insecure: truthy(tls.allowInsecure),
    // newer Xray calls REALITY's public key "password"
    publicKey: str(reality.publicKey) || str(reality.password),
    shortId: str(reality.shortId),
    spiderX: str(reality.spiderX),
    plugin: protocol === 'shadowsocks' ? str(server.plugin) : '',
    name,
  }
}

// --- the rules ------------------------------------------------------------------------------

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const LABEL = /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/
const IPV4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/

const isIPv6 = (s: string) => s.includes(':') && /^[0-9a-fA-F:.]+$/.test(s)
/** A host name, not an address: what TLS can ask for. */
export const isHostName = (s: string) => !!s && s.length <= 253 && !IPV4.test(s) && !/^\d+$/.test(s.split('.').pop() ?? '') && s.split('.').every((l) => LABEL.test(l))
const isHost = (s: string) => IPV4.test(s) || isIPv6(s) || isHostName(s)

const SS_CIPHERS = new Set([
  'aes-128-gcm',
  'aes-256-gcm',
  'chacha20-poly1305',
  'chacha20-ietf-poly1305',
  'xchacha20-poly1305',
  'xchacha20-ietf-poly1305',
  '2022-blake3-aes-128-gcm',
  '2022-blake3-aes-256-gcm',
  '2022-blake3-chacha20-poly1305',
])
const VMESS_CIPHERS = new Set(['', 'auto', 'aes-128-gcm', 'chacha20-poly1305'])
const XHTTP_MODES = new Set(['', 'auto', 'packet-up', 'stream-up', 'stream-one'])
const VISION = 'xtls-rprx-vision'

const NETWORKS: Record<string, OutboundNetwork> = { '': 'tcp', tcp: 'tcp', raw: 'tcp', ws: 'ws', websocket: 'ws', httpupgrade: 'httpupgrade', xhttp: 'xhttp', splithttp: 'xhttp' }
// the transports that aren't carried, by the name people know them by
const OTHER_NETWORKS: Record<string, string> = { grpc: 'gRPC', gun: 'gRPC', kcp: 'mKCP', mkcp: 'mKCP', quic: 'QUIC', h2: 'HTTP/2 (h2)', http: 'HTTP/2 (h2)' }
export const NETWORK_NAMES: Record<OutboundNetwork, string> = { tcp: 'TCP', ws: 'WebSocket', httpupgrade: 'HTTPUpgrade', xhttp: 'XHTTP' }

/** A remark as a name the hub takes: no control characters, at most 40 characters. */
export function cleanName(s: string): string {
  const plain = s.replace(/[\u0000-\u001f\u007f-\u009f\u2028\u2029]/g, ' ').replace(/\s+/g, ' ').trim()
  return [...plain].slice(0, MAX_OUTBOUND_NAME).join('').trim()
}

function finish(r: Raw, from: 'link' | 'json'): Parsed {
  const notices: Note<NoticeCode>[] = []

  const host = r.host.trim().replace(/^\[|\]$/g, '')
  if (!host) refuse('no_host')
  if (!isHost(host)) refuse('bad_host')
  const port = r.port.trim() === '' ? DEFAULT_PORT[r.protocol] : /^\d{1,5}$/.test(r.port.trim()) ? Number(r.port.trim()) : 0
  if (!(port >= 1 && port <= 65535)) refuse('bad_port')

  const rawNet = (r.network ?? '').toLowerCase()
  const network = NETWORKS[rawNet] ?? refuse('transport', OTHER_NETWORKS[rawNet] ?? r.network)

  let id = ''
  let password = ''
  let username = ''
  let method = ''
  let flow = ''
  switch (r.protocol) {
    case 'vless':
      id = (r.id ?? '').trim()
      if (!id) refuse('no_id')
      if (!UUID.test(id)) refuse('bad_id')
      if (r.encryption && r.encryption.toLowerCase() !== 'none') refuse('vless_encryption', r.encryption)
      flow = (r.flow ?? '').toLowerCase()
      if (flow && flow !== VISION) refuse('flow', r.flow)
      break
    case 'vmess': {
      id = (r.id ?? '').trim()
      if (!id) refuse('no_id')
      if (!UUID.test(id)) refuse('bad_id')
      if (Number(r.alterId || 0) > 0) refuse('vmess_alterid')
      method = (r.method ?? '').toLowerCase()
      if (method === 'none' || method === 'zero') {
        notices.push({ code: 'vmess_cipher', name: method })
        method = 'auto'
      }
      if (!VMESS_CIPHERS.has(method)) refuse('cipher', r.method)
      break
    }
    case 'trojan':
      password = r.password ?? ''
      if (!password) refuse('no_password')
      break
    case 'shadowsocks':
      if (r.plugin) refuse('ss_plugin', r.plugin.split(';')[0])
      method = r.method ?? ''
      password = r.password ?? ''
      if (!SS_CIPHERS.has(method)) refuse('cipher', method || '–')
      if (!password) refuse('no_password')
      break
    case 'socks':
    case 'http':
      username = r.username ?? ''
      password = r.password ?? ''
      if (!username !== !password) refuse('login_half')
      break
  }

  const headerType = (r.headerType ?? '').toLowerCase()
  if (network === 'tcp' && headerType === 'http') refuse('tcp_http')
  if (network === 'tcp' && headerType && headerType !== 'none') refuse('value', `headerType=${r.headerType}`)
  if (network === 'xhttp' && r.extra) refuse('xhttp_extra')
  const mode = network === 'xhttp' ? (r.mode ?? '').toLowerCase() : ''
  if (!XHTTP_MODES.has(mode)) refuse('value', `mode=${r.mode}`)

  const rawSec = (r.security ?? '').toLowerCase()
  const security: OutboundSecurity =
    rawSec === '' || rawSec === 'none' ? 'none'
    : rawSec === 'tls' || rawSec === 'reality' ? rawSec
    : refuse('security', rawSec === 'xtls' ? 'XTLS' : r.security)

  if (r.protocol === 'vmess' && security === 'reality') refuse('vmess_reality')
  if (security === 'reality' && (network === 'ws' || network === 'httpupgrade')) refuse('reality_transport', NETWORK_NAMES[network])
  if (flow === VISION && (network !== 'tcp' || security === 'none')) refuse('vision_transport')
  if (r.protocol === 'shadowsocks' && (network !== 'tcp' || security !== 'none')) refuse('plain_tcp', 'Shadowsocks')
  if (r.protocol === 'socks' && (network !== 'tcp' || security !== 'none')) refuse('plain_tcp', 'SOCKS')
  if (r.protocol === 'http' && (network !== 'tcp' || security === 'reality')) refuse('http_transport')

  // the transport's own settings; tcp has none
  let path = ''
  let hostHeader = ''
  if (network !== 'tcp') {
    path = (r.path ?? '').trim()
    if (path && !path.startsWith('/')) path = `/${path}`
    // h2-style lists of hosts: the first is the one sent
    hostHeader = (r.hostHeader ?? '').split(',')[0].trim()
  }

  let serverName = ''
  let fingerprint = ''
  let alpn: string[] = []
  let publicKey = ''
  let shortId = ''
  let spiderX = ''
  if (security !== 'none') {
    // with no sni, a name the link has: its address, else (on an IP) the Host it sends
    serverName = (r.sni ?? '').trim() || (isHostName(host) ? host : isHostName(hostHeader) ? hostHeader : '')
    if (!serverName) refuse('no_server_name')
    fingerprint = (r.fingerprint ?? '').trim().toLowerCase() || 'chrome'
    if (r.insecure) notices.push({ code: 'insecure' })
  }
  if (security === 'tls') alpn = r.alpn ?? []
  if (security === 'reality') {
    publicKey = (r.publicKey ?? '').trim()
    if (!publicKey) refuse('no_public_key')
    shortId = (r.shortId ?? '').trim()
    spiderX = (r.spiderX ?? '').trim()
  }

  const settings: OutboundSettings = {
    protocol: r.protocol,
    host,
    port,
    id,
    password,
    username,
    method,
    flow,
    network,
    path,
    hostHeader,
    mode,
    security,
    serverName,
    fingerprint,
    alpn,
    publicKey,
    shortId,
    spiderX,
  }
  return { ok: true, settings, name: cleanName(r.name) || host, notices, from }
}
