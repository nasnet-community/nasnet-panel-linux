import type { ReactNode } from 'react'
import { Card, cx, type Tone } from './ui'

/** A card of label and value rows, with a line of small print under it when it needs one. */
export function Section({ title, children, foot }: { title: string; children: ReactNode; foot?: string }) {
  return (
    <Card className="p-4">
      <h2 className="font-semibold">{title}</h2>
      <dl className="mt-2 divide-y divide-border">{children}</dl>
      {foot && <p className="mt-2 text-xs text-faint">{foot}</p>}
    </Card>
  )
}

const TEXT: Partial<Record<Tone, string>> = { danger: 'font-medium text-bad-text', warn: 'font-medium text-warn-text' }

/** A row; a number that is off gets its tone and a hint that says what it means. */
export function Item({ label, children, tone, hint }: { label: string; children: ReactNode; tone?: Tone; hint?: string | false }) {
  return (
    <div className="py-2 text-[13px]">
      <div className="flex items-baseline justify-between gap-3">
        <dt className="text-muted">{label}</dt>
        <dd className={cx('tabular min-w-0 break-words text-end', tone && TEXT[tone])}>{children}</dd>
      </div>
      {hint && <p className="mt-0.5 text-xs text-faint">{hint}</p>}
    </div>
  )
}

/** A card's heading with what it is about, and its action on the end. */
export function Head({ title, hint, children }: { title: string; hint: string; children?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <h2 className="font-semibold">{title}</h2>
        <p className="text-[13px] text-muted">{hint}</p>
      </div>
      {children}
    </div>
  )
}

/** A small outlined note next to a value: an update waiting, a version needed. */
export function Tag({ children }: { children: ReactNode }) {
  return <span className="ms-1.5 inline-block whitespace-nowrap rounded-full border border-border-strong px-1.5 py-px text-[11px] text-foreground">{children}</span>
}
