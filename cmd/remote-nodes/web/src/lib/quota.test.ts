import { describe, expect, it } from 'vitest'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import type { Lang } from '../i18n/lang'
import type { NodeSubscription, Subscription } from './api'
import { makeFormat } from './format'
import {
  MAX_LIMIT_GB,
  endOfDay,
  expiryParts,
  expiryText,
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

describe('the end date as words', () => {
  const iso = (days: number) => new Date(NOW + days * DAYMS).toISOString()

  it('says when it ends and how long that is, or when it ended', () => {
    expect(plain(expiryText(en.t, en.fmt, iso(12), NOW))).toMatch(/^Expires .+ · in 12 days$/)
    expect(plain(expiryText(en.t, en.fmt, iso(-3), NOW))).toMatch(/^Expired /)
    expect(expiryText(en.t, en.fmt, null, NOW)).toBe('')
  })

  it('reads in Persian too', () => {
    expect(plain(expiryText(fa.t, fa.fmt, iso(12), NOW))).toContain('پایان:')
    expect(plain(expiryText(fa.t, fa.fmt, iso(-3), NOW))).toContain('پایان یافته:')
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
    ...o,
  })
  const status = (s: Subscription, c = en) => {
    const { key, label, sub: line } = subscriptionStatus(s, infos, c.t, c.fmt, c.lang, NOW)
    return [key, label, plain(line)]
  }

  it('says why: its limit, or its date', () => {
    expect(status(sub({ stopped: 'limit', trafficLimit: 50e9, trafficUsed: 50e9 }))).toEqual(['limit', 'Limit reached', '50.0 GB of 50.0 GB used'])
    const gone = status(sub({ stopped: 'expired', expiresAt: new Date(NOW - DAYMS).toISOString() }))
    expect(gone.slice(0, 2)).toEqual(['expired', 'Expired'])
    expect(gone[2]).toMatch(/^expired /)
  })

  it('shows before "off", since it says more, and in Persian', () => {
    const off = sub({ stopped: 'limit', trafficLimit: 1e9, trafficUsed: 1e9, nodes: [] })
    expect(status(off)[0]).toBe('limit')
    expect(status(sub({ stopped: 'limit', trafficLimit: 1e9, trafficUsed: 1e9 }), fa)[1]).toBe('سقف پر شد')
    expect(status(sub({ stopped: 'expired', expiresAt: new Date(NOW - DAYMS).toISOString() }), fa)[1]).toBe('منقضی شد')
  })

  it('has no word of its own when it is running', () => {
    expect(status(sub({ trafficLimit: 50e9, trafficUsed: 10e9 }))[0]).toBe('active')
  })

  it('shows on a node\'s list as well', () => {
    const row = (o: Partial<NodeSubscription>): NodeSubscription => ({
      id: 1, name: 'Sara', enabled: true, stopped: '', trafficLimit: 0, trafficUsed: 0, expiresAt: null, online: false, connections: 0, lastSeenAt: null, todayUp: 0, todayDown: 0, monthUp: 0, monthDown: 0, ...o,
    })
    expect(nodeSubscriptionStatus(row({ stopped: 'limit' }), en.t, en.fmt, NOW).key).toBe('limit')
    expect(nodeSubscriptionStatus(row({ stopped: 'expired' }), en.t, en.fmt, NOW).label).toBe('Expired')
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
