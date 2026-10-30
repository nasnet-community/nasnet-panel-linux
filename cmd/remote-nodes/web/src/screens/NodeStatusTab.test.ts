import { describe, expect, it } from 'vitest'
import type { Lang } from '../i18n/lang'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import type { Entry, Node } from '../lib/api'
import { makeFormat } from '../lib/format'
import { nodeVerdict } from './NodeStatusTab'

const NOW = Date.parse('2026-10-30T12:00:00Z')
const ago = (sec: number) => new Date(NOW - sec * 1000).toISOString()

const node = (o: Partial<Node> = {}): Node => ({
  id: 1,
  name: 'Node 1',
  host: '203.0.113.24',
  sshPort: 22,
  sshUser: 'root',
  registered: true,
  createdAt: ago(9 * 86400),
  entryPort: 443,
  handshakeTarget: 'www.example.com:443',
  sni: 'www.example.com',
  sniCustom: false,
  publicKey: 'pk',
  shortId: 'ab',
  arch: 'amd64',
  agentVersion: '1.3.0',
  xrayVersion: '26.10.21',
  lastSeenAt: ago(2),
  subscriptionCount: 6,
  tunnel: { state: 'up', since: ago(6 * 86400 + 4 * 3600), rttMs: 23, streams: 9, carried: 0, bytesToSubs: 0, bytesFromSubs: 0, dialFailures: 0, reason: '' },
  agent: {
    version: '1.3.0',
    xrayVersion: '26.10.21',
    uptimeSec: 100,
    rssBytes: 0,
    reportedAt: ago(2),
    xray: { running: true, restarts: 0, uptimeSec: 100, lastExit: '', crashLoop: false },
    settingsApplied: true,
    lastPushError: '',
  },
  cert: { notAfter: ago(-300 * 86400), validDays: 300, renewsAt: ago(-270 * 86400) },
  updates: { agent: false, xray: false, agentVersion: '1.3.0', xrayVersion: '26.10.21' },
  usage: { live: true, online: 3, connections: 9, rateUp: 0, rateDown: 0, todayUp: 0, todayDown: 0, monthUp: 0, monthDown: 0, updatedAt: ago(2) },
  ...o,
})

const entry = (o: Partial<Entry> = {}): Entry => ({
  on: true,
  domestic: 'online',
  vpn: { state: 'online', kind: 'wireguard', label: 'WireGuard' },
  bridge: 'running',
  routerOS: { version: '7.24.5', min: '7.11', ok: true },
  routes: [],
  checkedAt: ago(120),
  stale: false,
  ...o,
})

const tunnel = (o: Partial<Node['tunnel']>): Node['tunnel'] => ({ ...node().tunnel, ...o })
const verdict = (n: Node, e: Entry | null = entry(), lang: Lang = 'en') =>
  nodeVerdict(n, e, (k: MsgKey, p?: Params) => translate(lang, k, p), makeFormat(lang), NOW)
const chips = (v: ReturnType<typeof verdict>) => v.chips.map((c) => [c.label, c.tone, c.value])
const plain = (s: string) => s.replace(/[‎‏‪-‮⁦-⁩]/g, '')

// the three states of the board, then what else a node can be in
describe('the status verdict of a node', () => {
  it('says all is well, and what that rests on', () => {
    const v = verdict(node())
    expect(v.title).toBe('Everything is working')
    expect(v.note).toBe('Last report 2 s ago')
    expect(chips(v)).toEqual([
      ['Portal', 'online', 'listening'],
      ['Tunnel', 'online', 'up 6d 4h'],
      ['Bridge', 'online', 'running'],
      ['VPN', 'online', 'WireGuard'],
    ])
  })

  it("says subscriptions can't connect when the tunnel is off for want of the right Xray, with the bridge waiting", () => {
    const v = verdict(node({ xrayVersion: '26.10.18', tunnel: tunnel({ state: 'off', since: null, reason: 'xray_mismatch' }) }))
    expect(v.title).toBe("Subscriptions can't connect right now")
    expect(chips(v)).toEqual([
      ['Portal', 'online', 'listening'],
      ['Tunnel', 'warn', 'off'],
      ['Bridge', 'idle', 'waiting'],
      ['VPN', 'online', 'WireGuard'],
    ])
  })

  it('says the router can\'t reach a node, and shows dashes where the numbers are old', () => {
    const v = verdict(node({ tunnel: tunnel({ state: 'off', reason: 'agent_down' }), agent: { ...node().agent!, reportedAt: ago(22 * 60) } }))
    expect(v.title).toBe("The router can't reach this node")
    expect(v.note).toBe("Last report 22 minutes ago. The router can't reach this node, so these numbers are old.")
    expect(chips(v)).toEqual([
      ['Portal', 'idle', '–'],
      ['Tunnel', 'idle', '–'],
      ['Bridge', 'idle', '–'],
      ['VPN', 'idle', '–'],
    ])
  })

  it("waits for a node's first report, with nothing to say yet", () => {
    const v = verdict(node({ agent: null }))
    expect(v.title).toBe("Waiting for the agent's first report")
    expect(v.note).toBe('')
    expect(v.chips.every((c) => c.tone === 'idle' && c.value === '–')).toBe(true)
  })

  it('says the tunnel waits for entry, with the router\'s own bridge and VPN', () => {
    const e = entry({ on: false, domestic: 'missing', vpn: { state: 'missing', kind: '', label: '' }, bridge: 'stopped' })
    const v = verdict(node({ tunnel: tunnel({ state: 'off', since: null, reason: 'entry_off' }) }), e)
    expect(v.title).toBe('The tunnel is waiting for entry to turn on')
    expect(chips(v)).toEqual([
      ['Portal', 'online', 'listening'],
      ['Tunnel', 'idle', 'off'],
      ['Bridge', 'idle', 'stopped'],
      ['VPN', 'warn', 'missing'],
    ])
  })

  it('shows Xray that is not running as a portal that is not, in the tone of why', () => {
    const stopped = node({ agent: { ...node().agent!, xray: { running: false, restarts: 0, uptimeSec: 0, lastExit: '', crashLoop: false } }, tunnel: tunnel({ state: 'down' }) })
    expect(verdict(stopped).title).toBe("Subscriptions can't connect right now")
    expect(chips(verdict(stopped))[0]).toEqual(['Portal', 'warn', 'not running'])
    expect(chips(verdict(stopped))[1]).toEqual(['Tunnel', 'danger', 'down'])
    const crashing = node({ agent: { ...node().agent!, xray: { running: false, restarts: 9, uptimeSec: 0, lastExit: 'x', crashLoop: true } }, tunnel: tunnel({ state: 'down' }) })
    expect(chips(verdict(crashing))[0]).toEqual(['Portal', 'danger', 'not running'])
  })

  it("says subscriptions can't connect while the router's bridge is down", () => {
    const v = verdict(node({ tunnel: tunnel({ state: 'off', since: null, reason: 'bridge_down' }) }))
    expect(v.title).toBe("Subscriptions can't connect right now")
    expect(chips(v)[1]).toEqual(['Tunnel', 'warn', 'off'])
    expect(chips(v)[2]).toEqual(['Bridge', 'warn', 'stopped'])
  })

  it("follows the bridge by the node's tunnel first, and by the router's when the tunnel has no word on it", () => {
    // tunnel down but the router's bridge is up: it is dialling
    expect(chips(verdict(node({ tunnel: tunnel({ state: 'down' }) })))[2]).toEqual(['Bridge', 'online', 'running'])
    expect(chips(verdict(node({ tunnel: tunnel({ state: 'down' }) }), entry({ bridge: 'waiting' })))[2]).toEqual(['Bridge', 'idle', 'waiting'])
    // a bridge that is down says so
    expect(chips(verdict(node({ tunnel: tunnel({ state: 'off', reason: 'bridge_down' }) })))[2]).toEqual(['Bridge', 'warn', 'stopped'])
    // before the config has loaded there is nothing to read the router's from
    const bare = chips(verdict(node({ tunnel: tunnel({ state: 'down' }) }), null))
    expect(bare[2]).toEqual(['Bridge', 'idle', '–'])
    expect(bare[3]).toEqual(['VPN', 'idle', '–'])
  })

  it('counts warnings in a node that works, in the singular and the plural', () => {
    const notApplied = node({ agent: { ...node().agent!, settingsApplied: false, lastPushError: 'bad' } })
    expect(verdict(notApplied).title).toBe('Working, with 1 warning')
    const both = node({ agent: { ...node().agent!, settingsApplied: false, lastPushError: 'bad' }, cert: { ...node().cert, validDays: 5 } })
    expect(verdict(both).title).toBe('Working, with 2 warnings')
  })

  it('reads in Persian, in Persian digits', () => {
    const v = verdict(node(), entry(), 'fa')
    expect(v.title).toBe('همه‌چیز کار می‌کند')
    expect(plain(v.note)).toBe('آخرین گزارش ۲ ثانیه پیش')
    expect(chips(v).map((c) => plain(String(c[2])))).toEqual(['در حال گوش دادن', '۶ روز و ۴ ساعت برقرار', 'در حال اجرا', 'وایرگارد'])
    expect(verdict(node({ agent: { ...node().agent!, settingsApplied: false, lastPushError: 'bad' }, cert: { ...node().cert, validDays: 5 } }), entry(), 'fa').title).toBe('کار می‌کند، با ۲ هشدار')
    const down = verdict(node({ tunnel: tunnel({ state: 'off', reason: 'agent_down' }), agent: { ...node().agent!, reportedAt: ago(22 * 60) } }), entry(), 'fa')
    expect(plain(down.note)).toBe('آخرین گزارش ۲۲ دقیقه پیش. روتر به این نود نمی‌رسد، پس این عددها قدیمی‌اند.')
  })
})
