import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { makeFormat, type Format } from '../lib/format'
import { errorText, type T } from './errors'
import { LANGS, LANG_KEY, detectLang, type Lang } from './lang'
import { translate, type MsgKey, type Params } from './translate'

export { LANGS, type Lang, type MsgKey, type Params, type T }

interface LangValue {
  lang: Lang
  dir: 'ltr' | 'rtl'
  t: T
  /** numbers, sizes and times in the language's digits */
  fmt: Format
  setLang: (lang: Lang) => void
}

const LangContext = createContext<LangValue>(null!)

/** The language, its text and its formats. */
export const useLang = () => useContext(LangContext)

const storage = () => {
  try {
    return window.localStorage
  } catch {
    return null
  }
}

/** Sets what the browser needs to lay the page out and pick fonts: lang and dir on <html>. */
function apply(lang: Lang) {
  document.documentElement.lang = lang
  document.documentElement.dir = LANGS[lang].dir
}

export function LangProvider({ children }: { children: ReactNode }) {
  const [lang, set] = useState<Lang>(() => {
    const first = detectLang(storage(), navigator.language)
    apply(first)
    return first
  })

  const setLang = useCallback((next: Lang) => {
    apply(next)
    try {
      localStorage.setItem(LANG_KEY, next)
    } catch {
      // Storage may be blocked; the choice then lasts for this page view.
    }
    set(next)
  }, [])

  const value = useMemo<LangValue>(
    () => ({
      lang,
      dir: LANGS[lang].dir,
      t: (key, params) => translate(lang, key, params),
      fmt: makeFormat(lang),
      setLang,
    }),
    [lang, setLang],
  )
  return <LangContext.Provider value={value}>{children}</LangContext.Provider>
}

export function useErrorText() {
  const { t } = useLang()
  return useCallback((e: unknown) => errorText(t, e), [t])
}
