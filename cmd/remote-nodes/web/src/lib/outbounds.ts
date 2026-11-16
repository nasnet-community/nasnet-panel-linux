// What the Outbounds tab and "Leaves through" say about an outbound, apart from the components.

import type { Tone } from '../components/ui'
import type { MsgKey, T } from '../i18n'
import type { Lang } from '../i18n/lang'
import type { Outbound, OutboundSettings, OutboundPatch } from './api'
import type { Format } from './format'
import { NETWORK_NAMES, type NoticeCode, type Note, type ReasonCode } from './link-parser'
import { nameList } from './subscriptions'

export const PROTOCOL_NAMES: Record<OutboundSettings['protocol'], string> = {
  vless: 'VLESS',
  vmess: 'VMess',
  trojan: 'Trojan',
  shadowsocks: 'Shadowsocks',
  socks: 'SOCKS',
  http: 'HTTP',
}

const SECURITY_NAMES: Record<OutboundSettings['security'], string> = { none: '', tls: 'TLS', reality: 'REALITY' }

// these run over plain TCP only, so naming the transport says nothing
const TCP_ONLY = new Set(['shadowsocks', 'socks', 'http'])

type Kind = Pick<OutboundSettings, 'protocol' | 'network' | 'security'>

/** "VLESS · TCP · REALITY", "Shadowsocks", "HTTP · TLS": the protocol, then the transport and what secures it. */
export function kindText(o: Kind): string {
  const network = TCP_ONLY.has(o.protocol) ? '' : NETWORK_NAMES[o.network || 'tcp']
  return [PROTOCOL_NAMES[o.protocol] ?? o.protocol, network, SECURITY_NAMES[o.security || 'none']].filter(Boolean).join(' · ')
}

/** The transport alone, for the summary of what a paste said. */
export const transportText = (o: Pick<OutboundSettings, 'network' | 'mode' | 'path'>) =>
  [NETWORK_NAMES[o.network || 'tcp'], o.mode, o.path].filter(Boolean).join(' · ')

/** "de.example.com:443", "[2001:db8::1]:443". */
export const serverText = (host: string, port: number) => `${host.includes(':') ? `[${host}]` : host}:${port}`

export type StateKey = 'up' | 'down' | 'fallback' | 'idle' | 'unknown'

export interface StateView {
  key: StateKey
  tone: Tone
  label: string
  /** the core's words for why it is down, or what being on the fallback means */
  detail: string
}

/** How an outbound is doing, in a badge's words. */
export function stateOf(o: Pick<Outbound, 'state'>, t: T): StateView {
  const s = o.state
  switch (s.status) {
    case 'up':
      return { key: 'up', tone: 'online', label: s.delayMs > 0 ? t('ob.state.upDelay', { delay: t('unit.ms', { n: s.delayMs }) }) : t('ob.state.up'), detail: '' }
    case 'down':
      return s.onFallback
        ? { key: 'fallback', tone: 'warn', label: t('ob.state.fallback'), detail: s.error }
        : { key: 'down', tone: 'danger', label: t('ob.state.down'), detail: s.error }
    case 'idle':
      return { key: 'idle', tone: 'muted', label: t('ob.state.idle'), detail: '' }
    default:
      return { key: 'unknown', tone: 'idle', label: t('ob.state.unknown'), detail: '' }
  }
}

/** A node or This router leaves through it. */
export const inUse = (o: Pick<Outbound, 'usedBy'>) => o.usedBy.nodes.length > 0 || o.usedBy.router

/** Who leaves through it, by name: This router first, then the nodes in the order they are listed. */
export function usersOf(o: Pick<Outbound, 'usedBy'>, nodes: { id: number; name: string }[] | null, t: T): string[] {
  const names = o.usedBy.nodes.map((id) => nodes?.find((n) => n.id === id)?.name ?? t('node.defaultName', { n: id }))
  return o.usedBy.router ? [t('router.name'), ...names] : names
}

/** "Node 1 and This router", or the word for nobody. */
export const usersText = (t: T, lang: Lang, names: string[]) => (names.length ? nameList(lang, names) : t('ob.usedBy.none'))

/** What its traffic adds up to since the bridge started. */
export const trafficOf = (o: Pick<Outbound, 'state'>) => o.state.bytesUp + o.state.bytesDown

/** Bytes both ways, or a dash before anything has gone through. */
export const trafficText = (fmt: Format, o: Pick<Outbound, 'state'>) => (trafficOf(o) > 0 ? fmt.bytes(trafficOf(o)) : '–')

/** A refusal or a notice of the parser, in the page's words. */
export const noteText = (t: T, note: Note<ReasonCode | NoticeCode>) =>
  t(`ob.${['insecure', 'vmess_cipher'].includes(note.code) ? 'note' : 'why'}.${note.code}` as MsgKey, { name: note.name ?? '' })

/**
 * Whether an edit changes the bridge, which restarts it: new settings or a new fallback on an
 * outbound that is in use. A new name doesn't; the bridge knows it by its id.
 */
export function restartsBridge(o: Pick<Outbound, 'usedBy' | 'fallback'>, patch: OutboundPatch): boolean {
  return inUse(o) && (patch.settings !== undefined || (patch.fallback !== undefined && patch.fallback !== o.fallback))
}

/** What is wrong with a health check address, or null: http or https, with a host, at most 255 characters. */
export function checkUrlProblem(url: string): 'invalid' | null {
  const s = url.trim()
  if (!s) return null
  if (s.length > 255 || /\s/.test(s)) return 'invalid'
  try {
    const u = new URL(s)
    if ((u.protocol !== 'http:' && u.protocol !== 'https:') || !u.hostname || u.username || u.password || u.hash) return 'invalid'
  } catch {
    return 'invalid'
  }
  return null
}

/** The choices of "Leaves through": the router's VPN, then the outbounds by name. */
export function routeChoices(list: Outbound[], t: T, lang: Lang): { id: number; label: string }[] {
  const sorted = [...list].sort((a, b) => a.name.localeCompare(b.name, lang))
  return [{ id: 0, label: t('via.vpn') }, ...sorted.map((o) => ({ id: o.id, label: `${o.name} · ${kindText(o)}` }))]
}

/** Where a node or This router leaves through, in words: the router's VPN, an outbound by name, or one that is gone. */
export function routeName(t: T, outboundId: number | undefined, list: Outbound[] | null): string {
  if (!outboundId) return t('via.vpn')
  return list?.find((o) => o.id === outboundId)?.name ?? (list ? t('via.gone') : '…')
}
