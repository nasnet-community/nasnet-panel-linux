import type { ReactNode } from 'react'
import { Card, cx } from './ui'

export type VerdictTone = 'online' | 'warn' | 'danger' | 'idle'

// background, dot, value
const CHIP: Record<VerdictTone, [string, string, string]> = {
  online: ['bg-ok-tint', 'bg-ok', 'text-ok-text'],
  warn: ['bg-warn-tint', 'bg-warn', 'text-warn-text'],
  danger: ['bg-bad-tint', 'bg-bad', 'text-bad-text'],
  idle: ['bg-track', 'bg-faint', 'text-foreground'],
}

export function VerdictChip({ tone, label, value }: { tone: VerdictTone; label: string; value: string }) {
  const [bg, dot, text] = CHIP[tone]
  return (
    <li className={cx('flex items-center gap-2 rounded-md px-3 py-1.5 text-[13px]', bg)}>
      <span className="flex items-center gap-2 text-muted">
        <span className={cx('h-2 w-2 rounded-full', dot)} />
        {label}
      </span>
      <b className={cx('font-semibold', text)}>{value}</b>
    </li>
  )
}

/** A verdict in a sentence ("Entry is on"), when it was checked, and one chip per part it rests on. */
export function VerdictCard({ title, note, noteTone, chips }: { title: string; note: ReactNode; noteTone?: 'warn'; chips: ReactNode }) {
  return (
    <section>
      <Card className="p-4">
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
          <h2 className="text-lg font-semibold">{title}</h2>
          <span className={cx('text-xs', noteTone === 'warn' ? 'text-warn-text' : 'text-faint')}>{note}</span>
        </div>
        <ul className="mt-3 flex flex-wrap gap-2">{chips}</ul>
      </Card>
    </section>
  )
}
