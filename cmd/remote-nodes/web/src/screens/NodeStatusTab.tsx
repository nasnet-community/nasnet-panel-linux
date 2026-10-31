import { TrafficChart } from '../components/chart'
import { Item, Section, Tag } from '../components/section'
import { Badge, Card } from '../components/ui'
import { VerdictCard, VerdictChip, type VerdictTone } from '../components/verdict'
import { useConfig } from '../hooks/useConfig'
import { useNow, usePoll } from '../hooks/usePoll'
import { useLang, type T } from '../i18n'
import type { Format } from '../lib/format'
import { api, type Entry, type Node } from '../lib/api'
import { issues, unreachable } from '../lib/health'
import { CERT_SOON_DAYS, agentMemory, agentUptime, eventLabel, renewLeadDays, roundTrip, settingsState } from '../lib/node'
import { entryChips } from './Entry'

interface Chip {
  key: string
  tone: VerdictTone
  label: string
  value: string
}

/** The one-line answer at the top, when the agent last reported, and the four things it rests on. */
export function nodeVerdict(
  n: Node,
  entry: Entry | null,
  t: T,
  fmt: Format,
  now = Date.now(),
): { title: string; note: string; chips: Chip[] } {
  const a = n.agent
  const found = issues(n)
  const blocked = found.some((i) => i === 'xrayMismatch' || i === 'bridgeDown' || i === 'xrayCrash' || i === 'xrayStopped' || i === 'tunnelDown')
  const warnings = found.filter((i) => i === 'notApplied' || i === 'certExpiring').length
  const title = unreachable(n)
    ? t('note.unreachable.title')
    : blocked
      ? t('status.title.blocked')
      : found.includes('waiting')
        ? t('node.reason.waiting')
        : found.includes('entryOff')
          ? t('status.title.entryOff')
          : warnings
            ? t('status.title.warn', { n: warnings })
            : t('status.title.ok')
  const ago = a ? fmt.secondsAgo(a.reportedAt, now) : ''
  const note = !a ? '' : unreachable(n) ? t('status.lastReportOld', { ago }) : t('status.lastReport', { ago })

  const chip = (key: string, label: string, tone: VerdictTone, value: string): Chip => ({ key, label, tone, value })
  // what the router can't reach it for, or hasn't heard yet, it can't say
  if (!a || unreachable(n)) {
    return {
      title,
      note,
      chips: [
        chip('portal', t('status.portal'), 'idle', '–'),
        chip('tunnel', t('status.tunnel'), 'idle', '–'),
        chip('bridge', t('entry.bridge'), 'idle', '–'),
        chip('vpn', t('entry.vpn'), 'idle', '–'),
      ],
    }
  }
  const tun = n.tunnel
  const portal = a.xray.running ? chip('portal', t('status.portal'), 'online', t('status.listening')) : chip('portal', t('status.portal'), a.xray.crashLoop ? 'danger' : 'warn', t('status.notRunning'))
  const tunnel =
    tun.state === 'up'
      ? chip('tunnel', t('status.tunnel'), 'online', t('status.tunnelUp', { time: tun.since ? fmt.duration((now - Date.parse(tun.since)) / 1000) : '' }).trim())
      : tun.state === 'down'
        ? chip('tunnel', t('status.tunnel'), 'danger', t('status.tunnelDown'))
        : chip('tunnel', t('status.tunnel'), tun.reason === 'entry_off' ? 'idle' : 'warn', t('status.tunnelOff'))
  // the bridge is the router's: it runs when this tunnel is up, and waits when the node's Xray isn't the one it speaks
  const bridge =
    tun.state === 'up'
      ? chip('bridge', t('entry.bridge'), 'online', t('entry.running'))
      : tun.reason === 'xray_mismatch'
        ? chip('bridge', t('entry.bridge'), 'idle', t('entry.waiting'))
        : tun.reason === 'bridge_down'
          ? chip('bridge', t('entry.bridge'), 'warn', t('entry.stopped'))
          : entry
            ? chip('bridge', t('entry.bridge'), entry.bridge === 'running' ? 'online' : 'idle', t(`entry.${entry.bridge}`))
            : chip('bridge', t('entry.bridge'), 'idle', '–')
  const vpn = entry ? entryChips(entry, t).find((c) => c.key === 'vpn') : undefined
  return { title, note, chips: [portal, tunnel, bridge, vpn ? chip('vpn', vpn.label, vpn.tone, vpn.value) : chip('vpn', t('entry.vpn'), 'idle', '–')] }
}

/** Why the tunnel's connection row is not simply "1 of 1 up". */
function tunnelHint(n: Node, t: T): string | false {
  if (n.tunnel.state === 'up') return false
  if (n.tunnel.reason === 'xray_mismatch') return t('status.mismatchHint')
  if (n.tunnel.reason === 'entry_off') return t('status.entryOffHint')
  return n.tunnel.state === 'down' && t('status.downHint')
}

/** What a node is doing: the verdict, 15 minutes of traffic, the agent, Xray, the tunnel and the certificate, and its events. */
export function NodeStatusTab({ node }: { node: Node }) {
  const { t, fmt } = useLang()
  const config = useConfig()
  const entry = config?.entry ?? null
  const now = useNow(1000)
  const traffic = usePoll(() => api.traffic(node.id), 15000, [node.id])
  const v = nodeVerdict(node, entry, t, fmt, now)
  const { agent, tunnel, updates, usage, cert } = node
  // the Xray the router uses, from the node's own update info or the config
  const pin = updates.xrayVersion || config?.xray || ''
  // the hub lists them in the order it keeps them; the page puts the newest first
  const events = [...(node.events ?? [])].sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
  const mismatch = tunnel.reason === 'xray_mismatch'
  const tone = tunnel.state === 'up' ? undefined : tunnel.state === 'down' ? 'danger' : 'warn'
  const left = cert.validDays
  const rtt = roundTrip(tunnel)
  const agentUp = agent ? agentUptime(agent) : null
  const memory = agent ? agentMemory(agent) : null
  const settings = agent ? settingsState(agent) : null

  return (
    <div className="flex flex-col gap-4">
      <VerdictCard
        title={v.title}
        note={v.note}
        chips={v.chips.map((c) => (
          <VerdictChip key={c.key} tone={c.tone} label={c.label} value={c.value} />
        ))}
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
        <Section title={t('section.agent')}>
          <Item label={t('status.version')}>
            <span dir="ltr">{agent?.version || node.agentVersion || '–'}</span>
            {updates.agent && <Tag>{t('status.available', { version: updates.agentVersion })}</Tag>}
          </Item>
          <Item label={t('status.runningFor')}>{agentUp === null ? '–' : fmt.duration(agentUp)}</Item>
          <Item label={t('status.memory')}>{memory === null ? '–' : fmt.bytes(memory)}</Item>
          <Item label={t('status.settings')} tone={settings === 'failed' ? 'warn' : undefined}>
            {settings === 'applied' ? <Badge tone="online">{t('status.applied')}</Badge> : settings === 'failed' ? <span dir="auto">{agent?.lastPushError}</span> : '–'}
          </Item>
        </Section>

        <Section title="Xray">
          <Item label={t('status.state')}>
            {!agent ? (
              '–'
            ) : agent.xray.crashLoop ? (
              <Badge tone="danger">{t('status.crashing')}</Badge>
            ) : agent.xray.running ? (
              <Badge tone="online">{t('status.running')}</Badge>
            ) : (
              <Badge tone="warn">{t('status.stopped')}</Badge>
            )}
          </Item>
          <Item label={t('status.version')}>
            <span dir="ltr">{agent?.xrayVersion || node.xrayVersion || '–'}</span>
            {mismatch ? <Tag>{t('status.needed', { version: pin })}</Tag> : updates.xray && <Tag>{t('status.available', { version: pin })}</Tag>}
          </Item>
          <Item label={t('status.runningFor')}>{!agent ? '–' : agent.xray.running ? fmt.duration(agent.xray.uptimeSec) : t('status.notRunning')}</Item>
          <Item label={t('status.restarts')} tone={agent?.xray.crashLoop ? 'danger' : undefined} hint={!!agent?.xray.crashLoop && t('note.xrayCrash.detail')}>
            {agent ? fmt.num(agent.xray.restarts) : '–'}
          </Item>
          {agent?.xray.lastExit && (
            <Item label={t('status.lastExit')}>
              <span dir="auto">{agent.xray.lastExit}</span>
            </Item>
          )}
        </Section>

        <Section title={t('section.tunnel')} foot={t('status.totals')}>
          <Item label={t('status.connections')} tone={tone} hint={tunnelHint(node, t)}>
            {t('status.connUp', { up: tunnel.state === 'up' ? 1 : 0, total: 1 })}
          </Item>
          <Item label={t('status.roundTrip')}>{rtt === null ? '–' : t('unit.ms', { n: rtt })}</Item>
          <Item label={t('status.connNow')}>{fmt.num(tunnel.streams)}</Item>
          <Item label={t('status.download')}>{fmt.bytes(tunnel.bytesToSubs)}</Item>
          <Item label={t('status.upload')}>{fmt.bytes(tunnel.bytesFromSubs)}</Item>
          <Item label={t('status.dialFail')} tone={tunnel.dialFailures ? 'warn' : undefined} hint={tunnel.dialFailures > 0 && t('status.dialHint')}>
            {fmt.num(tunnel.dialFailures)}
          </Item>
        </Section>

        <Section title={t('section.cert')}>
          <Item label={t('status.validFor')} tone={left <= 3 ? 'danger' : left <= CERT_SOON_DAYS ? 'warn' : undefined}>
            {left <= 0 ? t('node.reason.certExpired') : t('status.days', { n: left })}
          </Item>
          <Item label={t('status.validUntil')}>{fmt.date(cert.notAfter)}</Item>
          <Item label={t('status.renews')}>{t('status.renewsLead', { n: renewLeadDays(cert) })}</Item>
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
