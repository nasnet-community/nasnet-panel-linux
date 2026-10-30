import { describe, expect, it } from 'vitest'
import { makeFormat } from './format'

const en = makeFormat('en')
const fa = makeFormat('fa')

// Intl can add direction marks around Persian text; they are not part of what the page shows
const plain = (s: string) => s.replace(/[‎‏‪-‮⁦-⁩]/g, '')

const NOW = new Date('2026-10-03T12:00:00').getTime()
const at = (ms: number) => new Date(NOW + ms).toISOString()

describe('numbers', () => {
  it('uses plain digits in English and Persian digits and separators in Persian', () => {
    expect(en.num(1_884_201)).toBe('1,884,201')
    expect(plain(fa.num(1_884_201))).toBe('۱٬۸۸۴٬۲۰۱')
    expect(en.num(0)).toBe('0')
    expect(fa.num(0)).toBe('۰')
    expect(en.percent(62)).toBe('62%')
    expect(fa.percent(62)).toBe('۶۲٪')
  })
})

describe('sizes and rates', () => {
  it('formats bytes with one decimal below 100', () => {
    expect(en.bytes(0)).toBe('0 B')
    expect(en.bytes(999)).toBe('999 B')
    expect(en.bytes(1_500)).toBe('1.5 KB')
    expect(en.bytes(12_345_678)).toBe('12.3 MB')
    expect(en.bytes(5e9)).toBe('5.0 GB')
    expect(en.bytes(4.1e9)).toBe('4.1 GB')
    expect(en.bytes(123_456_789_012)).toBe('123 GB')
    expect(en.rate(1_200_000)).toBe('1.2 MB/s')
  })

  it('writes sizes in Persian words and digits, with the Persian decimal separator', () => {
    expect(fa.bytes(0)).toBe('۰ بایت')
    expect(fa.bytes(4.1e9)).toBe('۴٫۱ گیگابایت')
    expect(fa.bytes(13.6e6)).toBe('۱۳٫۶ مگابایت')
    expect(fa.bytes(310e6)).toBe('۳۱۰ مگابایت')
    expect(fa.rate(1_200_000)).toBe('۱٫۲ مگابایت بر ثانیه')
  })
})

describe('durations and clocks', () => {
  it('formats durations and stopwatches', () => {
    expect(en.duration(42)).toBe('42s')
    expect(en.duration(120)).toBe('2m')
    expect(en.duration(3_720)).toBe('1h 2m')
    expect(en.duration(90_000)).toBe('1d 1h')
    expect(en.clock(0)).toBe('0:00')
    expect(en.clock(134)).toBe('2:14')
    expect(en.clock(3729)).toBe('1:02:09')
  })

  it('writes them in Persian digits too', () => {
    expect(fa.duration(42)).toBe('۴۲ ثانیه')
    expect(fa.duration(3_720)).toBe('۱ ساعت و ۲ دقیقه')
    expect(fa.duration(90_000)).toBe('۱ روز و ۱ ساعت')
    expect(fa.clock(134)).toBe('۲:۱۴')
    expect(fa.clock(3729)).toBe('۱:۰۲:۰۹')
  })
})

describe('times relative to now', () => {
  it('says how far off a time is, in words', () => {
    expect(en.relative(at(3 * 86400e3), NOW)).toBe('in 3 days')
    expect(en.relative(at(-2 * 3600e3), NOW)).toBe('2 hours ago')
    expect(en.relative(at(20e3), NOW)).toBe('in 1 minute')
    expect(en.relative(at(-20e3), NOW)).toBe('just now')
    expect(en.relative(at(-80e3), NOW)).toBe('1 minute ago')
    expect(en.relative(at(-22 * 60e3), NOW)).toBe('22 minutes ago')
  })

  it('does the same in Persian, with Persian digits', () => {
    expect(plain(fa.relative(at(3 * 86400e3), NOW))).toBe('۳ روز دیگر')
    expect(plain(fa.relative(at(-2 * 3600e3), NOW))).toBe('۲ ساعت پیش')
    expect(fa.relative(at(-20e3), NOW)).toBe('همین الان')
    expect(plain(fa.relative(at(-22 * 60e3), NOW))).toBe('۲۲ دقیقه پیش')
  })

  it('has a short form too, which Persian words the same', () => {
    expect(en.relative(at(-2 * 60e3), NOW, true)).toBe('2 min ago')
    expect(en.relative(at(60 * 60e3), NOW, true)).toBe('in 1 hr')
    expect(plain(fa.relative(at(-2 * 60e3), NOW, true))).toBe('۲ دقیقه پیش')
  })

  it('counts seconds for the first two minutes, then falls back to words', () => {
    expect(en.secondsAgo(at(-10e3), NOW)).toBe('10 s ago')
    expect(en.secondsAgo(at(-10 * 60e3), NOW)).toBe('10 minutes ago')
    expect(fa.secondsAgo(at(-10e3), NOW)).toBe('۱۰ ثانیه پیش')
    expect(plain(fa.secondsAgo(at(-10 * 60e3), NOW))).toBe('۱۰ دقیقه پیش')
  })
})

describe('dates', () => {
  it('shows the time today and the day before that', () => {
    const today = new Date('2026-10-03T07:12:00').toISOString()
    const earlier = new Date('2026-10-01T07:12:00').toISOString()
    expect(en.clockOrDay(today, NOW)).toBe('07:12')
    expect(fa.clockOrDay(today, NOW)).toBe('۰۷:۱۲')
    expect(en.clockOrDay(earlier, NOW)).toBe('1 Oct')
  })

  it('counts the day in the Persian calendar for Persian', () => {
    const earlier = new Date('2026-10-01T07:12:00').toISOString()
    expect(plain(fa.clockOrDay(earlier, NOW))).toBe('۹ مهر')
  })

  it('gives a full date with the year', () => {
    expect(en.date(new Date('2027-08-27T12:00:00').toISOString())).toBe('27 Aug 2027')
    expect(plain(fa.date(new Date('2027-08-27T12:00:00').toISOString()))).toBe('۵ شهریور ۱۴۰۶')
  })
})
