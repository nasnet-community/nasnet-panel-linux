import { describe, expect, it } from 'vitest'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import type { Lang } from '../i18n/lang'
import type { NodeSubscription, Subscription } from './api'
import { makeFormat } from './format'
import {
  MAX_LIMIT_GB,
  endOfDay,
  expiryParts,
  limitsOf,
  statusText,
  extendDay,
  limitText,
  localDay,
  nodeSubscriptionStatus,
  parseLimit,
  quotaTone,
  readQuota,
  subscriptionStatus,
  today,
  usedPercent,
} from './subscriptions'

const NOW = new Date('2026-10-03T12:00:00').getTime()
const DAYMS = 86400e3
const speak = (lang: Lang) => ({ lang, t: (key: MsgKey, params?: Params) => translate(lang, key, params), fmt: makeFormat(lang) })
const en = speak('en')
const fa = speak('fa')
const plain = (s: string) => s.replace(/[‎‏‪-‮⁦-⁩]/g, '')

describe('the traffic limit field', () => {
  it('reads gigabytes as the page shows them: a billion bytes each', () => {
    expect(parseLimit('50')).toBe(50e9)
    expect(parseLimit(' 0.5 ')).toBe(0.5e9)
    expect(parseLimit('12,5')).toBe(12.5e9)
    expect(parseLimit('.25')).toBe(0.25e9)
    expect(parseLimit('3.')).toBe(3e9)
  })

  it('is empty for no limit, and refuses what is not a number above zero', () => {
    expect(parseLimit('')).toBeNull()
    expect(parseLimit('   ')).toBeNull()
    for (const bad of ['0', '0.0', '-5', 'abc', '1e3', '5 GB', '1.2.3', '١٢', String(MAX_LIMIT_GB + 1)]) expect(parseLimit(bad), bad).toBeUndefined()
    expect(parseLimit(String(MAX_LIMIT_GB))).toBe(MAX_LIMIT_GB * 1e9)
  })

  it('shows a limit without trailing zeros, and an empty field for none', () => {
    expect(limitText(50e9)).toBe('50')
    expect(limitText(0.5e9)).toBe('0.5')
    expect(limitText(12_345_000_000)).toBe('12.345')
    expect(limitText(0)).toBe('')
    // what the field shows reads back as the same bytes
    for (const bytes of [1, 999, 1.5e9, 7.25e10, 123_456_789_000]) expect(parseLimit(limitText(bytes))).toBe(parseLimit(String(Number((bytes / 1e9).toPrecision(6)))))
  })
})

describe('the end date', () => {
  it('ends at the last second of the day, in the person\'s own zone', () => {
    const iso = endOfDay('2027-01-31')
    expect(iso).not.toBeNull()
    const d = new Date(iso!)
    expect([d.getFullYear(), d.getMonth(), d.getDate(), d.getHours(), d.getMinutes(), d.getSeconds()]).toEqual([2027, 0, 31, 23, 59, 59])
    expect(localDay(iso!)).toBe('2027-01-31')
  })

  it('refuses what is not a day, and a day that does not exist', () => {
    for (const bad of ['', 'tomorrow', '2027-1-5', '2027-02-30', '2027-13-01', '31/01/2027']) expect(endOfDay(bad), bad).toBeNull()
  })

  it('gives today as the date field holds it', () => {
    expect(today(NOW)).toBe('2026-10-03')
  })
})

describe('reading both fields', () => {
  it('takes nothing, one, or both', () => {
    expect(readQuota('', '', NOW)).toEqual({ limit: null, expiresAt: null, badLimit: false, badDate: false })
    expect(readQuota('10', '', NOW)).toMatchObject({ limit: 10e9, expiresAt: null, badLimit: false, badDate: false })
    const both = readQuota('10', '2026-12-01', NOW)
    expect(both.limit).toBe(10e9)
    expect(localDay(both.expiresAt!)).toBe('2026-12-01')
  })

  it('flags each bad field on its own', () => {
    expect(readQuota('x', '2026-12-01', NOW)).toMatchObject({ badLimit: true, badDate: false })
    expect(readQuota('10', 'x', NOW)).toMatchObject({ badLimit: false, badDate: true })
    expect(readQuota('0', 'nope', NOW)).toMatchObject({ badLimit: true, badDate: true })
  })

  it('refuses a day that has passed, and takes today, which has not ended', () => {
    expect(readQuota('', '2026-10-02', NOW).badDate).toBe(true)
    expect(readQuota('', '2026-10-03', NOW).badDate).toBe(false)
    expect(readQuota('', '2026-10-03', new Date('2026-10-03T23:59:59').getTime() + 1500).badDate).toBe(true)
  })
})

describe('how full a limit is', () => {
  it('counts down to whole percents and never above 100', () => {
    expect(usedPercent(0, 100)).toBe(0)
    expect(usedPercent(999, 1000)).toBe(99)
    expect(usedPercent(1000, 1000)).toBe(100)
    expect(usedPercent(5000, 1000)).toBe(100)
    expect(usedPercent(10, 0)).toBe(0)
  })

  it('warns from 80% and goes red at the limit', () => {
    expect([0, 79, 80, 99, 100].map(quotaTone)).toEqual(['ok', 'ok', 'warn', 'warn', 'bad'])
  })
})

describe('the Limits column', () => {
  const iso = (days: number) => new Date(NOW + days * DAYMS).toISOString()
  const of = (o: { limit?: number; used?: number; days?: number }, c = en) => {
    const l = limitsOf({ trafficLimit: o.limit ?? 0, trafficUsed: o.used ?? 0, expiresAt: o.days === undefined ? null : iso(o.days) }, c.t, c.fmt, NOW)
    return { ...l, head: plain(l.head), foot: plain(l.foot) }
  }

  it('says None for a subscription with no limit and no end date', () => {
    expect(of({})).toEqual({ head: 'None', pct: null, foot: '', tone: 'muted' })
    // what it has used is no limit
    expect(of({ used: 5e9 }).head).toBe('None')
  })

  it('leads with the traffic limit, with what is left under its bar', () => {
    expect(of({ limit: 100e9, used: 62e9 })).toEqual({ head: '62.0 GB of 100 GB', pct: 62, foot: '38.0 GB left', tone: 'muted' })
    expect(of({ limit: 50e9, used: 50e9 })).toEqual({ head: '50.0 GB of 50.0 GB', pct: 100, foot: 'used up', tone: 'bad' })
    expect(of({ limit: 50e9, used: 60e9 })).toMatchObject({ pct: 100, foot: 'used up', tone: 'bad' })
  })

  it('puts the end date beside what is left, and warns in its last week', () => {
    expect(of({ limit: 100e9, used: 62e9, days: 40 })).toMatchObject({ pct: 62, tone: 'muted' })
    expect(of({ limit: 100e9, used: 62e9, days: 40 }).foot).toMatch(/^38\.0 GB left · ends .+, in 40 days$/)
    expect(of({ limit: 100e9, used: 62e9, days: 3 })).toMatchObject({ tone: 'warn' })
    // a limit that is used up is red whatever the date
    expect(of({ limit: 10e9, used: 10e9, days: 3 }).tone).toBe('bad')
    expect(of({ limit: 10e9, used: 10e9, days: 3 }).foot).toMatch(/^used up · ends .+, in 3 days$/)
    // and so is a date that has passed, whatever is left
    expect(of({ limit: 100e9, used: 1e9, days: -2 }).tone).toBe('bad')
    expect(of({ limit: 100e9, used: 1e9, days: -2 }).foot).toMatch(/^99\.0 GB left · ended .+, 2 days ago$/)
  })

  it('leads with the date when that is the only limit', () => {
    const soon = of({ days: 2 })
    expect(soon.head).toMatch(/^Ends \d{1,2} \w{3} 20\d\d$/)
    expect(soon).toMatchObject({ pct: null, foot: 'in 2 days', tone: 'warn' })
    expect(of({ days: 41 })).toMatchObject({ foot: 'in 41 days', tone: 'muted' })
    const past = of({ days: -3 })
    expect(past.head).toMatch(/^Ended /)
    expect(past).toMatchObject({ pct: null, foot: '3 days ago', tone: 'bad' })
  })

  it('reads in Persian too', () => {
    expect(of({}, fa).head).toBe('ندارد')
    expect(of({ days: 12 }, fa).head).toContain('پایان:')
    expect(of({ days: -3 }, fa).head).toContain('پایان یافته:')
    expect(of({ limit: 100e9, used: 62e9, days: 40 }, fa).foot).toContain('۴۰ روز')
  })
})

describe('a stopped subscription', () => {
  const infos = [{ id: 1, name: 'Node 1', unreachable: false, installing: false }]
  const sub = (o: Partial<Subscription>): Subscription => ({
    id: 1,
    name: 'Sara',
    createdAt: new Date(NOW).toISOString(),
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
    nodes: [{ nodeId: 1, enabled: true, online: false, connections: 0, lastSeenAt: null, todayUp: 0, todayDown: 0, monthUp: 0, monthDown: 0 }],
    local: null,
    ...o,
  })
  const status = (s: Subscription, c = en) => {
    const st = subscriptionStatus(s, infos, c.t, c.fmt, c.lang, NOW)
    return [st.key, plain(statusText(st))]
  }

  it('says that it is stopped, and why: its limit, or its date', () => {
    expect(status(sub({ stopped: 'limit', trafficLimit: 50e9, trafficUsed: 50e9 }))).toEqual(['limit', 'Stopped · limit reached'])
    const gone = status(sub({ stopped: 'expired', expiresAt: new Date(NOW - DAYMS).toISOString() }))
    expect(gone[0]).toBe('expired')
    expect(gone[1]).toMatch(/^Stopped · expired \d{1,2} \w{3} 20\d\d$/)
    // a date the hub no longer holds leaves the word
    expect(status(sub({ stopped: 'expired' }))).toEqual(['expired', 'Stopped · expired'])
  })

  it('shows before "off", since it says more, and in Persian', () => {
    const off = sub({ stopped: 'limit', trafficLimit: 1e9, trafficUsed: 1e9, nodes: [] })
    expect(status(off)[0]).toBe('limit')
    expect(status(sub({ stopped: 'limit', trafficLimit: 1e9, trafficUsed: 1e9 }), fa)[1]).toBe('متوقف · سقف ترافیک پر شد')
    expect(status(sub({ stopped: 'expired', expiresAt: new Date(NOW - DAYMS).toISOString() }), fa)[1]).toMatch(/^متوقف · منقضی شده در /)
  })

  it('has no word of its own when it is running', () => {
    expect(status(sub({ trafficLimit: 50e9, trafficUsed: 10e9 }))[0]).toBe('active')
  })

  it('shows on a node\'s list as well', () => {
    const row = (o: Partial<NodeSubscription>): NodeSubscription => ({
      id: 1, name: 'Sara', enabled: true, stopped: '', trafficLimit: 0, trafficUsed: 0, expiresAt: null, online: false, connections: 0, lastSeenAt: null, todayUp: 0, todayDown: 0, monthUp: 0, monthDown: 0, ...o,
    })
    expect(statusText(nodeSubscriptionStatus(row({ stopped: 'limit' }), en.t, en.fmt, NOW))).toBe('Stopped · limit reached')
    expect(nodeSubscriptionStatus(row({ stopped: 'expired' }), en.t, en.fmt, NOW)).toMatchObject({ key: 'expired', label: 'Stopped', sub: 'expired' })
    // switched off there is still just off
    expect(nodeSubscriptionStatus(row({ stopped: 'limit', enabled: false }), en.t, en.fmt, NOW).key).toBe('off')
  })
})

describe('the end date presets', () => {
  const at = (iso: string) => new Date(iso).getTime()

  it('count from today when there is no date, or it has passed', () => {
    expect(extendDay('', { days: 30 }, at('2026-10-03T12:00:00'))).toBe('2026-11-02')
    expect(extendDay('2026-09-01', { days: 30 }, at('2026-10-03T12:00:00'))).toBe('2026-11-02')
    expect(extendDay('not a day', { days: 90 }, at('2026-10-03T12:00:00'))).toBe('2027-01-01')
  })

  it('count on from a date that has not passed, so a second click adds again', () => {
    const now = at('2026-10-03T12:00:00')
    const once = extendDay('', { days: 30 }, now)
    expect(extendDay(once, { days: 30 }, now)).toBe('2026-12-02')
    // today has not ended yet, so it counts as a date that has not passed
    expect(extendDay('2026-10-03', { days: 1 }, now)).toBe('2026-10-04')
  })

  it('add a year as the same day a year on, and the 28th for a 29th of February', () => {
    expect(extendDay('', { years: 1 }, at('2026-10-03T12:00:00'))).toBe('2027-10-03')
    expect(extendDay('2028-02-29', { years: 1 }, at('2028-01-01T12:00:00'))).toBe('2029-02-28')
  })
})

describe('the Expires cell', () => {
  const iso = (days: number) => new Date(NOW + days * DAYMS).toISOString()

  it('is calm far off, warm in the last week, and red once past', () => {
    expect(expiryParts(en.t, en.fmt, iso(41), NOW)).toMatchObject({ tone: 'muted', when: 'in 41 days' })
    expect(expiryParts(en.t, en.fmt, iso(3), NOW)).toMatchObject({ tone: 'warn', when: 'in 3 days' })
    const past = expiryParts(en.t, en.fmt, iso(-2), NOW)
    expect(past.tone).toBe('bad')
    expect(past.when).toBe('ended 2 days ago')
    expect(past.date).toBe(en.fmt.date(iso(-2)))
  })

  it('reads in Persian', () => {
    expect(plain(expiryParts(fa.t, fa.fmt, iso(-2), NOW).when)).toContain('پایان یافته')
  })
})
