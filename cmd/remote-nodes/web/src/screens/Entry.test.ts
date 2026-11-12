import { describe, expect, it } from 'vitest'
import { translate } from '../i18n/translate'
import type { Lang } from '../i18n/lang'
import type { Entry } from '../lib/api'
import { entryAttention, entryChips, routerOSTooOld } from './Entry'

const entry = (o: Partial<Entry> = {}): Entry => ({
  on: true,
  domestic: 'online',
  vpn: { state: 'online', kind: 'wireguard', label: 'WireGuard' },
  bridge: 'running',
  routerOS: { version: '7.24.5', min: '7.20', ok: true },
  routes: [],
  checkedAt: '2026-10-30T12:00:00Z',
  stale: false,
  ...o,
})

const t = (lang: Lang) => (k: Parameters<typeof translate>[1], p?: Record<string, string | number>) => translate(lang, k, p)
const chips = (e: Entry, lang: Lang = 'en') => entryChips(e, t(lang)).map((c) => `${c.label}: ${c.value} (${c.tone})`)
const titles = (e: Entry, lang: Lang = 'en') => entryAttention(e, t(lang)).map((i) => i.title)

describe('the entry chips', () => {
  it('show what entry rests on when it works', () => {
    expect(chips(entry())).toEqual(['Domestic link: online (online)', 'VPN: WireGuard (online)', "Router's Xray: running (online)"])
  })

  it('tell a missing part from one that is down, and a bridge that waits from one that runs', () => {
    expect(chips(entry({ on: false, domestic: 'missing', vpn: { state: 'missing', kind: '', label: '' }, bridge: 'stopped' }))).toEqual([
      'Domestic link: missing (warn)',
      'VPN: missing (warn)',
      "Router's Xray: stopped (idle)",
    ])
    expect(chips(entry({ on: false, domestic: 'offline', vpn: { state: 'offline', kind: 'wireguard', label: 'WireGuard' }, bridge: 'waiting' }))).toEqual([
      'Domestic link: offline (danger)',
      'VPN: offline (danger)',
      "Router's Xray: waiting (idle)",
    ])
  })

  it('name the kind of VPN, or what the router calls it when it is not one the page knows', () => {
    expect(chips(entry({ vpn: { state: 'online', kind: 'l2tp', label: 'x' } }))[1]).toBe('VPN: L2TP (online)')
    expect(chips(entry({ vpn: { state: 'online', kind: 'openvpn', label: 'x' } }))[1]).toBe('VPN: OpenVPN (online)')
    expect(chips(entry({ vpn: { state: 'online', kind: 'other', label: 'ikev2-home' } }))[1]).toBe('VPN: ikev2-home (online)')
    expect(chips(entry({ vpn: { state: 'online', kind: 'other', label: '' } }))[1]).toBe('VPN: online (online)')
  })

  it('are in Persian too', () => {
    expect(chips(entry(), 'fa')).toEqual(['آپ‌لینک داخلی: آنلاین (online)', 'وی پی ان: وایرگارد (online)', 'Xray روتر: در حال اجرا (online)'])
  })
})

describe('what needs attention', () => {
  it('is nothing while entry works', () => {
    expect(entryAttention(entry(), t('en'))).toEqual([])
  })

  it('is one item for each thing that is missing, with a way to the setup', () => {
    const off = entry({ on: false, domestic: 'missing', vpn: { state: 'missing', kind: '', label: '' }, bridge: 'stopped' })
    expect(titles(off)).toEqual(['This router has no Domestic link', 'This router has no VPN'])
    for (const item of entryAttention(off, t('en'))) expect(item.acts).toEqual([{ label: 'Open setup', href: '/' }])
    expect(titles(off, 'fa')).toEqual(['این روتر آپ‌لینک داخلی ندارد', 'این روتر وی پی ان ندارد'])
  })

  // a hub that has not heard from the router yet sends no version and ok: false
  it('says nothing of RouterOS before the router has reported its version', () => {
    const unheard = entry({ on: false, routerOS: { version: '', min: '7.20', ok: false }, checkedAt: null, domestic: 'missing', vpn: { state: 'missing', kind: '', label: '' }, bridge: 'stopped' })
    expect(titles(unheard)).toEqual(['This router has no Domestic link', 'This router has no VPN'])
    expect(routerOSTooOld(unheard)).toBe(false)
    expect(routerOSTooOld(entry({ routerOS: { version: '7.9', min: '7.20', ok: false } }))).toBe(true)
    expect(routerOSTooOld(entry())).toBe(false)
  })

  it('is an item for a part that is down, and one for a RouterOS that is too old', () => {
    expect(titles(entry({ on: false, domestic: 'offline' }))).toEqual(['The Domestic link is offline'])
    expect(titles(entry({ on: false, vpn: { state: 'offline', kind: 'wireguard', label: '' } }))).toEqual(['The VPN is offline'])
    const old = entryAttention(entry({ on: false, routerOS: { version: '7.9', min: '7.20', ok: false } }), t('en'))
    expect(old).toHaveLength(1)
    expect(old[0].title).toBe('RouterOS 7.9 is too old')
    expect(old[0].detail).toContain('7.20')
    // it comes first: the rest can't be fixed on a router that can't run the plugin
    expect(titles(entry({ on: false, domestic: 'missing', routerOS: { version: '7.9', min: '7.20', ok: false } }))[0]).toBe('RouterOS 7.9 is too old')
  })
})
