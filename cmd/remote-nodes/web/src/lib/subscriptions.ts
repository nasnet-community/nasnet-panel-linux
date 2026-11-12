import type { Lang, MsgKey, T } from '../i18n'
import { LANGS } from '../i18n/lang'
import type { LocalState, Node, NodeSubscription, PlaceId, Subscription } from './api'
import { DAY, type Format } from './format'
import { unreachable } from './health'

/** A place a subscription can be on, as the subscription screens need it: its name, and whether a switch on it can take effect now. */
export interface Place {
  /** a node's id, or 'router' for the router's own entry */
  id: PlaceId
  name: string
  /** the router can't reach it; for the router's own entry, it isn't working right now */
  unreachable: boolean
  /** its install is still running */
  installing: boolean
}

/** A node as a place. */
export type NodeInfo = Place & { id: number }

/** The router's own entry as a place, while it is switched on. Off, it is no place: null. */
export const routerPlace = (t: T, local: { enabled: boolean; state: LocalState } | null | undefined): Place | null =>
  local?.enabled ? { id: 'router', name: t('router.name'), unreachable: local.state !== 'working', installing: false } : null

/** Every place a subscription can be on: the router's own entry first, then the nodes. */
export const placesOf = (router: Place | null, nodes: NodeInfo[]): Place[] => (router ? [router, ...nodes] : nodes)

/** Whether a subscription is switched on for a place. */
export const enabledAt = (s: Pick<Subscription, 'nodes' | 'local'>, place: PlaceId): boolean =>
  place === 'router' ? s.local?.enabled === true : s.nodes.find((n) => n.nodeId === place)?.enabled === true

/** Whether a subscription has a connection on a place right now, as far as the hub knows. */
export const onlineAt = (s: Pick<Subscription, 'nodes' | 'local'>, place: PlaceId): boolean =>
  place === 'router' ? s.local?.online === true : s.nodes.find((n) => n.nodeId === place)?.online === true

/** The nodes a subscription can be on, or soon can: installed ones and those being installed. A dead install is left out. */
export const nodeInfos = (nodes: Node[]): NodeInfo[] =>
  nodes
    .filter((n) => n.registered || n.installing)
    .map((n) => ({ id: n.id, name: n.name, unreachable: unreachable(n), installing: !!n.installing }))

/** The places with a column of switches: those whose install is done. */
export const readyNodes = <P extends Place>(nodes: P[]) => nodes.filter((n) => !n.installing)

/** The places a new subscription starts on: every one that works now. */
export const defaultNodeIds = <P extends Place>(nodes: P[]): P['id'][] => nodes.filter((n) => !n.installing && !n.unreachable).map((n) => n.id)

const collators = new Map<Lang, Intl.Collator>()

/** By name, the way the language sorts: Persian names in Persian order, "Node 2" before "Node 10", case ignored. */
export function sortByName<Item extends { name: string }>(list: Item[], lang: Lang): Item[] {
  let collator = collators.get(lang)
  if (!collator) collators.set(lang, (collator = new Intl.Collator(LANGS[lang].intl, { numeric: true, sensitivity: 'accent' })))
  return [...list].sort((a, b) => collator.compare(a.name, b.name))
}

/** "Node 1", "Node 1 and Node 2": names joined the way the language does it. */
export const nameList = (lang: Lang, names: string[]) => new Intl.ListFormat(LANGS[lang].intl, { style: 'long', type: 'conjunction' }).format(names)

/** "on Node 1", "on Node 1 and Node 2", "on 3 nodes"; `places`: "on 3 places", when the router's own entry is one of them. */
export function onNodes(t: T, lang: Lang, names: string[], places = false): string {
  return t('subs.onNodes', { nodes: names.length <= 2 ? nameList(lang, names) : t(places ? 'subs.placeCount' : 'subs.nodeCount', { n: names.length }) })
}

/** "last seen 3 hours ago", "last seen yesterday" (where a plain "1 day ago" reads stiffly), or "never connected". */
export function lastSeen(t: T, fmt: Format, iso: string | null, now = Date.now()): string {
  if (!iso) return t('subs.sub.never')
  const age = now - Date.parse(iso)
  // the same span that relative() calls "1 day ago"
  if (age >= DAY && age < 1.5 * DAY) return t('subs.sub.yesterday')
  return t('subs.sub.lastSeen', { ago: fmt.relative(iso, now) })
}

export type StatusKey = 'online' | 'active' | 'off' | 'unknown' | 'limit' | 'expired'

export interface Status {
  key: StatusKey
  /** the badge's word */
  label: string
  /** the line under it */
  sub: string
}

const WORD: Record<StatusKey, MsgKey> = {
  online: 'subs.status.online',
  active: 'subs.status.active',
  off: 'subs.status.off',
  unknown: 'subs.status.unknown',
  limit: 'subs.status.limit',
  expired: 'subs.status.expired',
}

/**
 * Where a subscription stands across all places: online (and where), active (switched on,
 * nobody connected), off everywhere, or unknown while its places can't be reached.
 */
export function subscriptionStatus(s: Subscription, places: Place[], t: T, fmt: Format, lang: Lang, now = Date.now()): Status {
  const status = (key: StatusKey, sub: string): Status => ({ key, label: t(WORD[key]), sub })
  // the router's own entry is among the places while it is switched on
  const router = s.local !== null
  // stopped by the hub: it says more than "off", and the switches are still as they were
  if (s.stopped === 'limit') return status('limit', t('subs.sub.limitUsed', { used: fmt.bytes(s.trafficUsed), limit: fmt.bytes(s.trafficLimit) }))
  if (s.stopped === 'expired') return status('expired', s.expiresAt ? t('subs.sub.expiredOn', { date: fmt.date(s.expiresAt) }) : '')
  if (!s.nodes.some((n) => n.enabled) && !s.local?.enabled) return status('off', t(router ? 'subs.sub.offEverywhere' : 'subs.sub.offEvery'))
  if (s.online === true) {
    const where = places.filter((p) => onlineAt(s, p.id)).map((p) => p.name)
    return status('online', where.length ? onNodes(t, lang, where, router) : '')
  }
  if (s.online === false) return status('active', lastSeen(t, fmt, s.lastSeenAt, now))
  return status('unknown', s.lastSeenAt ? t('subs.sub.nodeDown') : t('subs.sub.never'))
}

/**
 * The same for one place's list: off here, online (with its connections), active, or unknown while the
 * place isn't answering. `router`: the list is the router's own entry's, and not a node's.
 */
export function nodeSubscriptionStatus(s: NodeSubscription, t: T, fmt: Format, now = Date.now(), router = false): Status {
  const status = (key: StatusKey, sub: string): Status => ({ key, label: t(WORD[key]), sub })
  if (!s.enabled) return status('off', t(router ? 'subs.sub.offRouter' : 'subs.sub.offHere'))
  if (s.stopped === 'limit' || s.stopped === 'expired') return status(s.stopped, '')
  if (s.online === true) return status('online', t('subs.sub.connections', { n: s.connections ?? 0 }))
  if (s.online === false) return status('active', lastSeen(t, fmt, s.lastSeenAt, now))
  return status('unknown', s.lastSeenAt ? t(router ? 'subs.sub.routerDown' : 'subs.sub.nodeDown') : t('subs.sub.never'))
}

/**
 * What a toast says after a switch. A place that isn't answering takes the change when it is back, and
 * the toast says so, in the words for a node or (`router`) for the router's own entry.
 */
export function switchedText(t: T, on: boolean, down: boolean, name: string, node: string, router = false): string {
  if (down && router) return t(on ? 'subs.toast.onLaterRouter' : 'subs.toast.offLaterRouter', { name })
  const key = on ? (down ? 'subs.toast.onLater' : 'subs.toast.on') : down ? 'subs.toast.offLater' : 'subs.toast.off'
  return t(key, { name, node })
}

type Counters = Pick<NodeSubscription, 'todayUp' | 'todayDown' | 'monthUp' | 'monthDown'>

/** What a list of subscriptions moved today and over 30 days, up and down. */
export function totals(list: Counters[]): Counters {
  const sum = (pick: (c: Counters) => number) => list.reduce((n, c) => n + pick(c), 0)
  return { todayUp: sum((c) => c.todayUp), todayDown: sum((c) => c.todayDown), monthUp: sum((c) => c.monthUp), monthDown: sum((c) => c.monthDown) }
}

/** A traffic cell's lines: "1.2 GB" (a table adds "today"), "38 GB in 30 days", and both as one line for phones. */
export function trafficText(t: T, fmt: Format, today: number, month: number) {
  const day = fmt.bytes(today)
  const span = t('subs.traffic.month', { size: fmt.bytes(month) })
  return { today: day, month: span, line: t('subs.traffic.line', { today: day, month: span }) }
}

/** A tile's small line: "↑ 1.0 GB · ↓ 3.1 GB". */
export const upDown = (t: T, fmt: Format, up: number, down: number) => t('subs.tile.updown', { up: fmt.bytes(up), down: fmt.bytes(down) })

/**
 * A link with every non-ASCII character percent-encoded. The hub already sends the name encoded,
 * so this changes nothing there; it keeps a raw name (Persian, say) from reaching the QR code,
 * which would hold the wrong bytes for it.
 */
export const portableLink = (vless: string) =>
  vless.replace(/[^\x00-\x7f]+/g, (run) => {
    try {
      return encodeURIComponent(run)
    } catch {
      // a lone surrogate has no UTF-8 form
      return '%EF%BF%BD'
    }
  })

// --- limits and expiry ------------------------------------------------------------

/** What the page shows sizes in: 1 GB is a billion bytes, as in fmt.bytes. */
const GB = 1e9

/** The largest limit the field takes, in GB. The hub's own largest is 1 PiB. */
export const MAX_LIMIT_GB = 1_000_000

/** A limit in bytes as the field shows it: "50", "0.5", "12.345". */
export const limitText = (bytes: number): string => (bytes > 0 ? String(Number((bytes / GB).toPrecision(6))) : '')

/**
 * What a limit field holds, in bytes: null for an empty field (no limit), undefined when the text is
 * not a number of gigabytes above zero. A comma works as the decimal point.
 */
export function parseLimit(text: string): number | null | undefined {
  const raw = text.trim().replace(',', '.')
  if (raw === '') return null
  if (!/^\d*\.?\d+$|^\d+\.$/.test(raw)) return undefined
  const gb = Number(raw)
  if (!Number.isFinite(gb) || gb <= 0 || gb > MAX_LIMIT_GB) return undefined
  return Math.max(1, Math.round(gb * GB))
}

const pad = (n: number) => String(n).padStart(2, '0')
const dayOf = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`

/** The day an instant falls on in the person's own zone, as a date field holds it: "2027-01-31". */
export const localDay = (iso: string): string => dayOf(new Date(iso))

/** Today's day in the person's zone, for the date field's earliest choice. */
export const today = (now = Date.now()): string => dayOf(new Date(now))

/** A subscription ends with the day it is given: 23:59:59 in the person's zone, as an instant. Empty for no end. */
export function endOfDay(day: string): string | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day)
  if (!m) return null
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), 23, 59, 59)
  // a day that doesn't exist (the 31st of February) rolls over, and is not what was asked
  return dayOf(d) === day ? d.toISOString() : null
}

/** How full a limit is, 0 to 100, rounded down so "100%" means it is reached. */
export const usedPercent = (used: number, limit: number): number => (limit > 0 ? Math.min(100, Math.floor((used / limit) * 100)) : 0)

/** The colour of a limit's bar: calm, then a warning at 80%, then red at the limit. */
export const quotaTone = (pct: number): 'ok' | 'warn' | 'bad' => (pct >= 100 ? 'bad' : pct >= 80 ? 'warn' : 'ok')

/** What the edit dialog needs of a subscription, from either list. */
export type Editable = Pick<Subscription, 'id' | 'name' | 'trafficLimit' | 'trafficUsed' | 'expiresAt' | 'stopped'>

/**
 * A day some days or years after another, as a date field holds it. It counts on from the day given
 * when that day has not passed, so a second "+30 days" adds another 30; from today when it has, or
 * when there is none, so an expired subscription is renewed from now and not from its old date.
 */
export function extendDay(day: string, by: { days?: number; years?: number }, now = Date.now()): string {
  const end = endOfDay(day)
  const base = end !== null && Date.parse(end) > now ? new Date(end) : new Date(now)
  const d = new Date(base.getFullYear() + (by.years ?? 0), base.getMonth(), base.getDate() + (by.days ?? 0))
  // the 29th of February a year on is the 1st of March; the 28th reads as "a year" better
  if (by.years && d.getDate() !== base.getDate()) d.setDate(0)
  return dayOf(d)
}

/** An end date as a table cell shows it: the day, and how far off it is, coloured as it nears. */
export function expiryParts(t: T, fmt: Format, iso: string, now = Date.now()): { date: string; when: string; tone: 'muted' | 'warn' | 'bad' } {
  const left = Date.parse(iso) - now
  if (left <= 0) return { date: fmt.date(iso), when: t('subs.quota.ended', { when: fmt.relative(iso, now) }), tone: 'bad' }
  return { date: fmt.date(iso), when: fmt.relative(iso, now), tone: left <= 7 * DAY ? 'warn' : 'muted' }
}

/** "Expires 5 Dec 2026 · in 12 days", or "Expired 5 Dec 2026"; empty with no end date. */
export function expiryText(t: T, fmt: Format, iso: string | null, now = Date.now()): string {
  if (!iso) return ''
  if (Date.parse(iso) <= now) return t('subs.quota.expiredOn', { date: fmt.date(iso) })
  return t('subs.quota.expiresIn', { date: fmt.date(iso), when: fmt.relative(iso, now) })
}

/** What the two limit fields hold. `limit` is bytes and `expiresAt` an instant; null is "none". A bad field is flagged and read as none. */
export interface QuotaRead {
  limit: number | null
  expiresAt: string | null
  badLimit: boolean
  badDate: boolean
}

/** Reads the traffic limit field (in GB) and the date field. A date has to be a day that hasn't passed. */
export function readQuota(gb: string, day: string, now = Date.now()): QuotaRead {
  const parsed = parseLimit(gb)
  const badLimit = parsed === undefined
  let expiresAt: string | null = null
  let badDate = false
  if (day !== '') {
    expiresAt = endOfDay(day)
    if (expiresAt === null || Date.parse(expiresAt) <= now) {
      expiresAt = null
      badDate = true
    }
  }
  return { limit: parsed ?? null, expiresAt, badLimit, badDate }
}
