import { describe, expect, it } from 'vitest'
import { translate } from '../i18n/translate'
import type { Lang } from '../i18n/lang'
import type { Node } from './api'
import { makeFormat } from './format'
import { health, unreachable } from './health'

const NOW = Date.parse('2026-10-30T12:00:00Z')
const ago = (min: number) => new Date(NOW - min * 60e3).toISOString()

const node = (o: Partial<Node> = {}): Node => ({
  id: 1,
  name: 'Node 1',
  host: '203.0.113.24',
  sshPort: 22,
  sshUser: 'root',
  registered: true,
  createdAt: ago(60 * 24 * 9),
  entryPort: 443,
  handshakeTarget: 'www.example.com:443',
  sni: 'www.example.com',
  sniCustom: false,
  publicKey: 'pk',
  shortId: 'ab',
  arch: 'amd64',
  agentVersion: '1.3.0',
  xrayVersion: '26.10.21',
  lastSeenAt: ago(0),
  subscriptionCount: 6,
  tunnel: { state: 'up', since: ago(60), rttMs: 20, streams: 0, carried: 0, bytesToSubs: 0, bytesFromSubs: 0, dialFailures: 0, reason: '' },
  agent: {
    version: '1.3.0',
    xrayVersion: '26.10.21',
    uptimeSec: 100,
    rssBytes: 0,
    reportedAt: ago(0),
    xray: { running: true, restarts: 0, uptimeSec: 100, lastExit: '', crashLoop: false },
    settingsApplied: true,
    lastPushError: '',
  },
  cert: { notAfter: ago(-1000), validDays: 300, renewsAt: ago(-900) },
  updates: { agent: false, xray: false, agentVersion: '1.3.0', xrayVersion: '26.10.21' },
  usage: { live: true, online: 3, connections: 9, rateUp: 0, rateDown: 0, todayUp: 0, todayDown: 0, monthUp: 0, monthDown: 0, updatedAt: ago(0) },
  ...o,
})

const down = (o: Partial<Node> = {}) =>
  node({ tunnel: { ...node().tunnel, state: 'off', reason: 'agent_down' }, lastSeenAt: ago(22), ...o })

const en = (n: Node) => health(n, (k, p) => translate('en', k, p), makeFormat('en'), NOW)
const fa = (n: Node) => health(n, (k, p) => translate('fa', k, p), makeFormat('fa'), NOW)

describe('a node\'s health', () => {
  it('is working when nothing is wrong', () => {
    expect(en(node())).toEqual({ tone: 'online', label: 'Working', reason: '' })
  })

  it('does not hold the entry being off against a node that works', () => {
    const off = node({ tunnel: { ...node().tunnel, state: 'off', reason: 'entry_off' } })
    expect(en(off).label).toBe('Working')
  })

  it('says a node is installing, whatever else is true of it', () => {
    expect(en(node({ installing: { jobId: 'j', step: 5, steps: 15, key: 'upload_xray', label: 'x', percent: 62 }, registered: false }))).toEqual({ tone: 'idle', label: 'Installing', reason: '' })
  })

  it('calls a node whose install never finished not finished', () => {
    expect(en(node({ registered: false }))).toEqual({ tone: 'danger', label: 'Not finished', reason: 'The install never finished' })
  })

  it('says when an unreachable node was last seen, and how long ago', () => {
    expect(unreachable(down())).toBe(true)
    expect(unreachable(node())).toBe(false)
    const h = en(down())
    expect(h).toMatchObject({ tone: 'danger', label: 'Unreachable' })
    expect(h.reason).toMatch(/^Not reachable since (\d\d:\d\d|\d+ \w+) \(22 minutes ago\)$/)
    expect(en(down({ lastSeenAt: null })).reason).toBe('Not reachable')
  })

  it('says it in Persian, with Persian digits', () => {
    const h = fa(down())
    expect(h).toMatchObject({ tone: 'danger', label: 'در دسترس نیست' })
    expect(h.reason.replace(/[\u200e\u200f]/g, '')).toMatch(/^از (ساعت [۰-۹]{2}:[۰-۹]{2}|[۰-۹]+ \S+) در دسترس نیست \(۲۲ دقیقه پیش\)$/)
    expect(fa(node()).label).toBe('در حال کار')
  })

  it('says "since a date" without the word for an hour when the node was last seen on another day', () => {
    const old = down({ lastSeenAt: ago(3 * 24 * 60) })
    expect(en(old).reason).toMatch(/^Not reachable since \d+ \w+ \(/)
    const reason = fa(old).reason.replace(/[\u200e\u200f]/g, '')
    expect(reason).toMatch(/^از [۰-۹]+ \S+ در دسترس نیست \(/)
    expect(reason.startsWith('از ساعت')).toBe(false)
  })

  it('asks for attention when the tunnel is off because Xray is not the pinned version', () => {
    const h = en(node({ tunnel: { ...node().tunnel, state: 'off', reason: 'xray_mismatch' } }))
    expect(h).toEqual({ tone: 'warn', label: 'Needs attention', reason: 'The tunnel is off until Xray is updated' })
  })

  it('names what is wrong with Xray, the settings and the tunnel, worst first', () => {
    const a = node().agent!
    expect(en(node({ agent: { ...a, xray: { ...a.xray, crashLoop: true, running: false } } }))).toMatchObject({ tone: 'danger', reason: 'Xray keeps crashing' })
    expect(en(node({ agent: { ...a, xray: { ...a.xray, running: false } } }))).toMatchObject({ tone: 'warn', reason: 'Xray is not running' })
    expect(en(node({ agent: { ...a, settingsApplied: false, lastPushError: 'xray -test failed' } }))).toMatchObject({ tone: 'warn', reason: "Settings aren't applied yet" })
    // not applied yet, but no error: still on its way
    expect(en(node({ agent: { ...a, settingsApplied: false } })).label).toBe('Working')
    expect(en(node({ tunnel: { ...node().tunnel, state: 'down' } }))).toMatchObject({ tone: 'warn', reason: 'The tunnel is down' })
  })

  it('waits for the first report of a new agent', () => {
    expect(en(node({ agent: null }))).toEqual({ tone: 'idle', label: 'Starting', reason: "Waiting for the agent's first report" })
  })

  it('ranks being unreachable above a version mismatch', () => {
    expect(en(down({ tunnel: { ...node().tunnel, state: 'off', reason: 'agent_down' } })).label).toBe('Unreachable')
  })

  it.each<Lang>(['en', 'fa'])('gives every state a label in %s', (lang) => {
    const t = (k: Parameters<typeof translate>[1], p?: Record<string, string | number>) => translate(lang, k, p)
    const fmt = makeFormat(lang)
    for (const n of [node(), down(), node({ registered: false }), node({ agent: null })]) {
      const h = health(n, t, fmt, NOW)
      expect(h.label).not.toBe('')
    }
  })
})
