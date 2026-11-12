import { TrafficChart } from '../components/chart'
import { Item, Section } from '../components/section'
import { Badge, Card } from '../components/ui'
import { VerdictCard, VerdictChip } from '../components/verdict'
import { useConfig } from '../hooks/useConfig'
import { useNow, usePoll } from '../hooks/usePoll'
import { useLang } from '../i18n'
import { local as localApi, type Local } from '../lib/api'
import { eventLabel } from '../lib/node'
import { routerVerdict } from '../lib/router'
import { entryChips } from './Entry'

/** What the router's own entry is doing: the verdict, 15 minutes of traffic, its address, Xray, who is on it, and its events. */
export function RouterStatusTab({ local }: { local: Local }) {
  const { t, fmt } = useLang()
  const config = useConfig()
  const entry = config?.entry ?? null
  const now = useNow(1000)
  const traffic = usePoll(localApi.traffic, 15000)
  const v = routerVerdict(local, entry, t, fmt, now)
  const vpn = entry ? entryChips(entry, t).find((c) => c.key === 'vpn') : undefined
  const { usage } = local
  // the hub lists them in the order it keeps them; the page puts the newest first
  const events = [...(local.events ?? [])].sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
  const working = local.state === 'working'
  const source = local.hostSource === 'own' ? t('router.source.ownShort') : local.hostSource === 'cloud' ? t('router.source.cloudShort') : '–'

  return (
    <div className="flex flex-col gap-4">
      <VerdictCard
        title={v.title}
        note={v.note}
        chips={
          <>
            {v.chips.map((c) => (
              <VerdictChip key={c.key} tone={c.tone} label={c.label} value={c.value} />
            ))}
            <VerdictChip tone={vpn?.tone ?? 'idle'} label={t('entry.vpn')} value={vpn?.value ?? '–'} />
          </>
        }
      />

      <Card className="p-4">
        <div className="flex flex-col gap-1 sm:flex-row sm:items-baseline sm:justify-between">
          <h2 className="font-semibold">{t('traffic.title')}</h2>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
            <span className="inline-flex items-center gap-1.5">
              <span className="h-0.5 w-3 rounded bg-info" /> {t('traffic.down')} <b className="tabular text-foreground">{usage.live ? fmt.rate(usage.rateDown) : '–'}</b>
            </span>
            <span className="inline-flex items-center gap-1.5">
              <span className="h-0.5 w-3 rounded bg-muted" /> {t('traffic.up')} <b className="tabular text-foreground">{usage.live ? fmt.rate(usage.rateUp) : '–'}</b>
            </span>
          </div>
        </div>
        {traffic.data ? <TrafficChart traffic={traffic.data} now={now} /> : <p className="mt-3 text-[13px] text-muted">{traffic.error ? t('chart.failed') : t('chart.loading')}</p>}
      </Card>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        <Section title={t('router.set.address')}>
          <Item label={t('router.status.name')} tone={local.host ? undefined : 'warn'}>
            {local.host ? <span dir="ltr">{local.host}</span> : t('router.set.address.none')}
          </Item>
          <Item label={t('router.status.source')}>{source}</Item>
          <Item label={t('set.port')}>{local.port !== null ? <span dir="ltr">{local.port}</span> : '–'}</Item>
          <Item label={t('router.status.public')}>{local.publicAddr ? <span dir="ltr">{local.publicAddr}</span> : '–'}</Item>
          <Item label={t('router.status.domestic')}>{local.domesticAddr ? <span dir="ltr">{local.domesticAddr}</span> : '–'}</Item>
          <Item
            label={t('router.status.front')}
            tone={local.behindNat ? 'warn' : undefined}
            hint={local.behindNat && t('router.reason.behindNat', { port: String(local.port ?? ''), addr: local.domesticAddr })}
          >
            {local.behindNat ? t('router.status.frontYes') : local.publicAddr && local.domesticAddr ? t('router.status.frontNo') : '–'}
          </Item>
        </Section>

        <Section title="Xray">
          <Item label={t('status.state')}>
            {working ? <Badge tone="online">{t('status.running')}</Badge> : local.reason === 'bridge_down' ? <Badge tone="warn">{t('status.stopped')}</Badge> : '–'}
          </Item>
          <Item label={t('status.version')}>
            <span dir="ltr">{local.xrayVersion || '–'}</span>
          </Item>
          <Item label={t('status.runningFor')}>{local.uptimeSec > 0 ? fmt.duration(local.uptimeSec) : '–'}</Item>
          <Item label={t('router.status.exit')}>{t('router.status.exitVpn')}</Item>
        </Section>

        <Section title={t('router.status.use')}>
          <Item label={t('subs.tile.online')}>{usage.live ? t('router.status.onlineOf', { n: usage.online, total: local.subscriptionCount }) : '–'}</Item>
          <Item label={t('status.connNow')}>{usage.live ? fmt.num(usage.connections) : '–'}</Item>
          <Item label={t('subs.tile.today')}>{fmt.bytes(usage.todayUp + usage.todayDown)}</Item>
          <Item label={t('subs.tile.month')}>{fmt.bytes(usage.monthUp + usage.monthDown)}</Item>
        </Section>
      </div>

      <Card className="p-4">
        <h2 className="font-semibold">{t('section.events')}</h2>
        {events.length > 0 ? (
          <ul className="mt-2 divide-y divide-border">
            {events.map((e, i) => (
              <li key={i} className="flex flex-col py-2 text-[13px] sm:flex-row sm:gap-4">
                <span className="hidden w-32 shrink-0 text-xs text-faint sm:block" title={e.at}>
                  {fmt.relative(e.at, now)}
                </span>
                <span className="font-medium">{eventLabel(t, e.kind)}</span>
                <span className="min-w-0 break-words text-xs text-muted sm:text-[13px]">
                  <span className="sm:hidden">
                    {fmt.relative(e.at, now)}
                    {e.detail && ' · '}
                  </span>
                  <span dir="auto">{e.detail}</span>
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="mt-2 text-[13px] text-muted">{t('events.none')}</p>
        )}
      </Card>
    </div>
  )
}
