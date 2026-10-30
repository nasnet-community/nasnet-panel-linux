import { en, type MsgKey } from './en'
import { fa } from './fa'
import { LANGS, numberFormat, type Lang, type Message } from './lang'

export type { MsgKey }
export type Params = Record<string, string | number>

const DICTS: Record<Lang, Record<string, Message>> = { en, fa }

const rules: Partial<Record<Lang, Intl.PluralRules>> = {}
const rule = (lang: Lang) => (rules[lang] ??= new Intl.PluralRules(LANGS[lang].intl))

function pick(lang: Lang, message: Message, params: Params): string {
  if (typeof message === 'string') return message
  const n = Number(params[message.by ?? 'n'])
  return (message.one !== undefined && rule(lang).select(n) === 'one' ? message.one : message.other)
}

/**
 * A message in `lang`, with {name} filled from `params`. A number is written in the
 * language's digits; a string (an IP, a version, a name) goes in as it is. An
 * unknown key falls back to English, then to the key itself.
 */
export function translate(lang: Lang, key: MsgKey, params: Params = {}): string {
  const message = DICTS[lang][key] ?? DICTS.en[key]
  if (message === undefined) return key
  return pick(lang, message, params).replace(/\{(\w+)\}/g, (whole, name: string) => {
    const v = params[name]
    return v === undefined ? whole : typeof v === 'number' ? numberFormat(lang, v) : v
  })
}
