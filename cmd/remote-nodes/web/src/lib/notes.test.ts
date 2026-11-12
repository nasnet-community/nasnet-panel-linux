import { describe, expect, it } from 'vitest'
import { en as enDict } from '../i18n/en'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import type { Lang } from '../i18n/lang'
import type { Node } from './api'
import { makeFormat } from './format'
import { health, issues, nodeNotes, type NodeAct, type NodeNote } from './health'

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
  cert: { notAfter: ago(-60 * 24 * 300), validDays: 300, renewsAt: ago(-60 * 24 * 270) },
  updates: { agent: false, xray: false, agentVersion: '1.3.0', xrayVersion: '26.10.21' },
  usage: { live: true, online: 3, connections: 9, rateUp: 0, rateDown: 0, todayUp: 0, todayDown: 0, monthUp: 0, monthDown: 0, updatedAt: ago(0) },
  ...o,
})

const tunnel = (o: Partial<Node['tunnel']>): Node['tunnel'] => ({ ...node().tunnel, ...o })
const agent = (o: Partial<NonNullable<Node['agent']>>): Node['agent'] => ({ ...node().agent!, ...o })
const crashing = (exit = '') => agent({ xray: { running: false, restarts: 14, uptimeSec: 0, lastExit: exit, crashLoop: true } })

const words = (lang: Lang) => (key: MsgKey, params?: Params) => translate(lang, key, params)
const notes = (n: Node, lang: Lang = 'en', xray?: string) => nodeNotes(n, { t: words(lang), fmt: makeFormat(lang), xray, now: NOW })
const badge = (n: Node, lang: Lang = 'en') => health(n, words(lang), makeFormat(lang), NOW)
const plain = (s: string) => s.replace(/[‎‏‪-‮⁦-⁩]/g, '')

describe("what is wrong with a node", () => {
  it('is nothing for a node that works, and nothing yet for one being installed', () => {
    expect(issues(node())).toEqual([])
    expect(issues(node({ installing: { jobId: 'j', step: 5, steps: 15, key: 'upload_xray', label: '', percent: 62 }, registered: false, agent: null }))).toEqual([])
  })

  it('says an unfinished install is the only thing that matters about it', () => {
    expect(issues(node({ registered: false, agent: null, tunnel: tunnel({ reason: 'agent_down' }) }))).toEqual(['notFinished'])
  })

  it("says the router can't reach it, and nothing else, since what the agent last said is old", () => {
    expect(issues(node({ tunnel: tunnel({ state: 'off', reason: 'agent_down' }), agent: crashing() }))).toEqual(['unreachable'])
  })

  it('names each problem of a node that answers', () => {
    expect(issues(node({ tunnel: tunnel({ state: 'off', reason: 'xray_mismatch' }) }))).toEqual(['xrayMismatch'])
    expect(issues(node({ tunnel: tunnel({ state: 'off', reason: 'bridge_down' }) }))).toEqual(['bridgeDown'])
    expect(issues(node({ agent: crashing('exit status 1') }))).toEqual(['xrayCrash'])
    expect(issues(node({ agent: agent({ xray: { running: false, restarts: 0, uptimeSec: 0, lastExit: '', crashLoop: false } }) }))).toEqual(['xrayStopped'])
    expect(issues(node({ agent: agent({ settingsApplied: false, lastPushError: 'xray -test failed' }) }))).toEqual(['notApplied'])
    // not applied yet but no error is a push on its way, not a problem
    expect(issues(node({ agent: agent({ settingsApplied: false }) }))).toEqual([])
    expect(issues(node({ tunnel: tunnel({ state: 'down' }) }))).toEqual(['tunnelDown'])
    expect(issues(node({ agent: null }))).toEqual(['waiting'])
    expect(issues(node({ tunnel: tunnel({ state: 'off', reason: 'entry_off' }) }))).toEqual(['entryOff'])
  })

  it('has a certificate that should have been renewed at 14 days or fewer', () => {
    const left = (validDays: number) => node({ cert: { ...node().cert, validDays } })
    expect(issues(left(15))).toEqual([])
    expect(issues(left(14))).toEqual(['certExpiring'])
    expect(issues(left(0))).toEqual(['certExpiring'])
  })

  it('lists several problems worst first', () => {
    const n = node({
      tunnel: tunnel({ state: 'off', reason: 'xray_mismatch' }),
      agent: agent({ settingsApplied: false, lastPushError: 'x' }),
      cert: { ...node().cert, validDays: 9 },
    })
    expect(issues(n)).toEqual(['xrayMismatch', 'notApplied', 'certExpiring'])
  })
})

describe('the badge and the notes', () => {
  it('give the card the worst problem, and never blame the node for entry being off', () => {
    const off = node({ tunnel: tunnel({ state: 'off', reason: 'entry_off' }) })
    expect(badge(off).label).toBe('Working')
    expect(notes(off).map((x) => x.id)).toEqual(['entryOff'])
  })

  it('call a certificate that is about to end a reason for attention, and one that has ended a danger', () => {
    const left = (validDays: number) => node({ cert: { ...node().cert, validDays } })
    expect(badge(left(9))).toEqual({ tone: 'warn', label: 'Needs attention', reason: 'The certificate expires in 9 days' })
    expect(badge(left(1)).reason).toBe('The certificate expires in 1 day')
    expect(badge(left(1)).tone).toBe('danger')
    expect(badge(left(0))).toEqual({ tone: 'danger', label: 'Needs attention', reason: 'The certificate has expired' })
    expect(plain(badge(left(9), 'fa').reason)).toBe('۹ روز تا پایان گواهی مانده است')
  })

  it('agree about which nodes have something wrong', () => {
    const states = [
      node(),
      node({ registered: false, agent: null }),
      node({ tunnel: tunnel({ state: 'off', reason: 'agent_down' }) }),
      node({ tunnel: tunnel({ state: 'off', reason: 'xray_mismatch' }) }),
      node({ tunnel: tunnel({ state: 'off', reason: 'bridge_down' }) }),
      node({ agent: crashing() }),
      node({ tunnel: tunnel({ state: 'down' }) }),
      node({ agent: null }),
      node({ cert: { ...node().cert, validDays: 3 } }),
      node({ tunnel: tunnel({ state: 'off', reason: 'entry_off' }) }),
    ]
    for (const n of states) {
      const fine = badge(n).reason === '' && badge(n).tone === 'online'
      // the badge is "Working" exactly when the list holds nothing but what never changes it
      expect(fine, JSON.stringify([n.tunnel.reason, n.tunnel.state, n.registered])).toBe(issues(n).every((i) => i === 'entryOff'))
    }
  })
})

describe('the notes on the node page', () => {
  const only = (n: Node, lang: Lang = 'en', xray?: string): NodeNote => {
    const list = notes(n, lang, xray)
    expect(list).toHaveLength(1)
    return list[0]
  }

  it('say nothing about a node that works', () => {
    expect(notes(node())).toEqual([])
    // an update being due is the chip's business, not a note
    expect(notes(node({ updates: { agent: true, xray: true, agentVersion: '1.3.1', xrayVersion: '26.10.22' } }))).toEqual([])
  })

  it('say a node is being installed and offer its job', () => {
    const n = only(node({ installing: { jobId: 'j', step: 5, steps: 15, key: 'upload_xray', label: 'Send Xray', percent: 62 }, registered: false, agent: null }))
    expect(n).toMatchObject({ id: 'installing', tone: 'info', title: 'This node is being installed', acts: [{ id: 'showJob' }] })
  })

  it('say an install that never finished can only be removed', () => {
    expect(only(node({ registered: false, agent: null }))).toEqual({
      id: 'notFinished',
      tone: 'danger',
      title: "This node's install never finished",
      detail: "It can't be repaired. Remove it, then add it again.",
      acts: [{ id: 'remove' }],
    })
  })

  it("say when an unreachable node was last seen, and offer to check again", () => {
    const down = node({ tunnel: tunnel({ state: 'off', reason: 'agent_down' }), lastSeenAt: ago(22) })
    const n = only(down)
    expect(n.tone).toBe('danger')
    expect(n.title).toBe("The router can't reach this node")
    expect(n.detail).toMatch(/^Last seen at (\d\d:\d\d|\d+ \w+), 22 minutes ago\. Check that the server is on and its address is on the domestic list, then check again\.$/)
    expect(n.acts).toEqual([{ id: 'checkAgain', primary: true }])
    // a node never seen has no time to give
    expect(only(node({ tunnel: tunnel({ state: 'off', reason: 'agent_down' }), lastSeenAt: null })).detail).toBe(
      'Check that the server is on and its address is on the domestic list, then check again.',
    )
  })

  it('say a tunnel is off because Xray is not the version the router uses, and offer the update', () => {
    const old = node({ xrayVersion: '26.10.18', tunnel: tunnel({ state: 'off', reason: 'xray_mismatch' }) })
    expect(only(old, 'en', '26.10.21')).toEqual({
      id: 'xrayMismatch',
      tone: 'warn',
      title: 'The tunnel is off',
      detail: 'This node runs Xray 26.10.18. Its tunnel stays off until it matches 26.10.21, the version this router uses.',
      acts: [{ id: 'updateXray', primary: true }],
    })
    // the router's version is known from the node's own update info when the config hasn't loaded
    expect(only(old).detail).toContain('matches 26.10.21,')
    // a node ahead of the router is told so, and gets the same way out
    const ahead = node({ xrayVersion: '26.11.2', tunnel: tunnel({ state: 'off', reason: 'xray_mismatch' }) })
    expect(only(ahead, 'en', '26.10.21').detail).toBe('This node runs Xray 26.11.2, newer than the 26.10.21 this router uses. Its tunnel stays off until they match.')
    expect(only(ahead, 'en', '26.10.21').acts).toEqual([{ id: 'updateXray', primary: true }])
  })

  it('say why Xray is not running, with its last exit when it has one', () => {
    expect(only(node({ agent: crashing('exit status 1') }))).toMatchObject({
      tone: 'danger',
      title: 'Xray keeps crashing',
      detail: 'It last stopped with "exit status 1". It restarts on its own and waits longer each time.',
    })
    expect(only(node({ agent: crashing() })).detail).toBe('It restarts on its own and waits longer each time.')
    const stopped = (lastExit: string) => node({ agent: agent({ xray: { running: false, restarts: 0, uptimeSec: 0, lastExit, crashLoop: false } }) })
    expect(only(stopped('signal: killed'))).toMatchObject({ tone: 'warn', title: 'Xray is not running', detail: 'It last stopped with "signal: killed".' })
    expect(only(stopped('')).detail).toBe("The agent reports that Xray isn't running, so subscriptions can't connect.")
  })

  it("say what the agent could not apply, as a sentence of its own", () => {
    const bad = (lastPushError: string) => node({ agent: agent({ settingsApplied: false, lastPushError }) })
    expect(only(bad('xray -test failed: port in use'))).toMatchObject({ tone: 'warn', title: "Settings aren't applied yet", detail: 'Xray -test failed: port in use.' })
    expect(only(bad('already ends in a stop.')).detail).toBe('Already ends in a stop.')
  })

  it("say the router's Xray is not running, and that it comes back by itself", () => {
    const n = only(node({ tunnel: tunnel({ state: 'off', reason: 'bridge_down' }) }))
    expect(n).toEqual({
      id: 'bridgeDown',
      tone: 'warn',
      title: "The router's Xray is not running",
      detail: "The router's Xray carries the tunnel to this node. The plugin starts it again by itself.",
      acts: [],
    })
    expect(badge(node({ tunnel: tunnel({ state: 'off', reason: 'bridge_down' }) }))).toEqual({ tone: 'warn', label: 'Needs attention', reason: "The router's Xray is not running" })
  })

  it('say the tunnel is down, or that the first report is awaited', () => {
    expect(only(node({ tunnel: tunnel({ state: 'down' }) }))).toMatchObject({ tone: 'warn', title: 'The tunnel is down', detail: "The router can't open the tunnel to this node. It keeps trying." })
    expect(only(node({ agent: null }))).toMatchObject({ tone: 'info', title: "Waiting for the agent's first report" })
  })

  it('offer to renew a certificate that should have been renewed already', () => {
    const n = only(node({ cert: { ...node().cert, validDays: 9 } }))
    expect(n).toMatchObject({ tone: 'warn', title: 'The certificate expires in 9 days', acts: [{ id: 'renewCert', primary: true }] })
    expect(only(node({ cert: { ...node().cert, validDays: 2 } })).tone).toBe('danger')
  })

  it('say entry is off, and where to look', () => {
    expect(only(node({ tunnel: tunnel({ state: 'off', reason: 'entry_off' }) }))).toEqual({
      id: 'entryOff',
      tone: 'info',
      title: 'Entry is off',
      detail: 'This node works, but its tunnel starts only when entry is on. Settings shows what is missing.',
      acts: [{ id: 'openSettings' }],
    })
  })

  it('come worst first', () => {
    const n = node({
      tunnel: tunnel({ state: 'off', reason: 'xray_mismatch' }),
      agent: agent({ settingsApplied: false, lastPushError: 'x' }),
      cert: { ...node().cert, validDays: 9 },
    })
    expect(notes(n).map((x) => x.id)).toEqual(['xrayMismatch', 'notApplied', 'certExpiring'])
  })

  it('read in Persian, with the digits of its language, for every state', () => {
    const states = [
      node({ installing: { jobId: 'j', step: 1, steps: 15, key: 'upload_xray', label: '', percent: null }, registered: false, agent: null }),
      node({ registered: false, agent: null }),
      node({ tunnel: tunnel({ state: 'off', reason: 'agent_down' }), lastSeenAt: ago(22) }),
      node({ xrayVersion: '26.10.18', tunnel: tunnel({ state: 'off', reason: 'xray_mismatch' }) }),
      node({ xrayVersion: '26.11.2', tunnel: tunnel({ state: 'off', reason: 'xray_mismatch' }) }),
      node({ tunnel: tunnel({ state: 'off', reason: 'bridge_down' }) }),
      node({ agent: crashing('exit status 1') }),
      node({ agent: agent({ xray: { running: false, restarts: 0, uptimeSec: 0, lastExit: '', crashLoop: false } }) }),
      node({ agent: agent({ settingsApplied: false, lastPushError: 'bad' }) }),
      node({ tunnel: tunnel({ state: 'down' }) }),
      node({ agent: null }),
      node({ cert: { ...node().cert, validDays: 9 } }),
      node({ tunnel: tunnel({ state: 'off', reason: 'entry_off' }) }),
    ]
    for (const n of states) {
      const en = notes(n, 'en', '26.10.21')
      const fa = notes(n, 'fa', '26.10.21')
      expect(fa.map((x) => x.id)).toEqual(en.map((x) => x.id))
      for (const [i, note] of fa.entries()) {
        expect(note.title, note.id).not.toBe('')
        expect(note.title, note.id).not.toBe(en[i].title)
        expect(note.detail, note.id).not.toBe('')
        expect(note.tone).toBe(en[i].tone)
        expect(note.acts).toEqual(en[i].acts)
      }
    }
    const down = notes(node({ tunnel: tunnel({ state: 'off', reason: 'agent_down' }), lastSeenAt: ago(22) }), 'fa')[0]
    expect(plain(down.detail)).toMatch(/^آخرین دیده‌شدن: ([۰-۹]{2}:[۰-۹]{2}|[۰-۹]+ \S+)، ۲۲ دقیقه پیش\./)
    expect(plain(down.detail)).not.toMatch(/[0-9]/)
  })

  it('name a button the page has words for, in both languages', () => {
    const acts: NodeAct[] = ['updateXray', 'checkAgain', 'remove', 'showJob', 'openSettings', 'renewCert']
    for (const act of acts) {
      expect(enDict, act).toHaveProperty(`act.${act}`)
      expect(translate('fa', `act.${act}` as MsgKey), act).not.toBe(translate('en', `act.${act}` as MsgKey))
    }
  })
})
