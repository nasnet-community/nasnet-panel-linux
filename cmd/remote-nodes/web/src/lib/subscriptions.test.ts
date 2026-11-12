import { describe, expect, it } from 'vitest'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import type { Lang } from '../i18n/lang'
import type { Node, NodeSubscription, Subscription, SubscriptionLocal, SubscriptionNode } from './api'
import { makeFormat } from './format'
import {
  defaultNodeIds,
  enabledAt,
  lastSeen,
  nameList,
  nodeInfos,
  nodeSubscriptionStatus,
  onNodes,
  onlineAt,
  placesOf,
  portableLink,
  readyNodes,
  routerPlace,
  sortByName,
  subscriptionStatus,
  switchedText,
  totals,
  trafficText,
  upDown,
  type NodeInfo,
  type Place,
} from './subscriptions'

const NOW = new Date('2026-10-03T12:00:00').getTime()
const HOUR = 3600e3
const at = (ms: number) => new Date(NOW + ms).toISOString()

/** The page's words and formats in one language, as a screen gets them. */
const speak = (lang: Lang) => ({ lang, t: (key: MsgKey, params?: Params) => translate(lang, key, params), fmt: makeFormat(lang) })
const en = speak('en')
const fa = speak('fa')

// Intl can add direction marks around Persian text; they are not part of what the page shows
const plain = (s: string) => s.replace(/[‎‏‪-‮⁦-⁩]/g, '')

const node = (o: { id: number; name?: string; registered?: boolean; installing?: boolean; down?: boolean }) =>
  ({
    id: o.id,
    name: o.name ?? `Node ${o.id}`,
    registered: o.registered ?? true,
    installing: o.installing ? { jobId: 'j', step: 1, steps: 8, label: '', percent: null } : undefined,
    tunnel: { reason: o.down ? 'agent_down' : '' },
  }) as Node

const infos: NodeInfo[] = [
  { id: 1, name: 'Node 1', unreachable: false, installing: false },
  { id: 2, name: 'Node 2', unreachable: true, installing: false },
  { id: 3, name: 'Node 3', unreachable: false, installing: true },
]

describe('the nodes a subscription can be on', () => {
  it('lists installed nodes and those being installed, and leaves out a dead install', () => {
    const list = nodeInfos([node({ id: 1 }), node({ id: 2, down: true }), node({ id: 3, installing: true }), node({ id: 4, registered: false })])
    expect(list).toEqual(infos)
    // an install that is still going isn't registered yet at first, and still shows
    expect(nodeInfos([node({ id: 5, registered: false, installing: true })]).map((n) => n.id)).toEqual([5])
  })

  it('has switches only for nodes whose install is done', () => {
    expect(readyNodes(infos).map((n) => n.id)).toEqual([1, 2])
  })

  it('starts a new subscription on the nodes that work now', () => {
    expect(defaultNodeIds(infos)).toEqual([1])
    expect(defaultNodeIds([])).toEqual([])
  })
})

describe('sortByName', () => {
  const names = (list: { name: string }[]) => list.map((s) => s.name)
  const of = (...n: string[]) => n.map((name) => ({ name }))

  it('sorts without caring for case, and counts "Node 2" before "Node 10"', () => {
    expect(names(sortByName(of('sara', 'Ali', 'Node 10', 'Node 2', 'node 3', 'Zed'), 'en'))).toEqual(['Ali', 'Node 2', 'node 3', 'Node 10', 'sara', 'Zed'])
  })

  it('sorts Persian names in the Persian alphabet, and puts them first in a Persian page', () => {
    const list = of('سارا', 'علی', 'رضا', 'مینا', 'پارسا', 'دانا', 'کیان')
    expect(names(sortByName(list, 'fa'))).toEqual(['پارسا', 'دانا', 'رضا', 'سارا', 'علی', 'کیان', 'مینا'])
    expect(names(sortByName(of('Ali', 'سارا'), 'fa'))).toEqual(['سارا', 'Ali'])
    expect(names(sortByName(of('Ali', 'سارا'), 'en'))).toEqual(['Ali', 'سارا'])
  })

  it('leaves the list it was given alone', () => {
    const list = of('b', 'a')
    sortByName(list, 'en')
    expect(names(list)).toEqual(['b', 'a'])
  })
})

describe('node names in a sentence', () => {
  it('joins them the way the language does', () => {
    expect(nameList('en', ['Node 1'])).toBe('Node 1')
    expect(nameList('en', ['Node 1', 'Node 2'])).toBe('Node 1 and Node 2')
    expect(nameList('en', ['A', 'B', 'C'])).toBe('A, B and C')
    expect(plain(nameList('fa', ['نود ۱', 'نود ۲']))).toBe('نود ۱ و نود ۲')
  })

  it('names one or two nodes and counts more', () => {
    expect(onNodes(en.t, 'en', ['Node 1'])).toBe('on Node 1')
    expect(onNodes(en.t, 'en', ['Node 1', 'Node 2'])).toBe('on Node 1 and Node 2')
    expect(onNodes(en.t, 'en', ['Node 1', 'Node 2', 'Node 3'])).toBe('on 3 nodes')
    expect(onNodes(en.t, 'en', ['This router', 'Node 1', 'Node 2'], true)).toBe('on 3 places')
    expect(onNodes(en.t, 'en', ['This router', 'Node 1'], true)).toBe('on This router and Node 1')
    expect(plain(onNodes(fa.t, 'fa', ['نود ۱']))).toBe('روی نود ۱')
    expect(plain(onNodes(fa.t, 'fa', ['a', 'b', 'c']))).toBe('روی ۳ نود')
  })
})

describe('last seen', () => {
  const seen = (ms: number, c = en) => lastSeen(c.t, c.fmt, at(ms), NOW)

  it('says "never connected" when there is no time', () => {
    expect(lastSeen(en.t, en.fmt, null, NOW)).toBe('never connected')
    expect(lastSeen(fa.t, fa.fmt, null, NOW)).toBe('هرگز وصل نشده')
  })

  it('counts minutes, hours and days', () => {
    expect(seen(-25 * 60e3)).toBe('last seen 25 minutes ago')
    expect(seen(-3 * HOUR)).toBe('last seen 3 hours ago')
    expect(seen(-2 * 24 * HOUR)).toBe('last seen 2 days ago')
    expect(plain(seen(-3 * HOUR, fa))).toBe('آخرین بازدید ۳ ساعت پیش')
    expect(plain(seen(-2 * 24 * HOUR, fa))).toBe('آخرین بازدید ۲ روز پیش')
  })

  it('says "yesterday" for the day that a plain "1 day ago" would cover', () => {
    expect(seen(-24 * HOUR)).toBe('last seen yesterday')
    expect(seen(-35 * HOUR)).toBe('last seen yesterday')
    expect(seen(-37 * HOUR)).toBe('last seen 2 days ago')
    expect(seen(-23 * HOUR)).toBe('last seen 23 hours ago')
    expect(seen(-24 * HOUR, fa)).toBe('آخرین بازدید دیروز')
  })
})

describe('a subscription across the nodes', () => {
  const on = (nodeId: number, o: Partial<SubscriptionNode> = {}): SubscriptionNode => ({
    nodeId,
    enabled: true,
    online: false,
    connections: 0,
    lastSeenAt: null,
    todayUp: 0,
    todayDown: 0,
    monthUp: 0,
    monthDown: 0,
    ...o,
  })
  const sub = (o: Partial<Subscription>): Subscription => ({
    id: 1,
    name: 'Sara',
    createdAt: at(-9 * 24 * HOUR),
    trafficLimit: 0,
    trafficUsed: 0,
    expiresAt: null,
    stopped: '',
    online: false,
    lastSeenAt: null,
    todayUp: 0,
    todayDown: 0,
    monthUp: 0,
    monthDown: 0,
    nodes: [on(1)],
    local: null,
    ...o,
  })
  const status = (s: Subscription, c = en) => {
    const { key, label, sub: line } = subscriptionStatus(s, infos, c.t, c.fmt, c.lang, NOW)
    return [key, label, plain(line)]
  }

  it('is online, and says where', () => {
    expect(status(sub({ online: true, nodes: [on(1, { online: true }), on(2, { online: null })] }))).toEqual(['online', 'Online', 'on Node 1'])
    expect(status(sub({ online: true, nodes: [on(1, { online: true }), on(2, { online: true })] }))).toEqual(['online', 'Online', 'on Node 1 and Node 2'])
    expect(status(sub({ online: true, nodes: [on(1, { online: true })] }), fa)).toEqual(['online', 'آنلاین', 'روی Node 1'])
  })

  it('is active when nobody is connected, with the last time somebody was', () => {
    expect(status(sub({ lastSeenAt: at(-3 * HOUR) }))).toEqual(['active', 'Active', 'last seen 3 hours ago'])
    expect(status(sub({}))).toEqual(['active', 'Active', 'never connected'])
    expect(status(sub({ lastSeenAt: at(-24 * HOUR) }), fa)).toEqual(['active', 'فعال', 'آخرین بازدید دیروز'])
  })

  it('is off when no node has it switched on', () => {
    expect(status(sub({ nodes: [on(1, { enabled: false }), on(2, { enabled: false })] }))).toEqual(['off', 'Off', 'off on every node'])
    expect(status(sub({ nodes: [] }), fa)).toEqual(['off', 'خاموش', 'روی همهٔ نودها خاموش است'])
  })

  it('is unknown while its nodes cannot be reached', () => {
    expect(status(sub({ online: null, lastSeenAt: at(-HOUR) }))).toEqual(['unknown', 'Unknown', 'node unreachable'])
    expect(status(sub({ online: null }))).toEqual(['unknown', 'Unknown', 'never connected'])
    expect(status(sub({ online: null, lastSeenAt: at(-HOUR) }), fa)).toEqual(['unknown', 'نامشخص', 'نود در دسترس نیست'])
  })

  describe("with the router's own entry among the places", () => {
    const here = (o: Partial<SubscriptionLocal> = {}): SubscriptionLocal => ({ enabled: true, online: false, connections: 0, lastSeenAt: null, todayUp: 0, todayDown: 0, monthUp: 0, monthDown: 0, ...o })
    const router: Place = { id: 'router', name: 'This router', unreachable: false, installing: false }
    const places = placesOf(router, infos)
    const at2 = (s: Subscription, c = en) => {
      const { key, sub: line } = subscriptionStatus(s, places, c.t, c.fmt, c.lang, NOW)
      return [key, plain(line)]
    }

    it('is a place of its own, before the nodes, while it is switched on', () => {
      expect(routerPlace(en.t, { enabled: true, state: 'working' })).toEqual(router)
      expect(routerPlace(fa.t, { enabled: true, state: 'attention' })).toEqual({ id: 'router', name: 'این روتر', unreachable: true, installing: false })
      expect(routerPlace(en.t, { enabled: true, state: 'starting' })?.unreachable).toBe(true)
      for (const off of [{ enabled: false, state: 'off' } as const, null, undefined]) expect(routerPlace(en.t, off)).toBeNull()
      expect(places.map((p) => p.id)).toEqual(['router', 1, 2, 3])
      expect(placesOf(null, infos)).toBe(infos)
      // a new subscription starts on it when it works, as on a node that works
      expect(defaultNodeIds(places)).toEqual(['router', 1])
      expect(defaultNodeIds(placesOf({ ...router, unreachable: true }, infos))).toEqual([1])
      expect(readyNodes(places).map((p) => p.id)).toEqual(['router', 1, 2])
    })

    it('knows where a subscription is switched on and where it is connected', () => {
      const s = sub({ nodes: [on(1, { online: true }), on(2, { enabled: false })], local: here({ online: true }) })
      expect(places.map((p) => enabledAt(s, p.id))).toEqual([true, true, false, false])
      expect(places.map((p) => onlineAt(s, p.id))).toEqual([true, true, false, false])
      // off on the router, and on a router whose entry is switched off
      expect(enabledAt(sub({ local: here({ enabled: false }) }), 'router')).toBe(false)
      expect(enabledAt(sub({ local: null }), 'router')).toBe(false)
      expect(onlineAt(sub({ local: here({ online: null }) }), 'router')).toBe(false)
    })

    it('is online on the router, alone or beside a node', () => {
      expect(at2(sub({ online: true, nodes: [on(1)], local: here({ online: true }) }))).toEqual(['online', 'on This router'])
      expect(at2(sub({ online: true, nodes: [on(1, { online: true })], local: here({ online: true }) }))).toEqual(['online', 'on This router and Node 1'])
      // three of them are places, and not nodes
      expect(at2(sub({ online: true, nodes: [on(1, { online: true }), on(2, { online: true })], local: here({ online: true }) }))).toEqual(['online', 'on 3 places'])
      expect(at2(sub({ online: true, nodes: [on(1, { online: true }), on(2, { online: true })], local: here({ online: true }) }), fa)).toEqual(['online', 'روی ۳ جا'])
    })

    it('is on while only the router has it, and off when nothing has', () => {
      expect(at2(sub({ nodes: [on(1, { enabled: false })], local: here() }))).toEqual(['active', 'never connected'])
      expect(at2(sub({ nodes: [on(1, { enabled: false })], local: here({ enabled: false }) }))).toEqual(['off', 'off everywhere'])
      expect(at2(sub({ nodes: [], local: here({ enabled: false }) }), fa)).toEqual(['off', 'همه‌جا خاموش است'])
    })
  })
})

describe('a subscription on one node', () => {
  const row = (o: Partial<NodeSubscription>): NodeSubscription => ({
    id: 1,
    name: 'Sara',
    enabled: true,
    stopped: '',
    trafficLimit: 0,
    trafficUsed: 0,
    expiresAt: null,
    online: false,
    connections: 0,
    lastSeenAt: null,
    todayUp: 0,
    todayDown: 0,
    monthUp: 0,
    monthDown: 0,
    ...o,
  })
  const status = (s: NodeSubscription, c = en) => {
    const { key, label, sub } = nodeSubscriptionStatus(s, c.t, c.fmt, NOW)
    return [key, label, plain(sub)]
  }

  it('counts the connections of one that is online', () => {
    expect(status(row({ online: true, connections: 3 }))).toEqual(['online', 'Online', '3 connections'])
    expect(status(row({ online: true, connections: 1 }))).toEqual(['online', 'Online', '1 connection'])
    expect(status(row({ online: true, connections: 3 }), fa)).toEqual(['online', 'آنلاین', '۳ اتصال'])
    // a node that doesn't report connections still has it online
    expect(status(row({ online: true, connections: null }))[2]).toBe('0 connections')
  })

  it('is active with a last seen time, off on this node, or unknown while the node is down', () => {
    expect(status(row({ lastSeenAt: at(-2 * 24 * HOUR) }))).toEqual(['active', 'Active', 'last seen 2 days ago'])
    expect(status(row({ enabled: false, online: null }))).toEqual(['off', 'Off', 'off on this node'])
    expect(status(row({ enabled: false, online: null }), fa)).toEqual(['off', 'خاموش', 'روی این نود خاموش است'])
    expect(status(row({ online: null, lastSeenAt: at(-HOUR) }))).toEqual(['unknown', 'Unknown', 'node unreachable'])
    expect(status(row({ online: null }))).toEqual(['unknown', 'Unknown', 'never connected'])
  })

  it("words the router's own list for the router, and not for a node", () => {
    const here = (s: NodeSubscription, c = en) => plain(nodeSubscriptionStatus(s, c.t, c.fmt, NOW, true).sub)
    expect(here(row({ enabled: false, online: null }))).toBe('off on this router')
    expect(here(row({ enabled: false, online: null }), fa)).toBe('روی این روتر خاموش است')
    expect(here(row({ online: null, lastSeenAt: at(-HOUR) }))).toBe('this router is not working')
    // what doesn't name the place reads the same
    expect(here(row({ online: true, connections: 2 }))).toBe('2 connections')
    expect(here(row({ online: null }))).toBe('never connected')
  })
})

describe('what a toast says after a switch', () => {
  it('names the subscription and the node', () => {
    expect(switchedText(en.t, true, false, 'Sara', 'Node 1')).toBe('Sara switched on for Node 1')
    expect(switchedText(en.t, false, false, 'Sara', 'Node 1')).toBe('Sara switched off on Node 1')
    expect(switchedText(fa.t, true, false, 'Sara', 'Node 1')).toBe('Sara روی Node 1 روشن شد')
    expect(switchedText(fa.t, false, false, 'Sara', 'Node 1')).toBe('Sara روی Node 1 خاموش شد')
  })

  it('says when a node that is down will take the change, as a sentence of its own', () => {
    expect(switchedText(en.t, true, true, 'Ali', 'Node 2')).toBe('Ali switched on for Node 2. It takes effect when the node is back.')
    expect(switchedText(en.t, false, true, 'Ali', 'Node 2')).toBe('Ali switched off on Node 2. It takes effect when the node is back.')
    expect(switchedText(fa.t, true, true, 'Ali', 'Node 2')).toBe('Ali روی Node 2 روشن شد. وقتی نود برگردد اعمال می‌شود.')
    expect(switchedText(fa.t, false, true, 'Ali', 'Node 2')).toBe('Ali روی Node 2 خاموش شد. وقتی نود برگردد اعمال می‌شود.')
  })

  it("says the same of the router's own entry in its own words: it is not a node, and doesn't come back", () => {
    expect(switchedText(en.t, true, false, 'Ali', 'This router', true)).toBe('Ali switched on for This router')
    expect(switchedText(en.t, true, true, 'Ali', 'This router', true)).toBe('Ali switched on for This router. It takes effect when it is working again.')
    expect(switchedText(en.t, false, true, 'Ali', 'This router', true)).toBe('Ali switched off on This router. It takes effect when it is working again.')
    expect(switchedText(fa.t, true, true, 'Ali', 'این روتر', true)).toBe('Ali روی این روتر روشن شد. وقتی دوباره کار کند اعمال می‌شود.')
  })
})

describe('traffic', () => {
  it('adds up what a list moved', () => {
    const list = [
      { todayUp: 1, todayDown: 2, monthUp: 10, monthDown: 20 },
      { todayUp: 3, todayDown: 4, monthUp: 30, monthDown: 40 },
    ]
    expect(totals(list)).toEqual({ todayUp: 4, todayDown: 6, monthUp: 40, monthDown: 60 })
    expect(totals([])).toEqual({ todayUp: 0, todayDown: 0, monthUp: 0, monthDown: 0 })
  })

  it('words a cell in English', () => {
    expect(trafficText(en.t, en.fmt, 1.2e9, 38e9)).toEqual({ today: '1.2 GB', month: '38.0 GB in 30 days', line: '1.2 GB today · 38.0 GB in 30 days' })
    expect(trafficText(en.t, en.fmt, 0, 600e6)).toEqual({ today: '0 B', month: '600 MB in 30 days', line: '0 B today · 600 MB in 30 days' })
  })

  it('words it in Persian, with Persian digits and the decimal separator', () => {
    const text = trafficText(fa.t, fa.fmt, 1.2e9, 38e9)
    expect(plain(text.today)).toBe('۱٫۲ گیگابایت')
    expect(plain(text.month)).toBe('۳۸٫۰ گیگابایت در ۳۰ روز')
    expect(plain(text.line)).toBe('۱٫۲ گیگابایت امروز · ۳۸٫۰ گیگابایت در ۳۰ روز')
    expect(plain(trafficText(fa.t, fa.fmt, 0, 0).today)).toBe('۰ بایت')
  })

  it('writes up and down under a tile, with a mark after each arrow in Persian so it stays by its number', () => {
    expect(upDown(en.t, en.fmt, 1.0e9, 3.1e9)).toBe('↑ 1.0 GB · ↓ 3.1 GB')
    const text = upDown(fa.t, fa.fmt, 1.0e9, 3.1e9)
    expect(text).toContain('↑ ‎')
    expect(text).toContain('↓ ‎')
    expect(plain(text)).toBe('↑ ۱٫۰ گیگابایت · ↓ ۳٫۱ گیگابایت')
  })
})

describe('portableLink', () => {
  const link = 'vless://3f0c9a52-7d1e-4b8a-9c44-0e5b2a1d6f73@203.0.113.24:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=x7QnLw4Rk2VtB9yD0sEoUj3HaFcN6mXz8PqIg1KvTbA&sid=a1b2c3d4&type=tcp'
  const fragment = (l: string) => decodeURIComponent(l.slice(l.indexOf('#') + 1))

  it('leaves a link the hub encoded as it is, a Persian name included', () => {
    expect(portableLink(`${link}#Sara`)).toBe(`${link}#Sara`)
    const sent = `${link}#${encodeURIComponent('سارا')}`
    expect(sent.endsWith('#%D8%B3%D8%A7%D8%B1%D8%A7')).toBe(true)
    expect(portableLink(sent)).toBe(sent)
    expect(fragment(portableLink(sent))).toBe('سارا')
  })

  it('percent-encodes a name that arrives raw, so the name survives in a QR code', () => {
    const out = portableLink(`${link}#سارا`)
    expect(out).toBe(`${link}#%D8%B3%D8%A7%D8%B1%D8%A7`)
    expect(/^[\x00-\x7f]*$/.test(out)).toBe(true)
    expect(fragment(out)).toBe('سارا')
    // text around the name is kept, and so is a name made of several scripts
    expect(fragment(portableLink(`${link}#Sara سارا ✓`))).toBe('Sara سارا ✓')
    expect(portableLink(`${link}#Sara-%D8%B3`)).toBe(`${link}#Sara-%D8%B3`)
  })

  it('does not fail on text that has no UTF-8 form', () => {
    expect(portableLink(`${link}#a\ud800b`)).toBe(`${link}#a%EF%BF%BDb`)
  })
})
