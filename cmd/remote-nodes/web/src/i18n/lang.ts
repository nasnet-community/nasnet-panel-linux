export type Lang = 'en' | 'fa'

/** A message with one form per plural category. Persian has no plural forms, so it can use a plain string. */
export interface Plural {
  one?: string
  other: string
  /** the parameter that picks the form; `n` when left out */
  by?: string
}

export type Message = string | Plural

export interface LangInfo {
  code: Lang
  dir: 'ltr' | 'rtl'
  /** locale for numbers, dates and times; Farsi always gets Persian digits */
  intl: string
  /** its own name, so someone who can't read the current language can still find it */
  name: string
  /** what the header button shows */
  short: string
}

// fa-IR counts dates in the Persian calendar, as nasnet-panel does.
export const LANGS: Record<Lang, LangInfo> = {
  en: { code: 'en', dir: 'ltr', intl: 'en-GB', name: 'English', short: 'EN' },
  fa: { code: 'fa', dir: 'rtl', intl: 'fa-IR-u-nu-arabext', name: 'فارسی', short: 'فا' },
}

export const LANG_KEY = 'remote-nodes.language'
// the panel stores its own choice here, and this page lives on the panel's origin
const PANEL_LANG_KEY = 'nasnet-panel.language'

/** The saved choice, else the panel's, else the browser's language. */
export function detectLang(storage: Pick<Storage, 'getItem'> | null, browserLang = ''): Lang {
  for (const key of [LANG_KEY, PANEL_LANG_KEY]) {
    try {
      const saved = storage?.getItem(key)
      if (saved === 'en' || saved === 'fa') return saved
    } catch {
      // blocked storage: fall through to the browser's language
    }
  }
  return browserLang.toLowerCase().startsWith('fa') ? 'fa' : 'en'
}

const formats = new Map<string, Intl.NumberFormat>()

/** A number in the language's digits and separators. */
export function numberFormat(lang: Lang, n: number, options?: Intl.NumberFormatOptions): string {
  const key = lang + JSON.stringify(options ?? '')
  let f = formats.get(key)
  if (!f) formats.set(key, (f = new Intl.NumberFormat(LANGS[lang].intl, options)))
  return f.format(n)
}

const PERSIAN_DIGITS = '۰۱۲۳۴۵۶۷۸۹'

/** The digits of a string in the language's script: "2:14" becomes "۲:۱۴" in Persian. */
export const localizeDigits = (text: string, lang: Lang) =>
  lang === 'fa' ? text.replace(/\d/g, (d) => PERSIAN_DIGITS[Number(d)]) : text
