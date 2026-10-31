import { describe, expect, it } from 'vitest'
import { ApiError } from '../lib/api'
import { en } from './en'
import { fa } from './fa'
import { detectLang, localizeDigits, type Message } from './lang'
import { errorText } from './errors'
import { translate, type MsgKey } from './translate'

const keys = Object.keys(en) as MsgKey[]
const text = (m: Message) => (typeof m === 'string' ? [m] : [m.one, m.other].filter((s): s is string => s !== undefined))
const holes = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort()

describe('translate', () => {
  it('fills in parameters', () => {
    expect(translate('en', 'attn.routerOS.title', { version: '7.9' })).toBe('RouterOS 7.9 is too old')
    expect(translate('en', 'entry.checked', { ago: '2 minutes ago' })).toBe('Checked 2 minutes ago')
  })

  it('writes a number in the language\'s digits and leaves a string as it is', () => {
    expect(translate('en', 'time.secondsAgo', { s: 12 })).toBe('12 s ago')
    expect(translate('fa', 'time.secondsAgo', { s: 12 })).toBe('۱۲ ثانیه پیش')
    expect(translate('fa', 'attn.routerOS.title', { version: '7.9' })).toBe('RouterOS 7.9 قدیمی است')
    expect(translate('fa', 'time.secondsAgo', { s: '12' })).toBe('12 ثانیه پیش')
  })

  it('keeps a placeholder it has no value for', () => {
    expect(translate('en', 'entry.checked')).toBe('Checked {ago}')
  })

  it('falls back to English, then to the key', () => {
    expect(translate('fa', 'no.such.key' as MsgKey)).toBe('no.such.key')
  })
})

describe('plurals', () => {
  // translate() reads dictionaries, so the plural rules are tried on a message of their own
  const dicts = en as Record<string, Message>
  it('picks the form by the number, or by the parameter the message names', () => {
    dicts['test.plain'] = { one: '{n} node', other: '{n} nodes' }
    dicts['test.by'] = { by: 'max', one: '{n} of {max} node', other: '{n} of {max} nodes' }
    try {
      expect(translate('en', 'test.plain' as MsgKey, { n: 1 })).toBe('1 node')
      expect(translate('en', 'test.plain' as MsgKey, { n: 0 })).toBe('0 nodes')
      expect(translate('en', 'test.plain' as MsgKey, { n: 3 })).toBe('3 nodes')
      expect(translate('en', 'test.by' as MsgKey, { n: 0, max: 1 })).toBe('0 of 1 node')
      expect(translate('en', 'test.by' as MsgKey, { n: 1, max: 3 })).toBe('1 of 3 nodes')
      // Persian falls back to the English message here, and writes the numbers in its own digits
      expect(translate('fa', 'test.plain' as MsgKey, { n: 2 })).toBe('۲ nodes')
    } finally {
      delete dicts['test.plain']
      delete dicts['test.by']
    }
  })
})

describe('dictionaries', () => {
  it('give Persian every key English has, and nothing more', () => {
    expect(Object.keys(fa).sort()).toEqual([...keys].sort())
  })

  it('keep the same placeholders in both languages', () => {
    for (const key of keys) {
      const [en0] = text(en[key])
      for (const f of text(fa[key])) expect(holes(f), key).toEqual(holes(en0))
      for (const e of text(en[key])) expect(holes(e), key).toEqual(holes(en0))
    }
  })

  it('translate every message, apart from names that stay Latin', () => {
    const same = new Set(['vpn.l2tp', 'vpn.openvpn'])
    for (const key of keys) {
      if (!same.has(key)) expect(text(fa[key]).join(), key).not.toBe(text(en[key]).join())
      for (const s of text(fa[key])) expect(s.trim(), key).not.toBe('')
    }
    // the exceptions are real: those two are written the same on purpose
    for (const key of same) expect(fa[key as MsgKey]).toBe(en[key as MsgKey])
  })

  it('write Persian digits, never ASCII ones, apart from product names', () => {
    for (const key of keys) {
      for (const s of text(fa[key])) {
        const prose = s.replace(/L2TP|OpenVPN|RouterOS|Remote Nodes|\{\w+\}/g, '')
        expect(prose, key).not.toMatch(/[0-9]/)
      }
    }
  })

  it('set a path in a Persian sentence off as a left-to-right run, or its slashes land in the wrong places', () => {
    // a path that opens a run of right-to-left text comes out as "usr/local/bin/xray/" unless it is isolated
    const paths = /(?<![\w.:/])\/[a-z][\w.-]*(?:\/[\w.-]+)+/g
    let seen = 0
    for (const key of keys) {
      for (const s of text(fa[key])) {
        for (const m of s.matchAll(paths)) {
          seen++
          const at = m.index ?? 0
          expect(s[at - 1], `${key}: ${m[0]}`).toBe('⁦')
          expect(s[at + m[0].length], `${key}: ${m[0]}`).toBe('⁩')
        }
      }
    }
    // the check is looking at something
    expect(seen).toBeGreaterThanOrEqual(2)
  })
})

describe('the language people see', () => {
  const store = (v: Record<string, string>) => ({ getItem: (k: string) => v[k] ?? null })

  it("takes the saved choice, then the panel's, then the browser's", () => {
    expect(detectLang(store({ 'remote-nodes.language': 'fa', 'nasnet-panel.language': 'en' }), 'en-US')).toBe('fa')
    expect(detectLang(store({ 'nasnet-panel.language': 'fa' }), 'en-US')).toBe('fa')
    expect(detectLang(store({}), 'fa-IR')).toBe('fa')
    expect(detectLang(store({}), 'de-DE')).toBe('en')
    expect(detectLang(store({ 'remote-nodes.language': 'xx' }), '')).toBe('en')
  })

  it('works when storage is blocked', () => {
    const blocked = {
      getItem: () => {
        throw new Error('denied')
      },
    }
    expect(detectLang(blocked, 'fa')).toBe('fa')
    expect(detectLang(null, 'en')).toBe('en')
  })

  it('turns the digits of a string into Persian ones only for Persian', () => {
    expect(localizeDigits('1:02:09', 'fa')).toBe('۱:۰۲:۰۹')
    expect(localizeDigits('1:02:09', 'en')).toBe('1:02:09')
  })
})

describe('errorText', () => {
  const t = (key: MsgKey) => translate('fa', key)
  const err = (code: string, message = 'English from the server') => new ApiError(message, 409, { code, error: message })

  it("uses the page's wording for a code it knows", () => {
    expect(errorText(t, err('node_limit'))).toBe(translate('fa', 'error.node_limit'))
    expect(errorText((k) => translate('en', k), err('busy'))).toBe('Another job is already running on this node.')
  })

  it("shows the server's sentence for other codes and plain errors", () => {
    expect(errorText(t, err('bad_request', 'give the server address'))).toBe('give the server address')
    // a name over the limit gets past the field only by a paste or a script; the hub's sentence says what is wrong
    expect(errorText(t, err('bad_request', 'The name can have at most 64 characters.'))).toBe('The name can have at most 64 characters.')
    expect(errorText(t, new Error('network down'))).toBe('network down')
    expect(errorText(t, 'oops')).toBe('oops')
  })
})
