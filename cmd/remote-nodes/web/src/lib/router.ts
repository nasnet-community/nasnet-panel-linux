import type { T } from '../i18n'
import type { Entry, Local } from './api'
import type { Format } from './format'
import type { HealthTone } from './health'


/** Switches for it are kept and take effect later: it is on, and not working at the moment. */
export const routerDown = (l: Pick<Local, 'state'>) => l.state !== 'working'

/** Which half of entry is down, in a sentence. */
function entryOffText(entry: Entry | null, t: T): string {
  if (entry && entry.vpn.state !== 'online') return t('router.reason.vpnDown')
  if (entry && entry.domestic !== 'online') return t('router.reason.domesticDown')
  return t('router.reason.entryOff')
}

type Sick = Pick<Local, 'state' | 'reason' | 'detail' | 'port' | 'domesticAddr' | 'behindNat'>

/** The entry's health for its card: a tone, a word, and the reason when it is not simply working. */
export function routerHealth(l: Sick, entry: Entry | null, t: T): { tone: HealthTone; label: string; reason: string } {
  switch (l.state) {
    case 'off':
      return { tone: 'idle', label: t('router.off'), reason: '' }
    case 'starting':
      return { tone: 'idle', label: t('node.starting'), reason: startingText(l, t) }
    case 'working':
      return { tone: 'online', label: t('node.working'), reason: l.behindNat ? natText(l, t) : '' }
  }
  switch (l.reason) {
    case 'entry_off':
      return { tone: 'warn', label: t('router.paused'), reason: entryOffText(entry, t) }
    case 'no_port':
      return { tone: 'danger', label: t('node.attention'), reason: t('router.reason.noPort') }
    case 'no_address':
      return { tone: 'warn', label: t('node.attention'), reason: t('router.reason.noAddress') }
    case 'target_failed':
      return { tone: 'warn', label: t('node.attention'), reason: t('router.reason.targetFailed') }
    default:
      return { tone: 'warn', label: t('node.attention'), reason: t('router.reason.bridgeDown') }
  }
}

const startingText = (l: Sick, t: T) =>
  l.reason === 'choosing_target' ? t('router.reason.choosingTarget') : l.reason === 'waiting_router' ? t('router.reason.waitingRouter') : t('router.reason.starting')

const natText = (l: Sick, t: T) => t('router.reason.behindNat', { port: String(l.port ?? ''), addr: l.domesticAddr })

/** What a note's button does; the page supplies the handler. */
export type RouterAct = 'setName' | 'setTarget' | 'openSettings'

export interface RouterNote {
  id: string
  tone: 'danger' | 'warn' | 'info'
  title: string
  detail: string
  acts: { id: RouterAct; primary?: boolean }[]
}

/** A hub's phrase as a sentence of its own: capital first, full stop last. */
const sentence = (s: string) => {
  const text = s.charAt(0).toUpperCase() + s.slice(1)
  return /[.!?]$/.test(text) ? text : `${text}.`
}

/** What the router's page says is in the way, with the one thing to do about it. Nothing while it is off. */
export function routerNotes(l: Sick, entry: Entry | null, t: T): RouterNote[] {
  const note = (id: string, tone: RouterNote['tone'], title: string, detail: string, acts: RouterNote['acts'] = []): RouterNote => ({ id, tone, title, detail, acts })
  if (l.state === 'off') return []
  if (l.state === 'starting') return [note('starting', 'info', t('router.note.starting'), startingText(l, t))]
  if (l.state === 'working') return l.behindNat ? [note('behindNat', 'warn', t('router.note.behindNat'), natText(l, t))] : []
  switch (l.reason) {
    case 'entry_off':
      return [note('entryOff', 'warn', t('router.note.paused'), entryOffText(entry, t), [{ id: 'openSettings' }])]
    case 'no_port':
      return [note('noPort', 'danger', t('router.reason.noPort'), t('router.note.noPort'))]
    case 'no_address':
      return [note('noAddress', 'warn', t('router.reason.noAddress'), t('router.note.noAddress'), [{ id: 'setName', primary: true }])]
    case 'target_failed':
      return [
        note('targetFailed', 'warn', t('router.reason.targetFailed'), `${l.detail ? `${sentence(l.detail)} ` : ''}${t('router.note.targetFailed')}`, [{ id: 'setTarget', primary: true }]),
      ]
    default:
      return [note('bridgeDown', 'warn', t('router.reason.bridgeDown'), t('router.note.bridgeDown'))]
  }
}

export interface RouterChip {
  key: string
  tone: 'online' | 'warn' | 'danger' | 'idle'
  label: string
  value: string
}

type Seen = Sick & Pick<Local, 'handshakeTarget' | 'usage'>

/** The Status tab's one-line answer, when its numbers were read, and the three things of its own it rests on. */
export function routerVerdict(l: Seen, entry: Entry | null, t: T, fmt: Format, now = Date.now()): { title: string; note: string; chips: RouterChip[] } {
  const title =
    l.state === 'working' ? (l.behindNat ? t('status.title.warn', { n: 1 }) : t('status.title.ok'))
    : l.state === 'starting' ? t('router.note.starting')
    : l.reason === 'entry_off' ? t('router.note.paused')
    : t('status.title.blocked')
  const note = l.usage.updatedAt ? t('status.lastReport', { ago: fmt.secondsAgo(l.usage.updatedAt, now) }) : ''

  const chip = (key: string, label: string, tone: RouterChip['tone'], value: string): RouterChip => ({ key, label, tone, value })
  const port =
    l.port !== null ? chip('port', t('set.port'), l.reason === 'entry_off' ? 'idle' : 'online', t('router.port.open'))
    : l.reason === 'no_port' ? chip('port', t('set.port'), 'danger', t('router.port.closed'))
    : chip('port', t('set.port'), 'idle', t('entry.waiting'))
  const target =
    l.handshakeTarget ? chip('target', t('set.target'), 'online', t('router.target.set'))
    : l.reason === 'target_failed' ? chip('target', t('set.target'), 'warn', t('router.target.none'))
    : chip('target', t('set.target'), 'idle', t('router.target.choosing'))
  const xray =
    l.state === 'working' ? chip('xray', t('entry.bridge'), 'online', t('entry.running'))
    : l.reason === 'bridge_down' ? chip('xray', t('entry.bridge'), 'warn', t('entry.stopped'))
    : entry ? chip('xray', t('entry.bridge'), entry.bridge === 'running' ? 'online' : 'idle', t(`entry.${entry.bridge}`))
    : chip('xray', t('entry.bridge'), 'idle', '–')
  return { title, note, chips: [port, target, xray] }
}
