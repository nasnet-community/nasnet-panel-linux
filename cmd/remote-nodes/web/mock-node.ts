// The mock's answers for the node page and the router's Settings tab: a node's detail with its
// events, traffic and keys, renaming, settings, the handshake check, updates, certificate,
// key rotation, removal, and the backup. mock-api.ts calls nodeApi() first on every request.
// Development only; never built into the page.
//
// Each long call starts a job with realistic steps (an upload reports its percentage), and
// the node ends up changed the way the hub would leave it. A node with a job running refuses
// another (409 busy). Calls that go to the agent answer 502 node_unreachable for a node that is down.
//
// What a scenario can set, with POST /api/__node { id, ... } after a reset (e2e/node-helpers.ts):
//   xrayVersion    "26.10.18" puts the node in the Xray mismatch state (tunnel off, portal up)
//   agentVersion   an old agent, so an update is due
//   down           the router can't reach it (true), or it is back (false)
//   registered     false: the install never finished
//   crash          Xray keeps crashing;  stopped: Xray is not running
//   notApplied     the settings failed to apply, with this error from the agent
//   certDays       days left on the node's certificate
//   hostKeyChanged the SSH host key now differs from the pinned one (the remove dialog's login stage)
//   failNext       the next job of this kind ("update-xray", "renew-cert", ...) fails at its second step
//   noSubscriptions true: every subscription is switched off on this node
//   bridgeDown     the router's bridge is not running: the tunnel is off for that
//   unmeasured     what the hub has no source for is 0 and false, as it sends it: the agent's own uptime and
//                  memory, whether its settings are applied, and the tunnel's round trip
//
// Handshake targets: one containing "fail" answers with a refused TLS 1.3 handshake, "big" with a
// handshake over the 8 KB the tunnel allows; anything else passes.
//
// Backups are a fake: the file is "RNBK1" and the nodes as base64 JSON with a hash of the
// passphrase, so a wrong passphrase or a wrong file is told apart the way the hub tells them.
// An import needs an empty install (reset with { fresh: true }). Export sets
// config.backup.lastExportAt, which a reset clears.
import { createHash } from 'node:crypto'
import type { ServerResponse } from 'node:http'
import type { HandshakeCheck, JobKind, Node, Traffic } from './src/lib/api'
import { NAME_TOO_LONG, STEPS, claimNodeId, configView, event, node as makeNode, nodes, startJob, subs, tooLong, view, type MockNode, type Step } from './mock-api'

const DAY = 86400e3
const H = 3600e3
const MIN = 60e3
const iso = (ms: number) => new Date(ms).toISOString().replace(/\.\d+Z$/, 'Z')
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
const MB = 1e6

export interface Ctx {
  method: string
  path: string
  body: Record<string, any>
  res: ServerResponse
  send: (status: number, data: unknown) => void
  fail: (status: number, code: string, error: string, extra?: object) => void
}

/** What the node pages need to know about a node beyond what the sample node carries. */
interface Extra {
  crash?: boolean
  stopped?: boolean
  notApplied?: string
  /** when its certificate was issued */
  certBase?: number
  keys?: { publicKey: string; shortId: string }
  /** a server name of its own; empty or missing: the handshake target's */
  sni?: string
  check?: { ok: boolean; at: number }
  hostKeyChanged?: boolean
  failNext?: string
  dialFailures?: number
  bridgeDown?: boolean
  unmeasured?: boolean
}

const extras = new Map<number, Extra>()
/** Nodes with a job running on them. */
const busy = new Set<number>()
let lastExportAt: number | null = null

function ex(n: MockNode): Extra {
  let x = extras.get(n.id)
  if (!x) {
    extras.set(n.id, (x = {}))
    // a node that has been around a while has had its certificate renewed once
    if (n.createdAt <= Date.now() - 7 * DAY) event(n, 'cert_renewed', '', Date.now() - 6 * DAY)
  }
  return x
}

const randomKey = () => {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_'
  return Array.from({ length: 43 }, () => chars[Math.floor(Math.random() * chars.length)]).join('')
}
const randomHex = (n: number) => Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('')

const reachable = (n: MockNode) => !n.down && n.registered && !n.installJob

// --- the node, as the detail page reads it ------------------------------------------

function detail(n: MockNode): Node {
  const x = ex(n)
  const v = view(n, true)
  const base = x.certBase ?? n.createdAt
  const live = reachable(n)
  if (v.agent) {
    if (x.stopped || x.crash) v.agent.xray = { ...v.agent.xray, running: false, crashLoop: !!x.crash, restarts: x.crash ? 14 : 0, lastExit: x.crash ? 'exit status 1: config: address already in use' : '' }
    if (x.notApplied) Object.assign(v.agent, { settingsApplied: false, lastPushError: x.notApplied })
  }
  if (x.stopped || x.crash) v.tunnel = { ...v.tunnel, state: v.tunnel.state === 'up' ? 'down' : v.tunnel.state, since: null, rttMs: 0, streams: 0 }
  if (x.bridgeDown && v.tunnel.state === 'up') v.tunnel = { ...v.tunnel, state: 'off', since: null, rttMs: 0, streams: 0, reason: 'bridge_down' }
  if (x.unmeasured) {
    if (v.agent) Object.assign(v.agent, { uptimeSec: 0, rssBytes: 0, settingsApplied: false, lastPushError: '' })
    v.tunnel = { ...v.tunnel, rttMs: 0 }
  }
  // the sample node is sure of its handshake target, which it checked when it was added
  const check = x.check ?? (x.check = { ok: true, at: Date.now() - 2 * H })
  v.tunnel.dialFailures = x.dialFailures ?? (live && v.tunnel.state === 'up' ? 3 : 0)
  // nothing flows through a tunnel that is off
  if (v.tunnel.state !== 'up') Object.assign(v.usage, { rateUp: 0, rateDown: 0 })
  return {
    ...v,
    publicKey: x.keys?.publicKey ?? v.publicKey,
    shortId: x.keys?.shortId ?? v.shortId,
    sni: x.sni || v.sni,
    sniCustom: !!x.sni,
    handshakeCheck: n.registered && !n.installJob ? { ok: check.ok, checkedAt: iso(check.at) } : undefined,
    cert: { notAfter: iso(base + 365 * DAY), validDays: Math.round((base + 365 * DAY - Date.now()) / DAY), renewsAt: iso(base + 335 * DAY) },
  }
}

// --- traffic -------------------------------------------------------------------------

// the same number for the same (node, 15 s step) every time it is asked, so the line scrolls instead of jumping
const hash = (a: number, b: number) => {
  let h = Math.imul(a ^ 0x9e3779b9, 0x85ebca6b) ^ Math.imul(b + 0x7f4a7c15, 0xc2b2ae35)
  h ^= h >>> 13
  h = Math.imul(h, 0x27d4eb2f)
  h ^= h >>> 15
  return (h >>> 0) / 4294967296
}
const smooth = (seed: number, step: number) => {
  const x = step / 4
  const i = Math.floor(x)
  const f = x - i
  const s = f * f * (3 - 2 * f)
  return hash(seed, i) * (1 - s) + hash(seed, i + 1) * s
}

function traffic(n: MockNode): Traffic {
  const stepSec = 15
  const step = stepSec * 1000
  const flows = detail(n).tunnel.state === 'up'
  const last = Math.floor(Date.now() / step)
  const samples: Traffic['samples'] = []
  // nothing to read from a node the router can't reach or that is still installing
  if (reachable(n)) {
    for (let k = last - 60; k <= last; k++) {
      samples.push({
        at: iso(k * step),
        down: flows ? Math.round((0.6 + 1.3 * smooth(n.id * 31 + 1, k)) * MB) : 0,
        up: flows ? Math.round((0.15 + 0.27 * smooth(n.id * 31 + 2, k)) * MB) : 0,
      })
    }
  }
  return { stepSec, samples }
}

// --- jobs ----------------------------------------------------------------------------

const FAILURE: Partial<Record<JobKind, string>> = {
  'update-agent': 'the new agent did not start: exit status 2',
  'update-xray': 'the download does not match the pinned checksum',
  'renew-cert': 'the node refused the certificate: x509: unknown authority',
  'rotate-keys': 'xray -test failed on the node: invalid short id',
  'change-settings': 'xray -test failed on the node: port already in use',
}

/** Starts a job on a node, and keeps the node busy until it ends. */
function run(kind: JobKind, n: MockNode, steps: Step[], o: { target?: string; end?: () => void } = {}) {
  const x = ex(n)
  busy.add(n.id)
  let list = steps
  // a scenario can make the next job of a kind fail, at its second step
  if (x.failNext === kind) {
    delete x.failNext
    list = steps.map((s, i) => (i === 1 ? { ...s, fail: FAILURE[kind] ?? 'it failed' } : s))
  }
  return startJob(kind, list, {
    nodeId: n.id,
    target: o.target ?? '',
    end: () => {
      busy.delete(n.id)
      o.end?.()
    },
  })
}

// --- handshake checks ----------------------------------------------------------------

const TARGET = /^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?:\d{1,5}$/
const goodTarget = (t: unknown): t is string => typeof t === 'string' && TARGET.test(t) && Number(t.split(':')[1]) >= 1 && Number(t.split(':')[1]) <= 65535

function handshake(target: string): HandshakeCheck {
  if (target.includes('fail'))
    return { ok: false, tls13: false, x25519: false, handshakeBytes: 0, latencyMs: 0, error: `${target} did not complete a TLS 1.3 handshake: remote error: tls: protocol version not supported` }
  if (target.includes('big'))
    return { ok: false, tls13: true, x25519: true, handshakeBytes: 9632, latencyMs: 71, error: 'the handshake is 9.4 KB, over the 8 KB limit' }
  return { ok: true, tls13: true, x25519: true, handshakeBytes: 4200, latencyMs: 38, error: '' }
}

// --- backup --------------------------------------------------------------------------

const MAGIC = 'RNBK1\n'
const proof = (passphrase: string) => createHash('sha256').update(`remote-nodes:${passphrase}`).digest('hex')

function backupFile(passphrase: string): Buffer {
  const payload = {
    proof: proof(passphrase),
    // every node in the state, as the hub's backup holds them: one that never finished comes back unfinished
    nodes: nodes.map((n) => ({ id: n.id, name: n.name, host: n.host, entryPort: n.entryPort, handshakeTarget: n.handshakeTarget, hostKey: n.hostKey, arch: n.arch, registered: n.registered })),
  }
  return Buffer.from(MAGIC + Buffer.from(JSON.stringify(payload)).toString('base64'))
}

/** The nodes a backup holds, or the code the hub answers with. */
function openBackup(passphrase: string, data: string): { nodes: { id: number; name: string; host: string; entryPort: number; handshakeTarget: string; hostKey: string; arch: string; registered: boolean }[] } | 'backup_bad_file' | 'backup_bad_passphrase' {
  const text = Buffer.from(data, 'base64').toString('utf8')
  if (!text.startsWith(MAGIC)) return 'backup_bad_file'
  let payload: any
  try {
    payload = JSON.parse(Buffer.from(text.slice(MAGIC.length), 'base64').toString('utf8'))
  } catch {
    return 'backup_bad_file'
  }
  if (!payload || !Array.isArray(payload.nodes) || typeof payload.proof !== 'string') return 'backup_bad_file'
  return payload.proof === proof(passphrase) ? { nodes: payload.nodes } : 'backup_bad_passphrase'
}

// --- the routes ----------------------------------------------------------------------

const validPort = (v: unknown) => Number.isInteger(v) && (v as number) >= 1 && (v as number) <= 65535

/** Answers the node pages' calls. Returns false for every other call, and for a reset, which it only notes. */
export async function nodeApi({ method, path, body, res, send, fail }: Ctx): Promise<boolean> {
  if (path === '__reset' && method === 'POST') {
    extras.clear()
    busy.clear()
    lastExportAt = null
    return false
  }

  if (path === 'config' && method === 'GET') {
    send(200, { ...configView(), backup: { lastExportAt: lastExportAt === null ? null : iso(lastExportAt) } })
    return true
  }

  if (path === 'backup/export' && method === 'POST') {
    const passphrase = String(body.passphrase ?? '')
    if (!passphrase) return fail(400, 'bad_request', 'give a passphrase'), true
    await sleep(250)
    lastExportAt = Date.now()
    const day = new Date().toISOString().slice(0, 10)
    res.statusCode = 200
    res.setHeader('Content-Type', 'application/octet-stream')
    // as Go writes it: a name that is plain ASCII needs no quotes
    res.setHeader('Content-Disposition', `attachment; filename=remote-nodes-backup-${day}.rnbackup`)
    res.end(backupFile(passphrase))
    return true
  }

  if (path === 'backup/import' && method === 'POST') {
    const passphrase = String(body.passphrase ?? '')
    const data = String(body.data ?? '')
    if (!passphrase || !data) return fail(400, 'bad_request', 'give a passphrase and a file'), true
    await sleep(350)
    const opened = openBackup(passphrase, data)
    if (opened === 'backup_bad_file') return fail(400, 'backup_bad_file', 'this is not a Remote Nodes backup'), true
    if (opened === 'backup_bad_passphrase') return fail(400, 'backup_bad_passphrase', 'the passphrase does not open this backup'), true
    if (nodes.length) return fail(409, 'bad_request', 'a backup can only be imported on an empty install, with no nodes'), true
    // the nodes come back as they were, already trusting this router, so none needs a login
    for (const b of opened.nodes) nodes.push(makeNode({ ...b, id: claimNodeId(b.id) }))
    send(200, { ok: true, nodes: opened.nodes.length })
    return true
  }

  // test hook: put one node in a state
  if (path === '__node' && method === 'POST') {
    const n = nodes.find((x) => x.id === Number(body.id))
    if (!n) return fail(404, 'not_found', 'no such node'), true
    const x = ex(n)
    const now = Date.now()
    if (typeof body.xrayVersion === 'string' && body.xrayVersion !== n.xrayVersion) {
      n.xrayVersion = body.xrayVersion
      const pinned = view(n).updates
      if (pinned.xray) {
        event(n, 'tunnel_down', `Xray ${n.xrayVersion} doesn't match ${pinned.xrayVersion}`, now - 2 * MIN)
        // earlier than the sample node's own events, so their order does not depend on the second a test ran in
        event(n, 'xray_changed', 'on the node', now - 3 * H)
      }
    }
    if (typeof body.agentVersion === 'string') n.agentVersion = body.agentVersion
    if (typeof body.down === 'boolean') {
      n.down = body.down
      if (body.down) {
        n.lastSeen = now - 22 * MIN
        event(n, 'node_unreachable', 'no answer over the Domestic link', n.lastSeen)
      } else n.lastSeen = now
    }
    if (typeof body.registered === 'boolean') n.registered = body.registered
    if (typeof body.crash === 'boolean') x.crash = body.crash
    if (typeof body.stopped === 'boolean') x.stopped = body.stopped
    if (typeof body.notApplied === 'string') x.notApplied = body.notApplied || undefined
    if (typeof body.certDays === 'number') x.certBase = now - (365 - body.certDays) * DAY
    if (typeof body.hostKeyChanged === 'boolean') x.hostKeyChanged = body.hostKeyChanged
    if (typeof body.failNext === 'string') x.failNext = body.failNext || undefined
    if (typeof body.dialFailures === 'number') x.dialFailures = body.dialFailures
    if (body.noSubscriptions === true) for (const sub of subs) sub.on.delete(n.id)
    if (typeof body.bridgeDown === 'boolean') x.bridgeDown = body.bridgeDown
    if (typeof body.unmeasured === 'boolean') x.unmeasured = body.unmeasured
    send(200, { ok: true })
    return true
  }

  // a node with a changed SSH key: the host key check the remove dialog starts with sees it
  if (path === 'probe' && method === 'POST') {
    const n = nodes.find((x) => x.host === String(body.host ?? '').trim() && extras.get(x.id)?.hostKeyChanged)
    if (n) {
      send(200, { fingerprint: 'SHA256:u3Rq0Q1uV3Jc0mYw7b6o8xk3j2m9Pq5c1f0aZyXwVb8', status: 'changed' })
      return true
    }
    return false
  }

  const m = path.match(/^nodes\/(\d+)(?:\/(.*))?$/)
  if (!m) return false
  const n = nodes.find((x) => x.id === Number(m[1]))
  const rest = m[2] ?? ''
  const known = (method === 'GET' && (rest === '' || rest === 'traffic')) || ['PATCH:', 'PATCH:settings', 'POST:handshake-check', 'POST:check-updates', 'POST:update', 'POST:renew-cert', 'POST:rotate-keys', 'POST:remove'].includes(`${method}:${rest}`)
  // the node's own list of subscriptions is another page's
  if (!known) return false
  if (!n) return fail(404, 'not_found', 'no such node'), true

  if (rest === '' && method === 'GET') return send(200, detail(n)), true
  if (rest === 'traffic') return send(200, traffic(n)), true

  if (rest === '' && method === 'PATCH') {
    const name = String(body.name ?? '').trim()
    if (!name) return fail(400, 'bad_request', 'give the node a name'), true
    if (tooLong(name)) return fail(400, 'bad_request', NAME_TOO_LONG), true
    if (nodes.some((x) => x !== n && x.name.toLowerCase() === name.toLowerCase())) return fail(409, 'name_taken', `a node named ${name} already exists`), true
    n.name = name
    return send(200, detail(n)), true
  }

  // everything below needs the agent, except taking a node out
  const remove = rest === 'remove'
  if (!remove && !n.registered) return fail(409, 'bad_request', 'this node was never fully installed; it can only be removed'), true
  if (!remove && !reachable(n)) return fail(502, 'node_unreachable', `the router can't reach ${n.host}`), true
  if (busy.has(n.id) || n.installJob) return fail(409, 'busy', `a job is already running on ${n.name}`), true

  const x = ex(n)
  const pin = view(n).updates

  if (rest === 'handshake-check') {
    const target = String(body.target ?? '').trim()
    if (!goodTarget(target)) return fail(400, 'bad_request', 'give the target as host:port, such as www.example.com:443'), true
    await sleep(450)
    const result = handshake(target)
    if (target === n.handshakeTarget) x.check = { ok: result.ok, at: Date.now() }
    return send(200, result), true
  }

  if (rest === 'check-updates') {
    await sleep(500)
    return send(200, detail(n)), true
  }

  if (rest === 'settings') {
    const { entryPort, handshakeTarget, sni } = body
    if (entryPort === undefined && handshakeTarget === undefined && sni === undefined) return fail(400, 'bad_request', 'nothing to change'), true
    if (entryPort !== undefined && (!validPort(entryPort) || entryPort === n.sshPort)) return fail(400, 'bad_request', 'the entry port must be 1 to 65535 and differ from the SSH port'), true
    if (handshakeTarget !== undefined && !goodTarget(handshakeTarget)) return fail(400, 'bad_request', 'give the target as host:port, such as www.example.com:443'), true
    if (sni !== undefined && (typeof sni !== 'string' || /\s/.test(sni) || sni.length > 253)) return fail(400, 'bad_request', 'the server name must be a host name'), true
    const steps: Step[] = []
    if (handshakeTarget !== undefined) {
      const result = handshake(handshakeTarget)
      steps.push({ ...STEPS.chooseTarget, detail: handshakeTarget, ms: 500, fail: !result.ok && (result.error || 'the handshake check failed') })
    }
    steps.push(
      { ...STEPS.applyConfig, ms: 500 },
      {
        ...STEPS.saveNode,
        ms: 450,
        then: () => {
          if (entryPort !== undefined) n.entryPort = entryPort
          if (handshakeTarget !== undefined) {
            n.handshakeTarget = handshakeTarget
            x.check = { ok: true, at: Date.now() }
          }
          if (sni !== undefined) x.sni = sni
        },
      },
    )
    return send(202, { jobId: run('change-settings', n, steps) }), true
  }

  if (rest === 'update') {
    const kind: unknown = body.kind
    if (kind !== 'agent' && kind !== 'xray') return fail(400, 'bad_request', 'kind is agent or xray'), true
    if (!pin[kind]) return fail(409, 'bad_request', `${kind === 'agent' ? 'the agent' : 'Xray'} is up to date`), true
    if (kind === 'agent') {
      const was = n.agentVersion
      return send(202, {
        jobId: run('update-agent', n, [
          { ...STEPS.downloadAgent, detail: `v${pin.agentVersion} · 18 MB`, ms: 500 },
          { ...STEPS.uploadAgent, detail: '18.0 MB sent', ms: 900, progress: { total: '18.0', unit: 'MB' } },
          {
            ...STEPS.restartAgent,
            ms: 600,
            then: () => {
              n.agentVersion = pin.agentVersion
              n.agentStart = Date.now()
              event(n, 'agent_updated', `${was} to ${pin.agentVersion}`)
            },
          },
          { ...STEPS.checkAgent, detail: `v${pin.agentVersion}, managed profile`, ms: 400 },
        ]),
      }), true
    }
    const was = n.xrayVersion
    return send(202, {
      jobId: run('update-xray', n, [
        { ...STEPS.downloadXray, detail: `${pin.xrayVersion} · 13.6 MB`, ms: 500 },
        {
          ...STEPS.uploadXray,
          detail: '13.6 MB sent',
          ms: 1200,
          progress: { total: '13.6', unit: 'MB' },
          then: () => {
            n.xrayVersion = pin.xrayVersion
            event(n, 'xray_changed', `${was} to ${pin.xrayVersion}`)
          },
        },
        { ...STEPS.checkXray, detail: `Xray ${pin.xrayVersion}`, ms: 500, then: () => event(n, 'tunnel_up') },
      ]),
    }), true
  }

  if (rest === 'renew-cert') {
    return send(202, {
      jobId: run('renew-cert', n, [
        { ...STEPS.renewCert, detail: `Valid until ${iso(Date.now() + 365 * DAY).slice(0, 10)}`, ms: 500 },
        {
          ...STEPS.saveNode,
          ms: 700,
          then: () => {
            x.certBase = Date.now()
            event(n, 'cert_renewed')
          },
        },
      ]),
    }), true
  }

  if (rest === 'rotate-keys') {
    return send(202, {
      jobId: run('rotate-keys', n, [
        {
          ...STEPS.createKeys,
          ms: 400,
          then: () => {
            x.keys = { publicKey: randomKey(), shortId: randomHex(8) }
            event(n, 'keys_rotated')
          },
        },
        { ...STEPS.applyConfig, ms: 500 },
        { ...STEPS.saveNode, ms: 450 },
      ]),
    }), true
  }

  // remove: the agent can't take itself off the server, so the hub wants a login to do it, or force to only
  // forget the node, whether the node can be reached or not
  const req = body as { force?: boolean; login?: { password?: string; privateKey?: string; hostKey?: string } }
  if (req.force && req.login) return fail(400, 'bad_request', 'Send a login or force the removal, not both.'), true
  if (!req.force && !req.login)
    return fail(409, 'needs_login', "The agent can't be taken off the server without an SSH login. Send one, or forget the node and leave the server as it is.", { needLogin: true }), true
  if (req.login) {
    if (!req.login.password && !req.login.privateKey) return fail(400, 'bad_request', 'give a password or a private key'), true
    if (!String(req.login.hostKey ?? '').startsWith('SHA256:')) return fail(400, 'bad_request', "confirm the node's host key first"), true
    await sleep(500)
    if (req.login.password === 'wrong') return fail(400, 'bad_request', 'ssh: unable to authenticate: the password or key was not accepted'), true
  }
  const gone: Step = {
    ...STEPS.forgetNode,
    ms: 350,
    then: () => {
      nodes.splice(nodes.indexOf(n), 1)
      for (const s of subs) s.on.delete(n.id)
      extras.delete(n.id)
    },
  }
  // the node is gone by the end, so the job carries its name
  const steps: Step[] = req.force ? [gone] : [{ ...STEPS.checkServer, ms: 300 }, { ...STEPS.stopAgent, ms: 300 }, { ...STEPS.removeAgent, ms: 500 }, gone]
  return send(202, { jobId: run('remove', n, steps, { target: n.name }) }), true
}
