import type { ReactNode } from 'react'
import { ExternalLink, Info, TriangleAlert } from '../components/icons'
import { Button, btn, cx } from '../components/ui'
import { useLang } from '../i18n'

/** A button, or with `href` a link that opens in a new tab. */
export type Act = { label: string; primary?: boolean; run?: () => void; href?: string }
export type AttentionItem = { tone: 'danger' | 'warn' | 'info'; title: string; detail: ReactNode; acts?: Act[] }

const ICON = {
  danger: 'bg-bad-tint text-bad-text',
  warn: 'bg-warn-tint text-warn-text',
  info: 'bg-track text-muted',
}

/** One list above the tabs instead of stacked banners. */
export function AttentionList({ list }: { list: AttentionItem[] }) {
  const { t } = useLang()
  if (!list.length) return null
  return (
    <section aria-label={t('attn.label')} className="flex flex-col gap-2">
      {list.map((it) => (
        <div key={it.title} className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4 shadow-sm sm:flex-row sm:items-center">
          <div className="flex min-w-0 flex-1 gap-3">
            <span className={cx('flex h-8 w-8 shrink-0 items-center justify-center rounded-full', ICON[it.tone])}>
              {it.tone === 'info' ? <Info className="h-4 w-4" /> : <TriangleAlert className="h-4 w-4" />}
            </span>
            <div className="min-w-0">
              <div className="font-semibold">{it.title}</div>
              <div className="text-[13px] text-muted">{it.detail}</div>
            </div>
          </div>
          {it.acts && it.acts.length > 0 && (
            <div className="grid grid-flow-col auto-cols-fr gap-2 sm:flex">
              {it.acts.map((x) =>
                x.href ? (
                  <a key={x.label} href={x.href} target="_blank" rel="noopener" className={btn(x.primary ? 'primary' : 'secondary', 'sm')}>
                    {x.label}
                    <ExternalLink className="h-3.5 w-3.5 rtl:-scale-x-100" />
                  </a>
                ) : (
                  <Button key={x.label} size="sm" variant={x.primary ? 'primary' : 'secondary'} onClick={x.run}>
                    {x.label}
                  </Button>
                ),
              )}
            </div>
          )}
        </div>
      ))}
    </section>
  )
}
