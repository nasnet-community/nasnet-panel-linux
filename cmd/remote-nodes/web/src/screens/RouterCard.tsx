import { ChevronRight, TriangleAlert } from '../components/icons'
import { RouteLink } from '../components/link'
import { Badge, Card, cx } from '../components/ui'
import { useLang } from '../i18n'
import type { Entry, Local } from '../lib/api'
import { routerHealth } from '../lib/router'
import { RouterIntro } from './RouterScreen'

/**
 * The router's own entry on the Nodes tab, before the nodes. Switched off it says what it is and offers to
 * switch it on (with the three steps while it is the only thing on the page); switched on it is a card like a node's.
 */
export function RouterCard({ local, entry, alone, onTurnOn }: { local: Local; entry: Entry | null; alone: boolean; onTurnOn: () => void }) {
  const { t, fmt } = useLang()
  const h = routerHealth(local, entry, t)
  const u = local.usage
  const num = (text: string) => <b className="text-lg font-semibold text-foreground">{text}</b>

  if (!local.enabled) {
    return (
      <Card className={cx('flex h-full flex-col gap-3 p-4', alone && 'sm:col-span-2 lg:col-span-3')}>
        <div className="flex items-start justify-between gap-3">
          <h2 className="font-semibold">{t('router.name')}</h2>
          <Badge tone={h.tone}>{h.label}</Badge>
        </div>
        <RouterIntro steps={alone} onTurnOn={onTurnOn} />
      </Card>
    )
  }

  return (
    <RouteLink to={{ page: 'router' }} className="group block">
      <Card className="flex h-full flex-col gap-3 p-4 transition group-hover:border-border-strong">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="truncate font-semibold">{t('router.name')}</div>
            {/* the port is what tells two links apart, so the name gives way and the port stays whole */}
            <div dir="ltr" className="flex font-mono text-xs text-muted rtl:justify-end">
              <span className="truncate">{local.host || '–'}</span>
              {local.host && local.port !== null && <span className="shrink-0">:{local.port}</span>}
            </div>
          </div>
          <Badge tone={h.tone}>{h.label}</Badge>
        </div>
        {local.state !== 'starting' && (
          <div className="tabular flex flex-wrap items-baseline gap-x-6 gap-y-1 text-[13px] text-muted">
            {u.live && local.subscriptionCount > 0 ? (
              <span>
                {num(fmt.num(u.online))} {t('node.onlineOf', { total: local.subscriptionCount })}
              </span>
            ) : (
              <span>
                {num(fmt.num(local.subscriptionCount))} {t('node.subscriptions', { n: local.subscriptionCount })}
              </span>
            )}
            <span>
              {num(fmt.bytes(u.todayUp + u.todayDown))} {t('time.today')}
            </span>
          </div>
        )}
        {h.reason && (
          <div className={cx('flex items-start gap-2 text-[13px]', h.tone === 'danger' ? 'text-bad-text' : h.tone === 'idle' ? 'text-muted' : 'text-warn-text')}>
            {h.tone !== 'idle' && <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />}
            {h.reason}
          </div>
        )}
        <div className="mt-auto flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
          <span>{local.xrayVersion ? t('router.card.foot', { version: local.xrayVersion }) : t('router.card.footNoVersion')}</span>
          <ChevronRight className="ms-auto h-4 w-4 text-faint transition-colors group-hover:text-foreground rtl:-scale-x-100" />
        </div>
      </Card>
    </RouteLink>
  )
}
