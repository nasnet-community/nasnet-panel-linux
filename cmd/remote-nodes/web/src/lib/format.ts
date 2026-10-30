import { LANGS, localizeDigits, numberFormat, type Lang } from '../i18n/lang'
import { translate, type MsgKey, type Params } from '../i18n/translate'

export const DAY = 86400e3

const SIZES = ['B', 'KB', 'MB', 'GB', 'TB'] as const

const dateFormats = new Map<string, Intl.DateTimeFormat>()
const dates = (lang: Lang, options: Intl.DateTimeFormatOptions) => {
  const key = lang + JSON.stringify(options)
  let f = dateFormats.get(key)
  if (!f) dateFormats.set(key, (f = new Intl.DateTimeFormat(LANGS[lang].intl, options)))
  return f
}

const relatives = new Map<string, Intl.RelativeTimeFormat>()

/**
 * Numbers, sizes and times as text in one language: Persian digits and separators
 * in Persian, plain ones in English. Machine values (IPs, ports, versions, keys)
 * are not for these: they are shown as they are.
 */
export function makeFormat(lang: Lang) {
  const t = (key: MsgKey, params?: Params) => translate(lang, key, params)
  const num = (n: number) => numberFormat(lang, n)

  /** 1000-based, one decimal below 100: "999 B", "4.1 GB", "13.6 MB". */
  function size(value: number, rate: boolean): string {
    let v = value
    let unit = 0
    while (v >= 1000 && unit < SIZES.length - 1) {
      v /= 1000
      unit += 1
    }
    const digits = unit === 0 || v >= 100 ? 0 : 1
    const shown = numberFormat(lang, v, { minimumFractionDigits: digits, maximumFractionDigits: digits, useGrouping: false })
    return t(`unit.${SIZES[unit]}${rate ? 'ps' : ''}` as MsgKey, { value: shown })
  }

  function relative(iso: string, now = Date.now(), short = false): string {
    const diff = new Date(iso).getTime() - now
    const abs = Math.abs(diff)
    if (diff < 0 && abs < 60000) return t('time.justNow')
    const [n, unit] =
      abs >= DAY
        ? ([Math.round(abs / DAY), 'day'] as const)
        : abs >= 3600e3
          ? ([Math.round(abs / 3600e3), 'hour'] as const)
          : ([Math.max(1, Math.round(abs / 60e3)), 'minute'] as const)
    const key = `${lang}-${short}`
    let rtf = relatives.get(key)
    if (!rtf) relatives.set(key, (rtf = new Intl.RelativeTimeFormat(LANGS[lang].intl, { numeric: 'always', style: short ? 'short' : 'long' })))
    return rtf.format(diff >= 0 ? n : -n, unit)
  }

  const clock24 = (d: Date) => dates(lang, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(d)

  return {
    num,
    /** 62 as "62%" or "۶۲٪" */
    percent: (pct: number) => numberFormat(lang, pct / 100, { style: 'percent', maximumFractionDigits: 0 }),
    bytes: (bytes: number) => size(bytes, false),
    rate: (bytesPerSec: number) => size(bytesPerSec, true),

    /** "42s", "1h 2m", "6d 4h" */
    duration(seconds: number): string {
      const s = Math.max(0, Math.floor(seconds))
      const d = Math.floor(s / 86400)
      const h = Math.floor((s % 86400) / 3600)
      const m = Math.floor((s % 3600) / 60)
      if (d > 0) return t('duration.dh', { d, h })
      if (h > 0) return t('duration.hm', { h, m })
      if (m > 0) return t('duration.m', { m })
      return t('duration.s', { s })
    },

    /** A stopwatch: "2:14", "1:02:09". */
    clock(seconds: number): string {
      const s = Math.max(0, Math.floor(seconds))
      const h = Math.floor(s / 3600)
      const m = Math.floor((s % 3600) / 60)
      const pad = (n: number) => String(n).padStart(2, '0')
      return localizeDigits(`${h ? `${h}:${pad(m)}` : m}:${pad(s % 60)}`, lang)
    },

    /** "in 3 days", "2 hours ago", "just now", relative to `now`; `short` gives "2 min ago". */
    relative,

    /** "12 s ago" for the last two minutes, then like relative. */
    secondsAgo(iso: string, now = Date.now()): string {
      const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000))
      return s < 120 ? t('time.secondsAgo', { s }) : relative(iso, now)
    },

    /** Local "07:12" today, else "3 Oct" (the day in the Persian calendar for Persian). */
    clockOrDay(iso: string, now = Date.now()): string {
      const d = new Date(iso)
      return d.toDateString() === new Date(now).toDateString() ? clock24(d) : dates(lang, { day: 'numeric', month: 'short' }).format(d)
    },

    /** "27 Aug 2027". */
    date: (iso: string) => dates(lang, { day: 'numeric', month: 'short', year: 'numeric' }).format(new Date(iso)),
  }
}

export type Format = ReturnType<typeof makeFormat>
