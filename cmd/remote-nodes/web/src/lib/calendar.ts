import { LANGS, type Lang } from '../i18n/lang'

/**
 * A day as the page keeps it: Gregorian, "2026-12-15", whatever calendar is on the screen. The
 * calendar here is only how a month is laid out and named. Months are found by asking Intl which
 * day of its month a date is, so the Persian calendar's leap years are Intl's to get right.
 */
export type Day = string

const pad = (n: number) => String(n).padStart(2, '0')
// whole days in UTC, so a zone's daylight saving can't shift one
const toUTC = (day: Day) => {
  const [y, m, d] = day.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d))
}
const fromUTC = (d: Date): Day => `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`

export const addDays = (day: Day, n: number): Day => {
  const d = toUTC(day)
  d.setUTCDate(d.getUTCDate() + n)
  return fromUTC(d)
}

/** Whether a string is a day that exists: "2026-02-30" is not. */
export const isDay = (s: string): boolean => /^\d{4}-\d{2}-\d{2}$/.test(s) && fromUTC(toUTC(s)) === s

/** 0 for Sunday to 6 for Saturday. */
export const weekday = (day: Day): number => toUTC(day).getUTCDay()

export interface Calendar {
  /** "persian" or "gregory" */
  kind: string
  /** the day a week starts on, 0 for Sunday to 6 for Saturday */
  firstDay: number
  /** the day in this calendar: its year, month and day of the month */
  parts(day: Day): { y: number; m: number; d: number }
  monthStart(day: Day): Day
  monthDays(day: Day): Day[]
  /** the same day of the month, n months on (or back), or that month's last day if it is shorter */
  shiftMonth(day: Day, n: number): Day
  /** "December 2026", "آذر ۱۴۰۵" */
  title(day: Day): string
  /** "15", "۲۴" */
  dayNumber(day: Day): string
  /** the week's days, from its first, short enough for a column */
  weekdays(): { short: string; long: string }[]
  /** the whole day, for a screen reader: "Tuesday 15 December 2026" */
  label(day: Day): string
  /** the month a day falls in, as weeks of seven, with null before its first day and after its last */
  grid(day: Day): (Day | null)[][]
}

const calendars = new Map<Lang, Calendar>()

export function calendarFor(lang: Lang): Calendar {
  const known = calendars.get(lang)
  if (known) return known

  const locale = LANGS[lang].intl
  const kind = new Intl.DateTimeFormat(locale).resolvedOptions().calendar
  const base = new Intl.Locale(locale).baseName
  // the week's first day is the locale's: Saturday in Iran, Monday for en-GB
  const info = (() => {
    try {
      const l = new Intl.Locale(locale) as Intl.Locale & { getWeekInfo?: () => { firstDay: number }; weekInfo?: { firstDay: number } }
      return l.getWeekInfo?.() ?? l.weekInfo
    } catch {
      return undefined
    }
  })()
  const firstDay = info ? info.firstDay % 7 : lang === 'fa' ? 6 : 1

  const numeric = new Intl.DateTimeFormat(`${base}-u-ca-${kind}-nu-latn`, { timeZone: 'UTC', year: 'numeric', month: 'numeric', day: 'numeric' })
  const titled = new Intl.DateTimeFormat(locale, { timeZone: 'UTC', month: 'long', year: 'numeric' })
  const dayOnly = new Intl.DateTimeFormat(locale, { timeZone: 'UTC', day: 'numeric' })
  const whole = new Intl.DateTimeFormat(locale, { timeZone: 'UTC', weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })
  // Persian names of the days are words; a letter each is what fits a column
  const shortDay = new Intl.DateTimeFormat(locale, { timeZone: 'UTC', weekday: lang === 'fa' ? 'narrow' : 'short' })
  const longDay = new Intl.DateTimeFormat(locale, { timeZone: 'UTC', weekday: 'long' })

  const parts = (day: Day) => {
    const out = { y: 0, m: 0, d: 0 }
    for (const p of numeric.formatToParts(toUTC(day))) {
      if (p.type === 'year') out.y = Number(p.value)
      else if (p.type === 'month') out.m = Number(p.value)
      else if (p.type === 'day') out.d = Number(p.value)
    }
    return out
  }
  const monthStart = (day: Day) => addDays(day, 1 - parts(day).d)
  const monthDays = (day: Day) => {
    const start = monthStart(day)
    const m = parts(start).m
    const out: Day[] = []
    for (let d = start; parts(d).m === m && out.length < 32; d = addDays(d, 1)) out.push(d)
    return out
  }
  const shiftMonth = (day: Day, n: number) => {
    const want = parts(day).d
    let start = monthStart(day)
    for (let i = 0; i < Math.abs(n); i += 1) start = n > 0 ? addDays(start, monthDays(start).length) : monthStart(addDays(start, -1))
    const days = monthDays(start)
    return days[Math.min(want, days.length) - 1]
  }

  const cal: Calendar = {
    kind,
    firstDay,
    parts,
    monthStart,
    monthDays,
    shiftMonth,
    title: (day) => titled.format(toUTC(day)),
    dayNumber: (day) => dayOnly.format(toUTC(day)),
    weekdays: () =>
      // 2023-01-01 was a Sunday
      Array.from({ length: 7 }, (_, i) => {
        const d = toUTC(addDays('2023-01-01', (firstDay + i) % 7))
        return { short: shortDay.format(d), long: longDay.format(d) }
      }),
    label: (day) => whole.format(toUTC(day)),
    grid: (day) => {
      const days = monthDays(day)
      const lead = (weekday(days[0]) - firstDay + 7) % 7
      const cells: (Day | null)[] = [...Array<null>(lead).fill(null), ...days]
      while (cells.length % 7) cells.push(null)
      const weeks: (Day | null)[][] = []
      for (let i = 0; i < cells.length; i += 7) weeks.push(cells.slice(i, i + 7))
      return weeks
    },
  }
  calendars.set(lang, cal)
  return cal
}
