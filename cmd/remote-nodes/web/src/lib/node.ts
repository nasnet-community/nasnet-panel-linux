import type { MsgKey, T } from '../i18n'
import type { Node, Traffic } from './api'

/** Dotted version numbers compared part by part: "26.10.9" is older than "26.10.21". A "v" and a "-rc1" are ignored. */
export function cmpVersion(a: string, b: string): -1 | 0 | 1 {
  const parts = (v: string) =>
    v
      .replace(/^v/i, '')
      .split(/[-+]/)[0]
      .split('.')
      .map((p) => parseInt(p, 10) || 0)
  const x = parts(a)
  const y = parts(b)
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] ?? 0) - (y[i] ?? 0)
    if (d) return d < 0 ? -1 : 1
  }
  return 0
}

/** What a version is next to the one this plugin installs. */
export const versionRelation = (have: string, want: string): 'older' | 'same' | 'newer' =>
  ['older', 'same', 'newer'][cmpVersion(have, want) + 1] as 'older' | 'same' | 'newer'

/** Connections a restart cuts off: those subscriptions have open now, or the tunnel's count when the agent can't say. */
export const openConnections = (n: Node) => (n.usage.live ? n.usage.connections : n.tunnel.streams)

/** How many days before its end the hub renews a node's certificate. */
export const renewLeadDays = (cert: Node['cert']) => Math.round((Date.parse(cert.notAfter) - Date.parse(cert.renewsAt)) / 86400e3)

/** A certificate with this many days left or fewer means the hub's renewal didn't happen. */
export const CERT_SOON_DAYS = 14

// --- events ----------------------------------------------------------------------

// what the hub reports in a node's event list, by kind
const EVENT_KEYS = [
  'tunnel_up',
  'tunnel_down',
  'node_unreachable',
  'node_back',
  'agent_updated',
  'xray_changed',
  'cert_renewed',
  'subscription_on',
  'subscription_off',
  'keys_rotated',
]

/** An event's kind ("cert_renewed") as the words for it; a kind the page doesn't know shows as itself, spaced out. */
export function eventLabel(t: T, kind: string): string {
  if (EVENT_KEYS.includes(kind)) return t(`event.${kind}` as MsgKey)
  const spaced = kind.replace(/_/g, ' ').trim()
  return spaced.charAt(0).toUpperCase() + spaced.slice(1)
}

// --- the traffic chart -----------------------------------------------------------

/** The chart covers the last 15 minutes. */
export const CHART_WINDOW = 15 * 60e3

/** The smallest of 1, 2 or 5 times a power of ten that is at least `v`, and at least `floor`, so a quiet line doesn't fill the chart. */
export function niceMax(v: number, floor = 100_000): number {
  const top = Math.max(v, floor)
  const p = 10 ** Math.floor(Math.log10(top))
  return ([1, 2, 5, 10].find((m) => m * p >= top) ?? 10) * p
}

export interface Plot {
  /** the top of the y axis, in bytes per second */
  top: number
  /** SVG path data: the download line, the area under it, the upload line */
  down: string
  area: string
  up: string
  /** two samples at least, so there is a line to draw */
  drawn: boolean
  /** the newest sample, and the highest download in view */
  last: { up: number; down: number } | null
  peak: number
}

/**
 * Turns a node's samples into paths in a `w` by `h` box: the newest 15 minutes up to `now`,
 * oldest at the left. One sample before the window is kept so the line starts at the edge.
 */
export function plot(traffic: Traffic, now: number, w: number, h: number): Plot {
  const start = now - CHART_WINDOW
  // an empty list can reach the page as null
  const samples = (traffic.samples ?? [])
    .map((s) => ({ ...s, ms: Date.parse(s.at) }))
    .filter((s) => s.ms >= start - traffic.stepSec * 1000)
    .sort((a, b) => a.ms - b.ms)
  const top = niceMax(Math.max(0, ...samples.map((s) => Math.max(s.up, s.down))))
  const x = (ms: number) => Math.max(0, Math.min(w, ((ms - start) / CHART_WINDOW) * w))
  const y = (v: number) => h - (v / top) * h
  const line = (k: 'up' | 'down') => samples.map((s, i) => `${i ? 'L' : 'M'}${x(s.ms).toFixed(1)},${y(s[k]).toFixed(1)}`).join('')
  const down = line('down')
  const first = samples[0]
  const last = samples[samples.length - 1]
  return {
    top,
    down,
    area: last ? `${down}L${x(last.ms).toFixed(1)},${h}L${x(first.ms).toFixed(1)},${h}Z` : '',
    up: line('up'),
    drawn: samples.length > 1,
    last: last ? { up: last.up, down: last.down } : null,
    peak: Math.max(0, ...samples.map((s) => s.down)),
  }
}
