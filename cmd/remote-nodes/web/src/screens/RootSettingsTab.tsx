import { ExternalLink } from '../components/icons'
import { Head, Item, Section } from '../components/section'
import { Badge, Card, Spinner, type Tone } from '../components/ui'
import { useLang, type MsgKey, type T } from '../i18n'
import type { AppConfig, Entry } from '../lib/api'
import { sourceUrl } from '../lib/node'
import { BackupCard } from './BackupCard'

type Reach = Entry['domestic']

const reach = (r: Reach, t: T): { tone: Tone; text: string } =>
  r === 'online' ? { tone: 'online', text: t('entry.online') } : r === 'offline' ? { tone: 'danger', text: t('entry.offline') } : { tone: 'warn', text: t('entry.missing') }

/** The VPN as the router has it: "WireGuard · online", or just "online" when its kind isn't known. */
function vpnBadge(v: Entry['vpn'], t: T): { tone: Tone; text: string } {
  const r = reach(v.state, t)
  const name = v.kind === 'wireguard' || v.kind === 'l2tp' || v.kind === 'openvpn' ? t(`vpn.${v.kind}`) : v.label
  if (v.state === 'missing' || !name) return r
  return { tone: r.tone, text: t(v.state === 'online' ? 'rs.vpnOnline' : 'rs.vpnOffline', { name }) }
}

/** What the router says about itself: the entry's three parts, RouterOS, and when it last reported. */
function EntrySection({ entry }: { entry: Entry }) {
  const { t, fmt } = useLang()
  const domestic = reach(entry.domestic, t)
  const vpn = vpnBadge(entry.vpn, t)
  return (
    <Section title={t('rs.entry')}>
      <Item label={t('entry.domestic')}>
        <Badge tone={domestic.tone}>{domestic.text}</Badge>
      </Item>
      <Item label={t('entry.vpn')}>
        <Badge tone={vpn.tone}>{vpn.text}</Badge>
      </Item>
      <Item label={t('entry.bridge')}>
        <Badge tone={entry.bridge === 'running' ? 'online' : 'idle'}>{t(`entry.${entry.bridge}`)}</Badge>
      </Item>
      <Item label="RouterOS" tone={entry.routerOS.ok ? undefined : 'danger'} hint={t('rs.routerOS.hint', { min: entry.routerOS.min })}>
        <span dir="ltr">{entry.routerOS.version}</span>
      </Item>
      <Item label={t('rs.checked')} tone={entry.stale ? 'warn' : undefined} hint={entry.stale && t('rs.stale')}>
        {entry.checkedAt ? fmt.relative(entry.checkedAt, Date.now(), true) : t('entry.notChecked')}
      </Item>
    </Section>
  )
}

/** The three source addresses the plugin uses on the router, each with the route its traffic takes. */
function RoutesCard({ routes }: { routes: Entry['routes'] }) {
  const { t } = useLang()
  const used = (r: Entry['routes'][number]) => t(`route.${r.usedFor}` as MsgKey)
  const via = (r: Entry['routes'][number]) => t(`route.${r.via}` as MsgKey)
  const status = (r: Entry['routes'][number]) => <Badge tone={r.ready ? 'online' : 'warn'}>{r.ready ? t('route.ready') : t('route.missing')}</Badge>
  return (
    <Card className="flex flex-col gap-3 p-4">
      <Head title={t('rs.routes')} hint={t('rs.routes.hint')} />
      <table className="hidden w-full text-start text-[13px] sm:table">
        <caption className="sr-only">{t('rs.routes.caption')}</caption>
        <thead className="border-b border-border text-xs text-muted">
          <tr>
            <th scope="col" className="py-2.5 pe-3 text-start font-medium">
              {t('rs.col.address')}
            </th>
            <th scope="col" className="px-3 py-2.5 text-start font-medium">
              {t('rs.col.usedFor')}
            </th>
            <th scope="col" className="px-3 py-2.5 text-start font-medium">
              {t('rs.col.route')}
            </th>
            <th scope="col" className="py-2.5 ps-3 text-start font-medium">
              {t('rs.col.status')}
            </th>
          </tr>
        </thead>
        <tbody>
          {routes.map((r) => (
            <tr key={r.address} className="border-b border-border last:border-0">
              <td dir="ltr" className="whitespace-nowrap py-3 pe-3 text-start font-mono">
                {r.address}
              </td>
              <td className="px-3 py-3">{used(r)}</td>
              <td className="px-3 py-3">{via(r)}</td>
              <td className="whitespace-nowrap py-3 ps-3">{status(r)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {/* a phone has no room for four columns: each address becomes two lines */}
      <ul className="divide-y divide-border sm:hidden" aria-label={t('rs.routes.caption')}>
        {routes.map((r) => (
          <li key={r.address} className="py-2.5 text-[13px]">
            <div className="flex items-center justify-between gap-3">
              <span dir="ltr" className="font-mono">
                {r.address}
              </span>
              {status(r)}
            </div>
            <div className="mt-0.5 text-xs text-muted">
              {used(r)} · {via(r)}
            </div>
          </li>
        ))}
      </ul>
    </Card>
  )
}

function AboutSection({ config }: { config: AppConfig }) {
  const { t } = useLang()
  return (
    <Section
      title={t('about.title')}
      after={
        <div className="mt-2 flex flex-col gap-2">
          <p className="text-[13px] text-muted">{t('about.update')}</p>
          <div className="flex gap-5 text-[13px] font-medium">
            <a href={sourceUrl(config.commit)} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1.5 underline">
              {t('about.source')}
              <ExternalLink className="h-3.5 w-3.5 rtl:-scale-x-100" />
            </a>
            {/* relative, so the base tag sends it to this hub's /licenses whatever the base path is */}
            <a href="licenses" target="_blank" rel="noopener noreferrer" className="underline">
              {t('about.notices')}
            </a>
          </div>
        </div>
      }
    >
      <Item label={t('about.version')}>
        <span dir="ltr">Remote Nodes {config.version}</span>
      </Item>
      <Item label={t('about.commit')}>
        <span dir="ltr" className="font-mono">
          {config.commit}
        </span>
      </Item>
      <Item label={t('about.license')}>
        <span dir="ltr">AGPL-3.0</span>
      </Item>
    </Section>
  )
}

/** The router's Settings tab: its entry state and routes, the backup, and what this plugin is. */
export function RootSettingsTab({ config, onChanged }: { config: AppConfig | null; onChanged: () => void }) {
  if (!config) return <Spinner />
  return (
    <div className="grid items-start gap-4 md:grid-cols-2">
      <div className="flex min-w-0 flex-col gap-4">
        <EntrySection entry={config.entry} />
        <RoutesCard routes={config.entry.routes ?? []} />
      </div>
      <div className="flex min-w-0 flex-col gap-4">
        <BackupCard config={config} onChanged={onChanged} />
        <AboutSection config={config} />
      </div>
    </div>
  )
}
