import { useEffect, useState, type ReactNode } from 'react'
import { Ellipsis, Gauge, Info, Pencil, Plus, QrCode as QrIcon, RotateCcw, Trash2, Users } from '../components/icons'
import { Badge, Button, Card, Menu, btn, cx, type Tone } from '../components/ui'
import { useNow } from '../hooks/usePoll'
import { useLang } from '../i18n'
import type { Subscription } from '../lib/api'
import { expiryText, quotaTone, usedPercent, type NodeInfo, type Status, type StatusKey } from '../lib/subscriptions'

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

const TONE: Record<StatusKey, Tone> = { online: 'online', active: 'active', off: 'muted', unknown: 'muted', limit: 'warn', expired: 'warn' }

export const StatusBadge = ({ status }: { status: Status }) => <Badge tone={TONE[status.key]}>{status.label}</Badge>

/**
 * A subscription's name. It reads in its own direction (a Persian name in an English page and the other
 * way round), sits at the start of its cell either way, and is cut short when it is too long.
 */
export const Name = ({ children, className }: { children: string; className?: string }) => (
  <span dir="auto" className={cx('inline-block max-w-full truncate align-bottom', className)}>
    {children}
  </span>
)

/** A node's name over a column of switches, with a dot for how it is doing. */
export function NodeHead({ node }: { node: NodeInfo }) {
  const { t } = useLang()
  return (
    <span className="inline-flex items-center gap-1.5">
      <span dir="auto">{node.name}</span>
      <span aria-hidden="true" className={cx('h-1.5 w-1.5 rounded-full', node.unreachable ? 'bg-bad' : 'bg-ok')} />
      {node.unreachable && <span className="text-xs font-medium text-bad-text">{t('subs.unreachable')}</span>}
    </span>
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

export interface RowActions {
  share: () => void
  rename: () => void
  remove: () => void
  /** the limits and the usage count belong to the subscription, so only its own list has these two */
  quota?: () => void
  reset?: () => void
}

/** The "…" menu of a row. `shareOff` is why Share link doesn't work, when it doesn't. */
export function RowMenu({ name, shareOff, actions }: { name: string; shareOff: string | null; actions: RowActions }) {
  const { t } = useLang()
  return (
    <Menu
      label={t('subs.menu.actions', { name })}
      variant="ghost"
      items={[
        { label: t('subs.menu.share'), icon: <QrIcon />, disabled: !!shareOff, hint: shareOff ?? undefined, onSelect: actions.share },
        ...(actions.quota ? [{ label: t('subs.menu.quota'), icon: <Gauge />, onSelect: actions.quota }] : []),
        { label: t('subs.menu.rename'), icon: <Pencil />, onSelect: actions.rename },
        ...(actions.reset ? [{ label: t('subs.menu.reset'), icon: <RotateCcw />, onSelect: actions.reset }] : []),
        { label: t('subs.menu.delete'), icon: <Trash2 />, danger: true, divide: true, onSelect: actions.remove },
      ]}
    >
      <Ellipsis className="h-4 w-4" />
    </Menu>
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

/** What a list shows before the first subscription. `noNodes`: there is nothing for one to be switched on for yet. */
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

/** A subscription's limits at a glance: how much of the traffic limit it used, and when it ends. */
export function QuotaCell({ s, className }: { s: Subscription; className?: string }) {
  const { t, fmt } = useLang()
  const now = useNow(60000)
  const pct = usedPercent(s.trafficUsed, s.trafficLimit)
  const ends = expiryText(t, fmt, s.expiresAt, now)
  return (
    <div className={cx('flex min-w-[9.5rem] flex-col gap-1', className)}>
      {s.trafficLimit > 0 ? (
        <>
          <span className="tabular text-[13px]">{t('subs.quota.usedOf', { used: fmt.bytes(s.trafficUsed), limit: fmt.bytes(s.trafficLimit) })}</span>
          <Meter pct={pct} label={t('subs.quota.traffic')} />
        </>
      ) : (
        <span className="text-xs text-faint">{t('subs.quota.none')}</span>
      )}
      <span className={cx('text-xs', s.stopped === 'expired' ? 'text-warn-text' : 'text-faint')}>{ends || t('subs.quota.noEnd')}</span>
    </div>
  )
}
