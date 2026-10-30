import type { Format } from './format'
import type { Node } from './api'
import type { T } from '../i18n'
import { CERT_SOON_DAYS, versionRelation } from './node'

export type HealthTone = 'online' | 'warn' | 'danger' | 'idle'

/** The router can't reach the node's agent. */
export const unreachable = (n: Node) => n.tunnel.reason === 'agent_down'

/** What can be wrong with a node. The card's badge and the node page's list of notes both read this. */
export type Issue =
  | 'notFinished'
  | 'unreachable'
  | 'xrayMismatch'
  | 'bridgeDown'
  | 'xrayCrash'
  | 'xrayStopped'
  | 'notApplied'
  | 'tunnelDown'
  | 'waiting'
  | 'certExpiring'
  | 'entryOff'

/** Everything wrong with a node, worst first. A node being installed has nothing wrong yet. */
export function issues(n: Node): Issue[] {
  if (n.installing) return []
  if (!n.registered) return ['notFinished']
  if (unreachable(n)) return ['unreachable']
  const a = n.agent
  const out: Issue[] = []
  if (n.tunnel.reason === 'xray_mismatch') out.push('xrayMismatch')
  if (n.tunnel.reason === 'bridge_down') out.push('bridgeDown')
  if (a?.xray.crashLoop) out.push('xrayCrash')
  else if (a && !a.xray.running) out.push('xrayStopped')
  if (a && !a.settingsApplied && a.lastPushError) out.push('notApplied')
  if (n.tunnel.state === 'down') out.push('tunnelDown')
  if (!a) out.push('waiting')
  if (n.cert.validDays <= CERT_SOON_DAYS) out.push('certExpiring')
  // a node that works is not at fault when entry is off, so this one never changes its badge
  if (n.tunnel.reason === 'entry_off') out.push('entryOff')
  return out
}

// A time of day reads "since 07:12", a date reads "since 3 Oct": Persian needs a different word before each.
const sameDay = (iso: string, now: number) => new Date(iso).toDateString() === new Date(now).toDateString()

/** A node's health for its card: a tone, a word, and the reason when it is not fine. Worst first. */
export function health(n: Node, t: T, fmt: Format, now = Date.now()): { tone: HealthTone; label: string; reason: string } {
  const attention = (tone: HealthTone, reason: string) => ({ tone, label: t('node.attention'), reason })
  if (n.installing) return { tone: 'idle', label: t('node.installingBadge'), reason: '' }
  switch (issues(n).find((i) => i !== 'entryOff')) {
    case 'notFinished':
      return { tone: 'danger', label: t('node.notFinished'), reason: t('node.reason.notFinished') }
    case 'unreachable':
      return {
        tone: 'danger',
        label: t('node.unreachable'),
        reason: n.lastSeenAt
          ? t(sameDay(n.lastSeenAt, now) ? 'node.reason.unreachableSince' : 'node.reason.unreachableSinceDay', {
              time: fmt.clockOrDay(n.lastSeenAt, now),
              ago: fmt.relative(n.lastSeenAt, now),
            })
          : t('node.reason.unreachable'),
      }
    case 'xrayMismatch':
      return attention('warn', t('node.reason.xrayMismatch'))
    case 'bridgeDown':
      return attention('warn', t('node.reason.bridgeDown'))
    case 'xrayCrash':
      return attention('danger', t('node.reason.xrayCrash'))
    case 'xrayStopped':
      return attention('warn', t('node.reason.xrayStopped'))
    case 'notApplied':
      return attention('warn', t('node.reason.notApplied'))
    case 'tunnelDown':
      return attention('warn', t('node.reason.tunnelDown'))
    case 'certExpiring':
      return attention(n.cert.validDays <= 3 ? 'danger' : 'warn', certReason(n, t))
    case 'waiting':
      return { tone: 'idle', label: t('node.starting'), reason: t('node.reason.waiting') }
    default:
      return { tone: 'online', label: t('node.working'), reason: '' }
  }
}

const certReason = (n: Node, t: T) => (n.cert.validDays <= 0 ? t('node.reason.certExpired') : t('node.reason.certExpiring', { n: n.cert.validDays }))

// --- the node page's list of notes -------------------------------------------------

/** What a note's button does; the page supplies the handler. */
export type NodeAct = 'updateXray' | 'checkAgain' | 'remove' | 'showJob' | 'openSettings' | 'renewCert'

export interface NodeNote {
  id: string
  tone: 'danger' | 'warn' | 'info'
  title: string
  detail: string
  acts: { id: NodeAct; primary?: boolean }[]
}

/** A hub's phrase as a sentence of its own: capital first, full stop last. */
const sentence = (s: string) => {
  const text = s.charAt(0).toUpperCase() + s.slice(1)
  return /[.!?]$/.test(text) ? text : `${text}.`
}

/**
 * What the node page says is wrong, worst first, each with the one or two things to do about it.
 * `xray` is the version this plugin installs, and the one a node's tunnel waits for.
 */
export function nodeNotes(n: Node, o: { t: T; fmt: Format; xray?: string; now?: number }): NodeNote[] {
  const { t, fmt } = o
  const now = o.now ?? Date.now()
  const note = (id: string, tone: NodeNote['tone'], title: string, detail: string, acts: NodeNote['acts'] = []): NodeNote => ({ id, tone, title, detail, acts })
  if (n.installing) return [note('installing', 'info', t('note.installing.title'), t('note.installing.detail'), [{ id: 'showJob' }])]
  const a = n.agent
  return issues(n).map((issue): NodeNote => {
    switch (issue) {
      case 'notFinished':
        return note(issue, 'danger', t('note.notFinished.title'), t('note.notFinished.detail'), [{ id: 'remove' }])
      case 'unreachable': {
        const seen = n.lastSeenAt ? `${t('note.unreachable.seen', { time: fmt.clockOrDay(n.lastSeenAt, now), ago: fmt.relative(n.lastSeenAt, now) })} ` : ''
        return note(issue, 'danger', t('note.unreachable.title'), seen + t('note.unreachable.detail'), [{ id: 'checkAgain', primary: true }])
      }
      case 'xrayMismatch': {
        const want = o.xray || n.updates.xrayVersion
        const have = n.xrayVersion || a?.xrayVersion || ''
        const newer = versionRelation(have, want) === 'newer'
        return note(issue, 'warn', t('note.mismatch.title'), t(newer ? 'note.mismatch.newer' : 'note.mismatch.older', { have, want }), [{ id: 'updateXray', primary: true }])
      }
      case 'bridgeDown':
        return note(issue, 'warn', t('node.reason.bridgeDown'), t('note.bridgeDown.detail'))
      case 'xrayCrash': {
        const exit = a?.xray.lastExit
        return note(issue, 'danger', t('node.reason.xrayCrash'), `${exit ? `${t('note.lastExit', { exit })} ` : ''}${t('note.xrayCrash.detail')}`)
      }
      case 'xrayStopped': {
        const exit = a?.xray.lastExit
        return note(issue, 'warn', t('node.reason.xrayStopped'), exit ? t('note.lastExit', { exit }) : t('note.xrayStopped.detail'))
      }
      case 'notApplied':
        return note(issue, 'warn', t('node.reason.notApplied'), sentence(a?.lastPushError ?? ''))
      case 'tunnelDown':
        return note(issue, 'warn', t('node.reason.tunnelDown'), t('note.tunnelDown.detail'))
      case 'waiting':
        return note(issue, 'info', t('node.reason.waiting'), t('note.waiting.detail'))
      case 'certExpiring':
        return note(issue, n.cert.validDays <= 3 ? 'danger' : 'warn', certReason(n, t), t('note.cert.detail'), [{ id: 'renewCert', primary: true }])
      case 'entryOff':
        return note(issue, 'info', t('note.entryOff.title'), t('note.entryOff.detail'), [{ id: 'openSettings' }])
    }
  })
}
