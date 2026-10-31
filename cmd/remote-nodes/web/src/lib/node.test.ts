import { describe, expect, it } from 'vitest'
import { en } from '../i18n/en'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import type { Lang } from '../i18n/lang'
import type { Node, Traffic } from './api'
import { makeFormat } from './format'
import { stepLabel } from './jobs'
import {
  CHART_WINDOW,
  REPO_URL,
  axisRate,
  cmpVersion,
  eventLabel,
  niceMax,
  normalizeTarget,
  openConnections,
  plot,
  renewLeadDays,
  sourceUrl,
  validServerName,
  versionRelation,
} from './node'

// Intl can add direction marks around Persian text; they are not part of what the page shows
const plain = (s: string) => s.replace(/[‎‏‪-‮⁦-⁩]/g, '')
const words = (lang: Lang) => (key: MsgKey, params?: Params) => translate(lang, key, params)

describe('version compare', () => {
  it('compares number by number, not letter by letter', () => {
    expect(cmpVersion('26.10.21', '26.10.21')).toBe(0)
    expect(cmpVersion('26.10.9', '26.10.21')).toBe(-1)
    expect(cmpVersion('26.10.21', '26.10.9')).toBe(1)
    expect(cmpVersion('1.10.0', '1.9.0')).toBe(1)
    expect(cmpVersion('26.11.0', '26.10.21')).toBe(1)
    expect(cmpVersion('25.12.31', '26.1.1')).toBe(-1)
  })

  it('treats a missing part as zero', () => {
    expect(cmpVersion('26.10', '26.10.0')).toBe(0)
    expect(cmpVersion('26.10', '26.10.1')).toBe(-1)
    expect(cmpVersion('27', '26.10.21')).toBe(1)
  })

  it('ignores a leading v and what follows a dash or plus', () => {
    expect(cmpVersion('v26.10.21', '26.10.21')).toBe(0)
    expect(cmpVersion('26.10.21-rc1', '26.10.21')).toBe(0)
    expect(cmpVersion('1.3.0+build5', '1.3.0')).toBe(0)
  })

  it('puts a version nobody reported, or a dev build, before any real one', () => {
    expect(cmpVersion('', '1.0.0')).toBe(-1)
    expect(cmpVersion('dev', '1.0.0')).toBe(-1)
    expect(cmpVersion('', '')).toBe(0)
  })

  it('says what a node runs next to the version the router uses', () => {
    expect(versionRelation('26.10.18', '26.10.21')).toBe('older')
    expect(versionRelation('26.10.21', '26.10.21')).toBe('same')
    expect(versionRelation('26.11.2', '26.10.21')).toBe('newer')
  })
})

describe('the chart axis', () => {
  it('goes up in 1, 2 and 5 times a power of ten, and never below a quiet line', () => {
    expect(niceMax(0)).toBe(100_000)
    expect(niceMax(40_000)).toBe(100_000)
    expect(niceMax(100_000)).toBe(100_000)
    expect(niceMax(100_001)).toBe(200_000)
    expect(niceMax(200_000)).toBe(200_000)
    expect(niceMax(200_001)).toBe(500_000)
    expect(niceMax(500_000)).toBe(500_000)
    expect(niceMax(500_001)).toBe(1_000_000)
    expect(niceMax(1.9e6)).toBe(2e6)
    expect(niceMax(2.1e6)).toBe(5e6)
    expect(niceMax(7e6)).toBe(1e7)
    expect(niceMax(1.2e9)).toBe(2e9)
  })

  it('always has room for the value', () => {
    for (const v of [1, 99_999, 123_456, 999_999, 3_333_333, 87_654_321, 4.2e10]) expect(niceMax(v)).toBeGreaterThanOrEqual(v)
  })

  it('writes the unit once and drops a needless .0, in both languages', () => {
    const en = makeFormat('en')
    const fa = makeFormat('fa')
    expect(axisRate(en, 2e6)).toBe('2 MB/s')
    expect(axisRate(en, 1e6)).toBe('1 MB/s')
    expect(axisRate(en, 2.5e6)).toBe('2.5 MB/s')
    expect(axisRate(en, 500_000)).toBe('500 KB/s')
    expect(axisRate(en, 50_000)).toBe('50 KB/s')
    expect(plain(axisRate(fa, 2e6))).toBe('۲ مگابایت بر ثانیه')
    expect(plain(axisRate(fa, 2.5e6))).toBe('۲٫۵ مگابایت بر ثانیه')
    expect(plain(axisRate(fa, 500_000))).toBe('۵۰۰ کیلوبایت بر ثانیه')
  })
})

describe('plot', () => {
  const NOW = Date.parse('2026-10-30T12:00:00Z')
  const at = (secondsAgo: number) => new Date(NOW - secondsAgo * 1000).toISOString()
  const traffic = (...samples: [number, number, number][]): Traffic => ({
    stepSec: 15,
    samples: samples.map(([ago, up, down]) => ({ at: at(ago), up, down })),
  })

  it('puts now at the right edge, 15 minutes ago at the left, and the top of the axis at the top', () => {
    const p = plot(traffic([900, 0, 0], [450, 100_000, 1_000_000], [0, 50_000, 2_000_000]), NOW, 600, 140)
    expect(p.top).toBe(2e6)
    expect(p.down).toBe('M0.0,140.0L300.0,70.0L600.0,0.0')
    expect(p.up).toBe('M0.0,140.0L300.0,133.0L600.0,136.5')
    // the fill closes down to the baseline and back to where the line began
    expect(p.area).toBe('M0.0,140.0L300.0,70.0L600.0,0.0L600.0,140L0.0,140Z')
    expect(p.drawn).toBe(true)
    expect(p.last).toEqual({ up: 50_000, down: 2_000_000 })
    expect(p.peak).toBe(2_000_000)
    expect(CHART_WINDOW).toBe(15 * 60e3)
  })

  it('keeps the one sample just before the window, so the line starts at the edge, and drops older ones', () => {
    const p = plot(traffic([1000, 0, 9_000_000], [905, 0, 1_000_000], [0, 0, 1_000_000]), NOW, 600, 140)
    // 1000 s ago is more than a step before the window: dropped, and it does not set the axis
    expect(p.top).toBe(1e6)
    expect(p.down).toBe('M0.0,0.0L600.0,0.0')
  })

  it('starts a hair inside the edge when the oldest sample is a hair inside the window, and drops it a step past', () => {
    // the page's clock ticks once a second, so it can trail the samples a little
    const inside = plot(traffic([899.7, 0, 1_000_000], [0, 0, 1_000_000]), NOW, 600, 140)
    expect(inside.down).toBe('M0.2,0.0L600.0,0.0')
    const gone = plot(traffic([915.5, 0, 9_000_000], [900.5, 0, 1_000_000], [0, 0, 1_000_000]), NOW, 600, 140)
    expect(gone.down).toBe('M0.0,0.0L600.0,0.0')
    expect(gone.top).toBe(1e6)
  })

  it('draws samples in time order whatever order they came in, and holds a future one at the edge', () => {
    const p = plot(traffic([0, 0, 1_000_000], [450, 0, 500_000], [-30, 0, 250_000]), NOW, 600, 140)
    expect(p.down.startsWith('M300.0,')).toBe(true)
    expect(p.down.endsWith('L600.0,105.0')).toBe(true)
    expect(p.last?.down).toBe(250_000)
  })

  it('takes an empty list that arrived as null for an empty list', () => {
    const none = plot({ stepSec: 15, samples: null as unknown as Traffic['samples'] }, NOW, 600, 140)
    expect(none.drawn).toBe(false)
    expect(none.last).toBeNull()
  })

  it('has nothing to draw without two samples, and says what it has', () => {
    const none = plot({ stepSec: 15, samples: [] }, NOW, 600, 140)
    expect(none).toEqual({ top: 100_000, down: '', area: '', up: '', drawn: false, last: null, peak: 0 })
    const one = plot(traffic([0, 10, 20]), NOW, 600, 140)
    expect(one.drawn).toBe(false)
    expect(one.last).toEqual({ up: 10, down: 20 })
  })
})

describe('what can be typed in', () => {
  it('makes a handshake target of a site: host:port, lower case, no scheme or path', () => {
    expect(normalizeTarget('www.example.com')).toBe('www.example.com:443')
    expect(normalizeTarget('WWW.Example.com:8443')).toBe('www.example.com:8443')
    expect(normalizeTarget('  example.org:443  ')).toBe('example.org:443')
    expect(normalizeTarget('https://www.example.com/path?x=1')).toBe('www.example.com:443')
    expect(normalizeTarget('https://example.com:8443/x')).toBe('example.com:8443')
    expect(normalizeTarget('example.xn--p1ai')).toBe('example.xn--p1ai:443')
  })

  it('refuses what is not a site', () => {
    for (const bad of ['', '   ', 'localhost', '1.2.3.4', '1.2.3.4:443', 'a b.com', 'example.com:0', 'example.com:99999', 'example.com:abc', '-a.com', 'a..com', 'https://']) {
      expect(normalizeTarget(bad), JSON.stringify(bad)).toBe('')
    }
  })

  it('takes a host name as a server name, and nothing else', () => {
    expect(validServerName('cdn.example.com')).toBe(true)
    expect(validServerName(' Cdn.Example.COM ')).toBe(true)
    expect(validServerName('a.b')).toBe(true)
    for (const bad of ['', 'example', '1.2.3.4', 'a_b.com', '-a.com', 'a.com-', 'a b.com', `${'a'.repeat(250)}.com`]) {
      expect(validServerName(bad), JSON.stringify(bad)).toBe(false)
    }
  })
})

describe('what a node reports', () => {
  const node = (o: Partial<Node>): Node => o as Node

  it('counts the connections a restart cuts off, from the subscriptions when the agent says, else from the tunnel', () => {
    expect(openConnections(node({ usage: { live: true, connections: 9 } as Node['usage'], tunnel: { streams: 412 } as Node['tunnel'] }))).toBe(9)
    expect(openConnections(node({ usage: { live: false, connections: 0 } as Node['usage'], tunnel: { streams: 412 } as Node['tunnel'] }))).toBe(412)
  })

  it('says how many days before its end the certificate is renewed', () => {
    expect(renewLeadDays({ notAfter: '2027-08-27T12:00:00Z', renewsAt: '2027-07-28T12:00:00Z', validDays: 301 })).toBe(30)
  })

  it("words an event by its kind, and shows one the page doesn't know as itself", () => {
    const kinds = ['tunnel_up', 'tunnel_down', 'node_unreachable', 'node_back', 'agent_updated', 'xray_changed', 'cert_renewed', 'subscription_on', 'subscription_off', 'keys_rotated']
    for (const kind of kinds) {
      // every kind the contract lists has its words in both languages
      expect(en, kind).toHaveProperty(`event.${kind}`)
      expect(eventLabel(words('en'), kind)).toBe(en[`event.${kind}` as MsgKey])
      expect(eventLabel(words('fa'), kind)).not.toBe(eventLabel(words('en'), kind))
    }
    expect(eventLabel(words('en'), 'cert_renewed')).toBe('Certificate renewed')
    expect(eventLabel(words('fa'), 'cert_renewed')).toBe('گواهی تمدید شد')
    expect(eventLabel(words('en'), 'portal_moved_house')).toBe('Portal moved house')
    expect(eventLabel(words('fa'), 'portal_moved_house')).toBe('Portal moved house')
  })

  it('words the steps of the jobs the node page starts, in both languages', () => {
    for (const lang of ['en', 'fa'] as const) {
      const t = words(lang)
      for (const step of ['cert', 'reconnect', 'keys', 'remove', 'forget']) {
        // a step the page has no words for shows as its name, capitalised
        expect(stepLabel(t, step), `${step} in ${lang}`).not.toBe(step.charAt(0).toUpperCase() + step.slice(1))
      }
    }
    expect(stepLabel(words('en'), 'cert')).toBe('Issue a new certificate')
    expect(stepLabel(words('en'), 'forget')).toBe('Forget the node')
  })
})

describe("the node page's numbers, in words", () => {
  const en = words('en')
  const fa = words('fa')
  const fen = makeFormat('en')
  const ffa = makeFormat('fa')

  it('says how long a node has been up', () => {
    const up = 6 * 86400 + 4 * 3600
    expect(en('node.up', { time: fen.duration(up) })).toBe('up 6d 4h')
    expect(plain(fa('node.up', { time: ffa.duration(up) }))).toBe('۶ روز و ۴ ساعت روشن')
    expect(en('node.up', { time: fen.duration(42) })).toBe('up 42s')
    expect(fa('node.up', { time: ffa.duration(42) })).toBe('۴۲ ثانیه روشن')
  })

  it('counts days, round trips and the days before a renewal', () => {
    expect(en('status.days', { n: 301 })).toBe('301 days')
    expect(en('status.days', { n: 1 })).toBe('1 day')
    expect(fa('status.days', { n: 301 })).toBe('۳۰۱ روز')
    expect(en('unit.ms', { n: 23 })).toBe('23 ms')
    expect(fa('unit.ms', { n: 23 })).toBe('۲۳ میلی‌ثانیه')
    expect(en('status.renewsLead', { n: 30 })).toBe('30 days before it ends')
    expect(fa('status.renewsLead', { n: 30 })).toBe('۳۰ روز پیش از پایان')
    expect(en('set.cert.renews', { n: 1 })).toBe('renews by itself 1 day before')
  })

  it('sizes up a handshake and keeps the protocol names Latin', () => {
    const tls = 'TLS 1.3 · X25519'
    expect(en('hs.facts', { tls, size: fen.bytes(4200) })).toBe('TLS 1.3 · X25519 · 4.2 KB handshake, checked from this node')
    const persian = plain(fa('hs.facts', { tls, size: ffa.bytes(4200) }))
    expect(persian).toContain('TLS 1.3 · X25519')
    expect(persian).toContain('۴٫۲ کیلوبایت')
  })

  it('labels the chart in minutes, in the language\'s digits', () => {
    expect(en('chart.minAgo', { n: 15 })).toBe('15 min ago')
    expect(en('chart.min', { n: 10 })).toBe('10 min')
    expect(fa('chart.minAgo', { n: 15 })).toBe('۱۵ دقیقه پیش')
    expect(fa('chart.min', { n: 5 })).toBe('۵ دقیقه')
    const label = (f: ReturnType<typeof makeFormat>, t: typeof en) => t('chart.label', { down: f.rate(1.2e6), peak: f.rate(1.9e6), up: f.rate(0.3e6) })
    expect(label(fen, en)).toBe('Download 1.2 MB/s now, at most 1.9 MB/s in the last 15 minutes. Upload 300 KB/s now.')
    expect(plain(label(ffa, fa))).toBe('دانلود اکنون ۱٫۲ مگابایت بر ثانیه، حداکثر ۱٫۹ مگابایت بر ثانیه در ۱۵ دقیقهٔ اخیر. آپلود اکنون ۳۰۰ کیلوبایت بر ثانیه.')
  })
})

describe('the link to the source', () => {
  it("points at the build's own commit when it has one", () => {
    expect(sourceUrl('a1b2c3d')).toBe(`${REPO_URL}/tree/a1b2c3d`)
    expect(sourceUrl('A1B2C3D4E5F60718293A4B5C6D7E8F9012345678')).toBe(`${REPO_URL}/tree/A1B2C3D4E5F60718293A4B5C6D7E8F9012345678`)
  })

  it('falls back to the repository for a build without one', () => {
    for (const commit of ['', 'dev', 'unknown', 'a1b2c3']) {
      expect(sourceUrl(commit), commit).toBe(REPO_URL)
    }
  })
})
