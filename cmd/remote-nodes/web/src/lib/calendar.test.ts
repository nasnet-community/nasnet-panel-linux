import { describe, expect, it } from 'vitest'
import { addDays, calendarFor, isDay, weekday } from './calendar'

const fa = calendarFor('fa')
const en = calendarFor('en')

describe('days', () => {
  it('step across months and years, and know which exist', () => {
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01')
    expect(addDays('2028-03-01', -1)).toBe('2028-02-29')
    expect(isDay('2026-02-28')).toBe(true)
    for (const bad of ['2026-02-30', '2026-13-01', '26-01-01', '', 'today']) expect(isDay(bad), bad).toBe(false)
    // 1 January 2023 was a Sunday
    expect(weekday('2023-01-01')).toBe(0)
  })
})

describe('the Persian calendar', () => {
  it('starts each year at Nowruz', () => {
    expect(fa.kind).toBe('persian')
    expect(fa.parts('2024-03-20')).toEqual({ y: 1403, m: 1, d: 1 })
    expect(fa.parts('2025-03-21')).toEqual({ y: 1404, m: 1, d: 1 })
    expect(fa.parts('2025-03-20')).toEqual({ y: 1403, m: 12, d: 30 })
  })

  it('has months of 31, 30 and a last one of 29 or, in a leap year, 30 days', () => {
    expect(fa.monthDays('2024-04-01')).toHaveLength(31) // Farvardin
    expect(fa.monthDays('2024-10-01')).toHaveLength(30) // Mehr
    const esfand1403 = fa.monthDays('2025-03-01')
    expect(esfand1403).toHaveLength(30)
    expect(esfand1403[0]).toBe('2025-02-19')
    expect(esfand1403[29]).toBe('2025-03-20')
    expect(fa.monthDays('2026-03-01')).toHaveLength(29) // Esfand 1404
  })

  it('starts its weeks on Saturday, and names its days a letter each', () => {
    expect(fa.firstDay).toBe(6)
    const days = fa.weekdays()
    expect(days.map((d) => d.long)[0]).toBe('شنبه')
    for (const d of days) expect([...d.short]).toHaveLength(1)
  })

  it('lays a month out under its days, with Persian names and digits', () => {
    const azar = '2026-12-15' // 24 Azar 1405
    expect(fa.title(azar)).toContain('آذر')
    expect(fa.title(azar)).toContain('۱۴۰۵')
    expect(fa.dayNumber(azar)).toBe('۲۴')
    const weeks = fa.grid(azar)
    const first = weeks.flat().find((d) => d !== null)!
    expect(fa.parts(first)).toEqual({ y: 1405, m: 9, d: 1 })
    // the first day sits under its own weekday, counted from Saturday
    expect(weeks[0].indexOf(first)).toBe((weekday(first) - 6 + 7) % 7)
    expect(weeks.flat().filter(Boolean)).toHaveLength(30)
    for (const w of weeks) expect(w).toHaveLength(7)
  })

  it('moves by months, keeping the day where the month allows', () => {
    // 31 Shahrivar 1405 to Mehr, which has 30 days
    const lastOfShahrivar = fa.monthDays('2026-09-01').at(-1)!
    expect(fa.parts(lastOfShahrivar).d).toBe(31)
    expect(fa.parts(fa.shiftMonth(lastOfShahrivar, 1))).toEqual({ y: 1405, m: 7, d: 30 })
    expect(fa.parts(fa.shiftMonth('2026-12-15', 12))).toEqual({ y: 1406, m: 9, d: 24 })
    expect(fa.parts(fa.shiftMonth('2026-12-15', -9))).toEqual({ y: 1404, m: 12, d: 24 })
  })
})

describe('the Gregorian calendar', () => {
  it('starts its weeks on Monday in English (en-GB)', () => {
    expect(en.kind).toBe('gregory')
    expect(en.firstDay).toBe(1)
    expect(en.weekdays()[0].short).toBe('Mon')
  })

  it('lays December 2026 out from Tuesday the 1st', () => {
    const weeks = en.grid('2026-12-10')
    expect(weeks[0]).toEqual([null, '2026-12-01', '2026-12-02', '2026-12-03', '2026-12-04', '2026-12-05', '2026-12-06'])
    expect(weeks.flat().filter(Boolean)).toHaveLength(31)
    expect(en.title('2026-12-10')).toBe('December 2026')
    expect(en.label('2026-12-15')).toBe('Tuesday, 15 December 2026')
  })

  it('keeps leap days and clamps a month that is shorter', () => {
    expect(en.monthDays('2028-02-10')).toHaveLength(29)
    expect(en.shiftMonth('2026-01-31', 1)).toBe('2026-02-28')
    expect(en.shiftMonth('2026-03-31', -1)).toBe('2026-02-28')
    expect(en.shiftMonth('2026-12-15', 2)).toBe('2027-02-15')
  })
})
