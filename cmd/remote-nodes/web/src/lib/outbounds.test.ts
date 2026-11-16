import { describe, expect, it } from 'vitest'
import type { Lang } from '../i18n/lang'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import type { Outbound, OutboundState } from './api'
import { checkUrlProblem, inUse, kindText, noteText, restartsBridge, routeChoices, routeName, serverText, stateOf, transportText, trafficText, usersOf, usersText } from './outbounds'
import { makeFormat } from './format'

const speak = (lang: Lang) => ({ lang, t: (key: MsgKey, params?: Params) => translate(lang, key, params), fmt: makeFormat(lang) })
const en = speak('en')
const fa = speak('fa')
// Intl puts direction marks around Persian lists; they aren't part of what the page shows
const plain = (s: string) => s.replace(/[‎‏‪-‮⁦-⁩]/g, '')

const idle: OutboundState = { status: 'idle', delayMs: 0, error: '', onFallback: false, lastTryAt: null, lastSeenAt: null, bytesUp: 0, bytesDown: 0 }

const outbound = (o: Partial<Outbound> = {}): Outbound => ({
  id: 1,
  name: 'Frankfurt',
  fallback: false,
  createdAt: '2026-11-18T10:00:00Z',
  protocol: 'vless',
  host: 'de.example.com',
  port: 443,
  username: '',
  method: '',
  flow: '',
  network: 'tcp',
  path: '',
  hostHeader: '',
  mode: '',
  security: 'reality',
  serverName: 'www.example.com',
  fingerprint: 'chrome',
  alpn: [],
  publicKey: 'k',
  shortId: '',
  spiderX: '',
  secretHint: 'b831…0811',
  usedBy: { nodes: [], router: false },
  state: idle,
  ...o,
})

describe('kindText', () => {
  it('names the protocol, the transport and the security, in a few words', () => {
    expect(kindText({ protocol: 'vless', network: 'tcp', security: 'reality' })).toBe('VLESS · TCP · REALITY')
    expect(kindText({ protocol: 'vmess', network: 'ws', security: 'tls' })).toBe('VMess · WebSocket · TLS')
    expect(kindText({ protocol: 'trojan', network: 'xhttp', security: 'tls' })).toBe('Trojan · XHTTP · TLS')
    expect(kindText({ protocol: 'vless', network: 'httpupgrade', security: 'none' })).toBe('VLESS · HTTPUpgrade')
  })

  it('leaves the transport out where there is only plain TCP', () => {
    expect(kindText({ protocol: 'shadowsocks', network: 'tcp', security: 'none' })).toBe('Shadowsocks')
    expect(kindText({ protocol: 'socks', network: 'tcp', security: 'none' })).toBe('SOCKS')
    expect(kindText({ protocol: 'http', network: 'tcp', security: 'tls' })).toBe('HTTP · TLS')
  })
})

describe('serverText and transportText', () => {
  it('writes host:port, with an IPv6 address in brackets', () => {
    expect(serverText('de.example.com', 443)).toBe('de.example.com:443')
    expect(serverText('2001:db8::1', 8443)).toBe('[2001:db8::1]:8443')
  })

  it("says the transport with its mode and path when it has them", () => {
    expect(transportText({ network: 'tcp', mode: '', path: '' })).toBe('TCP')
    expect(transportText({ network: 'xhttp', mode: 'packet-up', path: '/x' })).toBe('XHTTP · packet-up · /x')
  })
})

describe('stateOf', () => {
  it('words every state, in both languages', () => {
    const cases: [Partial<OutboundState>, string, string, string][] = [
      [{ status: 'up', delayMs: 180 }, 'up', 'Up · 180 ms', 'برقرار · ۱۸۰ میلی‌ثانیه'],
      [{ status: 'up', delayMs: 0 }, 'up', 'Up', 'برقرار'],
      [{ status: 'down', error: 'connection refused' }, 'down', 'Down', 'قطع'],
      [{ status: 'down', error: 'i/o timeout', onFallback: true }, 'fallback', "Down · on the router's VPN", 'قطع · روی وی پی ان روتر'],
      [{ status: 'idle' }, 'idle', 'Not in use', 'استفاده نمی‌شود'],
      [{ status: 'unknown' }, 'unknown', 'Not checked yet', 'هنوز بررسی نشده'],
    ]
    for (const [state, key, english, persian] of cases) {
      const o = outbound({ state: { ...idle, ...state } })
      expect(stateOf(o, en.t)).toMatchObject({ key, label: english })
      expect(stateOf(o, fa.t).label).toBe(persian)
    }
  })

  it("keeps the core's words for why it is down, and the tone says how bad it is", () => {
    expect(stateOf(outbound({ state: { ...idle, status: 'down', error: 'connection refused' } }), en.t)).toMatchObject({ tone: 'danger', detail: 'connection refused' })
    expect(stateOf(outbound({ state: { ...idle, status: 'down', error: 'timeout', onFallback: true } }), en.t)).toMatchObject({ tone: 'warn', detail: 'timeout' })
    expect(stateOf(outbound({ state: { ...idle, status: 'up', delayMs: 9 } }), en.t)).toMatchObject({ tone: 'online', detail: '' })
  })
})

describe('who uses it', () => {
  const nodes = [
    { id: 1, name: 'Node 1' },
    { id: 4, name: 'Berlin' },
  ]

  it('names This router first, then the nodes, and a node it does not know by its number', () => {
    const o = outbound({ usedBy: { nodes: [4, 1, 9], router: true } })
    expect(inUse(o)).toBe(true)
    expect(usersOf(o, nodes, en.t)).toEqual(['This router', 'Berlin', 'Node 1', 'Node 9'])
    expect(plain(usersText(en.t, 'en', usersOf(o, nodes, en.t)))).toBe('This router, Berlin, Node 1 and Node 9')
    expect(plain(usersText(fa.t, 'fa', usersOf(outbound({ usedBy: { nodes: [1], router: true } }), nodes, fa.t)))).toBe('این روتر و Node 1')
  })

  it('says nobody when nothing leaves through it', () => {
    const o = outbound()
    expect(inUse(o)).toBe(false)
    expect(usersText(en.t, 'en', usersOf(o, nodes, en.t))).toBe('None')
    expect(usersText(fa.t, 'fa', usersOf(o, nodes, fa.t))).toBe('هیچ‌کدام')
  })

  it('says who uses one that is in use, with the verb that fits the number', () => {
    expect(en.t('ob.inUse.text', { users: 'Node 1', n: 1 })).toMatch(/^Node 1 leaves through it\./)
    expect(en.t('ob.inUse.text', { users: 'Node 1 and This router', n: 2 })).toMatch(/^Node 1 and This router leave through it\./)
    expect(fa.t('ob.inUse.text', { users: 'Node 1', n: 1 })).toContain('Node 1')
  })
})

describe('trafficText', () => {
  it('adds both ways up, or shows a dash before anything went through', () => {
    expect(trafficText(en.fmt, outbound({ state: { ...idle, bytesUp: 1.4e9, bytesDown: 6.2e9 } }))).toBe('7.6 GB')
    expect(trafficText(fa.fmt, outbound({ state: { ...idle, bytesUp: 1.4e9, bytesDown: 6.2e9 } }))).toBe('۷٫۶ گیگابایت')
    expect(trafficText(en.fmt, outbound())).toBe('–')
  })
})

describe('noteText', () => {
  it('words refusals and notices in both languages, with what is not carried named', () => {
    expect(noteText(en.t, { code: 'transport', name: 'gRPC' })).toBe("The gRPC transport isn't carried here. Use TCP, WebSocket, HTTPUpgrade or XHTTP.")
    expect(plain(noteText(fa.t, { code: 'transport', name: 'gRPC' }))).toBe('روش انتقال gRPC اینجا پشتیبانی نمی‌شود. از TCP، WebSocket، HTTPUpgrade یا XHTTP استفاده کنید.')
    expect(noteText(en.t, { code: 'insecure' })).toBe('It turns certificate checks off. They stay on here.')
    expect(noteText(en.t, { code: 'vmess_cipher', name: 'zero' })).toBe('Its VMess cipher, zero, is read as auto.')
    expect(noteText(fa.t, { code: 'web_address' })).toContain('پراکسی HTTP')
  })

  it('has words for every refusal and notice the parser can give', async () => {
    const src = await import('./link-parser?raw').then((m: { default: string }) => m.default)
    const block = (name: string) => src.slice(src.indexOf(`export type ${name}`), src.indexOf('\n\n', src.indexOf(`export type ${name}`)))
    const codes = [...block('ReasonCode').matchAll(/'(\w+)'/g), ...block('NoticeCode').matchAll(/'(\w+)'/g)].map((m) => m[1])
    expect(codes.length).toBeGreaterThan(30)
    for (const code of codes) {
      for (const lang of [en, fa]) {
        const text = noteText(lang.t, { code: code as never, name: 'X' })
        expect(text, `${lang.lang} ${code}`).not.toMatch(/^ob\./)
      }
    }
  })
})

describe('restartsBridge', () => {
  const used = outbound({ usedBy: { nodes: [1], router: false } })
  it('is true for new settings or a new fallback on one in use, and never for a new name', () => {
    expect(restartsBridge(used, { name: 'Other' })).toBe(false)
    expect(restartsBridge(used, { fallback: true })).toBe(true)
    expect(restartsBridge(used, { fallback: false })).toBe(false)
    expect(restartsBridge(used, { settings: { ...used, id: 'x', password: '' } })).toBe(true)
    expect(restartsBridge(outbound(), { fallback: true, settings: { ...used, id: 'x', password: '' } })).toBe(false)
  })
})

describe('checkUrlProblem', () => {
  it('takes an http or https address with a host, and an empty one for the default', () => {
    for (const ok of ['', 'https://www.google.com/generate_204', 'http://cp.cloudflare.com/', 'https://203.0.113.4:8443/x?y=1']) expect(checkUrlProblem(ok), ok).toBeNull()
    for (const bad of ['ftp://x.example.com/', 'www.example.com', 'https://', 'https://u:p@x.example.com/', 'https://x.example.com/#a', 'https://x.example.com/a b', `https://x.example.com/${'a'.repeat(260)}`])
      expect(checkUrlProblem(bad), bad).toBe('invalid')
  })
})

describe('Leaves through', () => {
  const list = [outbound({ id: 3, name: 'Paris', protocol: 'vmess', network: 'ws', security: 'tls' }), outbound({ id: 1, name: 'Amsterdam' })]

  it("offers the router's VPN first, then the outbounds by name with their kind", () => {
    expect(routeChoices(list, en.t, 'en')).toEqual([
      { id: 0, label: "Router's VPN" },
      { id: 1, label: 'Amsterdam · VLESS · TCP · REALITY' },
      { id: 3, label: 'Paris · VMess · WebSocket · TLS' },
    ])
    expect(routeChoices([], fa.t, 'fa')).toEqual([{ id: 0, label: 'وی پی ان روتر' }])
  })

  it('names where traffic leaves: the VPN, an outbound, one that was removed, or nothing yet while loading', () => {
    expect(routeName(en.t, 0, list)).toBe("Router's VPN")
    expect(routeName(en.t, undefined, null)).toBe("Router's VPN")
    expect(routeName(en.t, 3, list)).toBe('Paris')
    expect(routeName(en.t, 7, list)).toBe('an outbound that was removed')
    expect(routeName(en.t, 7, null)).toBe('…')
  })
})
