import { useEffect, useState, type ReactNode } from 'react'
import { Info, Pencil, Plus, QrCode as QrIcon, Users } from '../components/icons'
import { Badge, Button, Card, btn, cx } from '../components/ui'
import { useNow } from '../hooks/usePoll'
import { useLang } from '../i18n'
import { limitsOf, quotaTone, statusText, type Editable, type Place, type Status, type StatusKey } from '../lib/subscriptions'

/** One number of the row above a list, with what it is and a small line under it. */
export function Tile({ label, value, unit, sub }: { label: string; value: ReactNode; unit?: string; sub: string }) {
  return (
    <Card className="min-w-0 px-4 py-3">
      <div className="truncate text-xs text-muted">{label}</div>
      <div className="tabular mt-0.5 truncate">
        <span className="text-xl font-semibold">{value}</span>
        {unit && <span className="text-[13px] text-muted"> {unit}</span>}
      </div>
      <div className="h-4 truncate text-xs text-faint">{sub}</div>
    </Card>
  )
}

/** The four tiles, two by two on a phone. */
export const Tiles = ({ children }: { children: ReactNode }) => <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">{children}</div>

/** When `data` last changed, which for a polled list is when the last load arrived. */
export function useLoadedAt(data: unknown): number | null {
  const [at, setAt] = useState<number | null>(null)
  useEffect(() => {
    if (data) setAt(Date.now())
  }, [data])
  return at
}

/** "Updated 10 s ago", ticking. */
export function Updated({ at }: { at: number | null }) {
  const { t, fmt } = useLang()
  const now = useNow(1000)
  return <span className="text-xs text-faint">{at ? t('subs.updated', { ago: fmt.secondsAgo(new Date(at).toISOString(), now) }) : null}</span>
}

// the dot before a status: green while connected, hollow while switched on with nobody connected,
// amber when the hub stopped it, faint when it is off or nothing is known
const DOT: Record<StatusKey, string> = {
  online: 'bg-ok',
  active: 'border border-border-strong',
  off: 'bg-border-strong',
  unknown: 'bg-border-strong',
  limit: 'bg-warn',
  expired: 'bg-warn',
}

/** Where a subscription stands, in one line under its name: a dot, the word, and what there is to add. */
export function StatusLine({ status, className }: { status: Status; className?: string }) {
  const stopped = status.key === 'limit' || status.key === 'expired'
  return (
    <span data-status={status.key} className={cx('flex items-center gap-1.5 text-xs font-normal', stopped ? 'text-warn-text' : status.key === 'online' ? 'text-ok-text' : 'text-muted', className)}>
      <span aria-hidden="true" className={cx('h-1.5 w-1.5 shrink-0 rounded-full', DOT[status.key])} />
      <span className="min-w-0">{statusText(status)}</span>
    </span>
  )
}

/**
 * A switch with what goes beside it: a green dot while the subscription is connected at that place,
 * and "when it is back" under one that is switched on for a place that isn't answering.
 */
export function PlaceSwitch({ children, online, later }: { children: ReactNode; /** connected there now */ online: boolean; /** on, and waiting for the place */ later: boolean }) {
  const { t } = useLang()
  return (
    <span className="inline-flex flex-col gap-0.5">
      <span className="inline-flex items-center gap-1.5">
        {children}
        <span aria-hidden="true" className={cx('h-1.5 w-1.5 shrink-0 rounded-full', online ? 'bg-ok' : 'bg-transparent')} />
        {online && <span className="sr-only">{t('subs.connectedHere')}</span>}
      </span>
      {later && <span className="whitespace-nowrap text-[11px] leading-tight text-faint">{t('subs.whenBack')}</span>}
    </span>
  )
}

/**
 * A subscription's name. It reads in its own direction (a Persian name in an English page and the other
 * way round), sits at the start of its cell either way, and is cut short when it is too long.
 */
export const Name = ({ children, className }: { children: string; className?: string }) => (
  <span dir="auto" className={cx('inline-block max-w-full truncate align-bottom', className)}>
    {children}
  </span>
)

/**
 * A place's name over a column of switches, with a dot for how it is doing. `brief`: beside a switch on a
 * phone's card. There a green dot means "connected here", so a place that is fine has none, and one that
 * isn't has a red one that stands for the word, which only screen readers get.
 */
export function NodeHead({ node, brief = false }: { node: Place; brief?: boolean }) {
  const { t } = useLang()
  const word = t(node.id === 'router' ? 'subs.notWorking' : 'subs.unreachable')
  return (
    <span className="inline-flex max-w-full items-center gap-1.5">
      <span dir="auto" className={brief ? 'truncate' : undefined}>
        {node.name}
      </span>
      {(!brief || node.unreachable) && <span aria-hidden="true" className={cx('h-1.5 w-1.5 shrink-0 rounded-full', node.unreachable ? 'bg-bad' : 'bg-ok')} />}
      {node.unreachable && <span className={brief ? 'sr-only' : 'text-xs font-medium text-bad-text'}>{word}</span>}
    </span>
  )
}

/** The pencil of a row: the edit dialog, one click away and not only in the menu. */
export const EditButton = ({ name, onClick, className }: { name: string; onClick: () => void; className?: string }) => {
  const { t } = useLang()
  const label = t('subs.edit.title', { name })
  return (
    <Button size="sm" variant="ghost" className={className} aria-label={label} title={label} onClick={onClick}>
      <Pencil className="h-4 w-4" />
    </Button>
  )
}

export const ShareButton = ({ onClick, className }: { onClick: () => void; className?: string }) => {
  const { t } = useLang()
  return (
    <Button size="sm" variant="ghost" className={className} onClick={onClick}>
      <QrIcon className="h-4 w-4" /> {t('subs.share')}
    </Button>
  )
}

/** What a row can do. Resetting its usage and deleting it are in the Edit dialog. */
export interface RowActions {
  share: () => void
  /** name, traffic limit and end date, in one dialog */
  edit: () => void
}

/** A row's two actions: Share while it has a link somewhere, and Edit. */
export function RowButtons({ name, shareable, actions, className }: { name: string; shareable: boolean; actions: RowActions; className?: string }) {
  return (
    <div className={cx('flex items-center justify-end gap-1', className)}>
      {shareable && <ShareButton onClick={actions.share} />}
      <EditButton name={name} onClick={actions.edit} />
    </div>
  )
}

/**
 * Add subscription. At the limit it stays focusable but does nothing, with a badge and a tooltip that
 * say why (the same as Add node), so keyboards and touch can reach the reason as well as the mouse.
 * The tooltip hangs from the toolbar around it, which must be `relative`.
 */
export function AddButton({ count, max, onAdd, showCount = true }: { count: number; max: number; onAdd: () => void; showCount?: boolean }) {
  const { t } = useLang()
  const full = max > 0 && count >= max
  return (
    <div className="group ms-auto flex flex-wrap items-center justify-end gap-3">
      {showCount && max > 0 && <span className="text-xs text-faint">{t('subs.count', { n: count, max })}</span>}
      {full && (
        <Badge tone="idle" icon={<Info className="h-3.5 w-3.5" />}>
          {t('subs.limitReached')}
        </Badge>
      )}
      <button
        type="button"
        aria-disabled={full || undefined}
        aria-describedby={full ? 'sub-cap-tip' : undefined}
        onClick={() => !full && onAdd()}
        className={btn('primary', 'md', 'aria-disabled:cursor-not-allowed aria-disabled:opacity-50 aria-disabled:hover:bg-accent')}
      >
        <Plus className="h-4 w-4" /> {t('subs.add')}
      </button>
      {full && (
        <div
          id="sub-cap-tip"
          role="tooltip"
          className="absolute inset-x-0 top-full z-30 mt-1.5 hidden rounded-md border border-border-strong bg-surface px-3 py-2.5 text-[13px] leading-normal shadow-lg group-focus-within:block group-hover:block sm:inset-x-auto sm:end-0 sm:w-[300px]"
        >
          <b>{t('subs.limit.title', { max })}</b> {t('subs.limit.why')}
        </div>
      )}
    </div>
  )
}

/** What a list shows before the first subscription. `noNodes`: there is nothing for one to be switched on for yet, no node and not the router. */
export function EmptyState({ noNodes, onAdd }: { noNodes: boolean; onAdd: () => void }) {
  const { t } = useLang()
  return (
    <Card className="flex flex-col items-center gap-2 px-6 py-10 text-center">
      <Users className="h-6 w-6 text-muted" />
      <h2 className="text-base font-semibold">{t('subs.empty.title')}</h2>
      <p className="max-w-md text-[13px] text-muted">{t('subs.empty.text')}</p>
      {noNodes && <p className="max-w-md text-[13px] text-muted">{t('subs.empty.noNodes')}</p>}
      <Button variant="primary" className="mt-1" onClick={onAdd}>
        <Plus className="h-4 w-4" /> {t('subs.addFirst')}
      </Button>
    </Card>
  )
}

const BAR: Record<ReturnType<typeof quotaTone>, string> = { ok: 'bg-ok', warn: 'bg-warn', bad: 'bg-bad' }

/** A thin bar for how much of a limit is used. */
function Meter({ pct, label }: { pct: number; label: string }) {
  return (
    <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct} className="h-1.5 w-full overflow-hidden rounded-full bg-track">
      <div className={cx('h-full rounded-full', BAR[quotaTone(pct)])} style={{ width: `${pct}%` }} />
    </div>
  )
}

type Quota = Pick<Editable, 'trafficLimit' | 'trafficUsed' | 'expiresAt' | 'stopped'>

const WHEN: Record<'muted' | 'warn' | 'bad', string> = { muted: 'text-faint', warn: 'text-warn-text', bad: 'text-bad-text' }

/** The Limits column: the traffic limit with its bar and what is left, and the end date beside it; the date alone when there is no limit. */
export function LimitsCell({ s }: { s: Quota }) {
  const { t, fmt } = useLang()
  const now = useNow(60000)
  const l = limitsOf(s, t, fmt, now)
  if (l.pct === null && !l.foot) return <span className="text-xs text-faint">{l.head}</span>
  return (
    <div className="flex min-w-[9rem] max-w-[15rem] flex-col gap-1">
      <span className="tabular whitespace-nowrap text-[13px]">{l.head}</span>
      {l.pct !== null && <Meter pct={l.pct} label={t('subs.quota.traffic')} />}
      <span className={cx('tabular text-xs', WHEN[l.tone])}>{l.foot}</span>
    </div>
  )
}

/**
 * The same on a phone's card, in a small box. A card has no column to keep in line, so the box is left
 * out when there is no limit and no date.
 */
export function QuotaSummary({ s }: { s: Quota }) {
  const { t, fmt } = useLang()
  const now = useNow(60000)
  const l = limitsOf(s, t, fmt, now)
  if (l.pct === null && !l.foot) return null
  return (
    <div className="flex flex-col gap-1 rounded-md border border-border px-3 py-2">
      <div className="flex flex-wrap items-baseline justify-between gap-x-2">
        <span className="tabular text-[13px]">{l.head}</span>
        <span className={cx('tabular text-xs', WHEN[l.tone])}>{l.foot}</span>
      </div>
      {l.pct !== null && <Meter pct={l.pct} label={t('subs.quota.traffic')} />}
    </div>
  )
}
