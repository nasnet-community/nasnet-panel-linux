import { describe, expect, it } from 'vitest'
import type { Lang } from '../i18n/lang'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import type { Entry, Local, LocalReason, LocalState } from './api'
import { makeFormat } from './format'
import { hasLinks, routerDown, routerHealth, routerNotes, routerVerdict } from './router'

const speak = (lang: Lang) => ({ t: (key: MsgKey, params?: Params) => translate(lang, key, params), fmt: makeFormat(lang) })
const { t, fmt } = speak('en')
const NOW = Date.parse('2026-10-30T12:00:00Z')

const local = (o: Partial<Local> = {}): Local => ({
  enabled: true,
  state: 'working',
  reason: '',
  detail: '',
  host: 'a1b2c3d4e5f6.sn.mynetname.net',
  hostSource: 'cloud',
  ownHost: '',
  cloudName: 'a1b2c3d4e5f6.sn.mynetname.net',
  publicAddr: '198.51.100.24',
  domesticAddr: '198.51.100.24',
  behindNat: false,
  port: 41873,
  handshakeTarget: 'www.example.com:443',
  sni: '',
  sniCustom: false,
  publicKey: 'pk',
  shortId: 'ab',
  subscriptionCount: 5,
  usage: { live: true, online: 2, connections: 3, rateUp: 0, rateDown: 0, todayUp: 0, todayDown: 0, monthUp: 0, monthDown: 0, updatedAt: new Date(NOW - 4000).toISOString() },
  xrayVersion: '26.10.21',
  uptimeSec: 3600,
  ...o,
})

const sick = (reason: LocalReason, o: Partial<Local> = {}) => local({ state: 'attention', reason, ...o })

const entry = (o: Partial<Entry> = {}): Entry => ({
  on: true,
  domestic: 'online',
  vpn: { state: 'online', kind: 'wireguard', label: 'WireGuard' },
  bridge: 'running',
  routerOS: { version: '7.24.5', min: '7.20', ok: true },
  routes: [],
  checkedAt: null,
  stale: false,
  ...o,
})

describe('routerHealth', () => {
  it('is off, starting or working in a word, with nothing more to say when all is well', () => {
    expect(routerHealth(local({ enabled: false, state: 'off' }), entry(), t)).toEqual({ tone: 'idle', label: 'Off', reason: '' })
    expect(routerHealth(local(), entry(), t)).toEqual({ tone: 'online', label: 'Working', reason: '' })
  })

  it('says what a start is waiting for', () => {
    const start = (reason: LocalReason) => routerHealth(local({ state: 'starting', reason, port: null }), entry(), t)
    expect(start('waiting_router')).toEqual({ tone: 'idle', label: 'Starting', reason: 'Waiting for the router to open the port. This takes up to a minute.' })
    expect(start('choosing_target').reason).toBe('Choosing a Reality target.')
    expect(start('').reason).toBe("Starting the router's Xray.")
  })

  it('still works behind another router, and says which port to forward where', () => {
    const h = routerHealth(local({ behindNat: true, domesticAddr: '192.168.1.20' }), entry(), t)
    expect(h.tone).toBe('online')
    expect(h.reason).toBe("Forward port 41873 on the router in front to 192.168.1.20, or links can't reach this router.")
  })

  it('is paused while entry is off, and names the half that is down', () => {
    const paused = (e: Entry | null) => routerHealth(sick('entry_off'), e, t)
    expect(paused(entry({ on: false, vpn: { state: 'offline', kind: 'wireguard', label: 'WireGuard' } }))).toEqual({
      tone: 'warn',
      label: 'Paused',
      reason: 'The VPN is down. Links stop until it is back, so nothing leaves another way.',
    })
    expect(paused(entry({ on: false, domestic: 'offline' })).reason).toBe('The Domestic link is down. Links stop until it is back.')
    // the VPN is what its traffic leaves through, so it is named first when both are down
    expect(paused(entry({ on: false, domestic: 'missing', vpn: { state: 'missing', kind: '', label: '' } })).reason).toMatch(/^The VPN is down/)
    expect(paused(null).reason).toBe('Entry is off. Links stop until it is back on.')
  })

  it('needs attention for the rest, in red only when no port was opened', () => {
    expect(routerHealth(sick('no_port', { port: null }), entry(), t)).toEqual({ tone: 'danger', label: 'Needs attention', reason: 'The router has not opened the port' })
    expect(routerHealth(sick('no_address', { host: '' }), entry(), t)).toEqual({ tone: 'warn', label: 'Needs attention', reason: 'There is no name to put in links' })
    expect(routerHealth(sick('target_failed', { handshakeTarget: '' }), entry(), t).reason).toBe('The router found no Reality target that works')
    expect(routerHealth(sick('bridge_down'), entry(), t).reason).toBe("The router's Xray is not running")
  })
})

describe('routerNotes', () => {
  it('has nothing to say while it is off, or works with nothing in front of it', () => {
    expect(routerNotes(local({ enabled: false, state: 'off' }), entry(), t)).toEqual([])
    expect(routerNotes(local(), entry(), t)).toEqual([])
  })

  it('offers the one thing to do, where there is one', () => {
    const acts = (l: Local) => routerNotes(l, entry(), t).map((n) => [n.id, n.tone, n.acts.map((a) => a.id)])
    expect(acts(local({ state: 'starting', reason: 'waiting_router' }))).toEqual([['starting', 'info', []]])
    expect(acts(local({ behindNat: true }))).toEqual([['behindNat', 'warn', []]])
    expect(acts(sick('entry_off'))).toEqual([['entryOff', 'warn', ['openSettings']]])
    expect(acts(sick('no_port'))).toEqual([['noPort', 'danger', []]])
    expect(acts(sick('no_address'))).toEqual([['noAddress', 'warn', ['setName']]])
    expect(acts(sick('target_failed'))).toEqual([['targetFailed', 'warn', ['setTarget']]])
    expect(acts(sick('bridge_down'))).toEqual([['bridgeDown', 'warn', []]])
  })

  it("puts the hub's reason for a failed target first, as a sentence", () => {
    const [note] = routerNotes(sick('target_failed', { detail: 'none of the 6 sites tried works' }), entry(), t)
    expect(note.detail).toBe('None of the 6 sites tried works. It tries again in a few minutes. You can set one yourself.')
    expect(routerNotes(sick('target_failed'), entry(), t)[0].detail).toBe('It tries again in a few minutes. You can set one yourself.')
  })

  it('has every note in Persian too', () => {
    const fa = speak('fa')
    const states: [LocalState, LocalReason][] = [['starting', ''], ['starting', 'choosing_target'], ['starting', 'waiting_router'], ['attention', 'entry_off'], ['attention', 'no_port'], ['attention', 'no_address'], ['attention', 'target_failed'], ['attention', 'bridge_down']]
    for (const [state, reason] of states) {
      const [note] = routerNotes(local({ state, reason }), entry(), fa.t)
      // Xray and IP Cloud are names, and stay as they are
      expect(`${note.title} ${note.detail}`.replace(/Xray|IP Cloud/g, ''), `${state}/${reason}`).not.toMatch(/[A-Za-z]{3,}/)
    }
  })
})

describe('routerVerdict', () => {
  const chips = (l: Local, e: Entry | null = entry()) => Object.fromEntries(routerVerdict(l, e, t, fmt, NOW).chips.map((c) => [c.key, `${c.tone}:${c.value}`]))

  it('says everything works, and when that was read', () => {
    const v = routerVerdict(local(), entry(), t, fmt, NOW)
    expect(v.title).toBe('Everything is working')
    expect(v.note).toBe('Last report 4 s ago')
    expect(chips(local())).toEqual({ port: 'online:open', target: 'online:set', xray: 'online:running' })
  })

  it('counts the router in front as a warning', () => {
    expect(routerVerdict(local({ behindNat: true }), entry(), t, fmt, NOW).title).toBe('Working, with 1 warning')
  })

  it('has no report time before the first report', () => {
    const quiet = local({ state: 'starting', reason: 'waiting_router', port: null, usage: { ...local().usage, live: false, updatedAt: null } })
    const v = routerVerdict(quiet, entry({ bridge: 'waiting' }), t, fmt, NOW)
    expect(v.title).toBe('This router is starting')
    expect(v.note).toBe('')
    expect(chips(quiet, entry({ bridge: 'waiting' }))).toEqual({ port: 'idle:waiting', target: 'online:set', xray: 'idle:waiting' })
  })

  it('shows which part is in the way', () => {
    expect(routerVerdict(sick('entry_off'), entry({ on: false }), t, fmt, NOW).title).toBe('This router is paused')
    expect(chips(sick('entry_off'), entry({ on: false, bridge: 'stopped' }))).toMatchObject({ port: 'idle:open', xray: 'idle:stopped' })
    expect(routerVerdict(sick('no_port', { port: null }), entry(), t, fmt, NOW).title).toBe("Subscriptions can't connect right now")
    expect(chips(sick('no_port', { port: null })).port).toBe('danger:not open')
    expect(chips(sick('target_failed', { handshakeTarget: '' })).target).toBe('warn:none works')
    expect(chips(local({ state: 'starting', reason: 'choosing_target', handshakeTarget: '' })).target).toBe('idle:choosing')
    expect(chips(sick('bridge_down')).xray).toBe('warn:stopped')
    // nothing is known of the router's Xray before the config has loaded
    expect(chips(sick('no_address', { host: '' }), null).xray).toBe('idle:–')
  })
})

describe('what a subscription can count on', () => {
  it('has links once it is on with a name and a port', () => {
    expect(hasLinks(local())).toBe(true)
    expect(hasLinks(local({ enabled: false }))).toBe(false)
    expect(hasLinks(local({ host: '' }))).toBe(false)
    expect(hasLinks(local({ port: null }))).toBe(false)
  })

  it('takes a switch later unless it is working', () => {
    expect(routerDown(local())).toBe(false)
    for (const state of ['starting', 'attention'] as const) expect(routerDown(local({ state }))).toBe(true)
  })
})
