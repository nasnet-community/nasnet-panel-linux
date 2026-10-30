import type { Format } from './format'
import type { Node } from './api'
import type { T } from '../i18n'

export type HealthTone = 'online' | 'warn' | 'danger' | 'idle'

/** The router can't reach the node's agent. */
export const unreachable = (n: Node) => n.tunnel.reason === 'agent_down'

// A time of day reads "since 07:12", a date reads "since 3 Oct": Persian needs a different word before each.
const sameDay = (iso: string, now: number) => new Date(iso).toDateString() === new Date(now).toDateString()

/** A node's health for its card: a tone, a word, and the reason when it is not fine. Worst first. */
export function health(n: Node, t: T, fmt: Format, now = Date.now()): { tone: HealthTone; label: string; reason: string } {
  const a = n.agent
  const attention = (tone: HealthTone, reason: string) => ({ tone, label: t('node.attention'), reason })
  if (n.installing) return { tone: 'idle', label: t('node.installingBadge'), reason: '' }
  if (!n.registered) return { tone: 'danger', label: t('node.notFinished'), reason: t('node.reason.notFinished') }
  if (unreachable(n))
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
  if (n.tunnel.reason === 'xray_mismatch') return attention('warn', t('node.reason.xrayMismatch'))
  if (a?.xray.crashLoop) return attention('danger', t('node.reason.xrayCrash'))
  if (a && !a.xray.running) return attention('warn', t('node.reason.xrayStopped'))
  if (a && !a.settingsApplied && a.lastPushError) return attention('warn', t('node.reason.notApplied'))
  if (n.tunnel.state === 'down') return attention('warn', t('node.reason.tunnelDown'))
  if (!a) return { tone: 'idle', label: t('node.starting'), reason: t('node.reason.waiting') }
  return { tone: 'online', label: t('node.working'), reason: '' }
}
