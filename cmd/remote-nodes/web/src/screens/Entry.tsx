import { VerdictCard, VerdictChip, type VerdictTone } from '../components/verdict'
import { useLang, type T } from '../i18n'
import type { Entry } from '../lib/api'
import type { AttentionItem } from './Attention'

// The panel's setup page is per router (/router/<id>/config), and this page doesn't know the
// router's id, so the link goes to the panel's own start page.
const PANEL_SETUP = '/'

const vpnName = (e: Entry['vpn'], t: T) =>
  e.kind === 'wireguard' || e.kind === 'l2tp' || e.kind === 'openvpn' ? t(`vpn.${e.kind}`) : e.label || t('entry.online')

/** The three parts entry rests on, each with how it is doing. */
export function entryChips(e: Entry, t: T): { key: string; tone: VerdictTone; label: string; value: string }[] {
  const missing = { tone: 'warn', value: t('entry.missing') } as const
  const offline = { tone: 'danger', value: t('entry.offline') } as const
  return [
    {
      key: 'domestic',
      label: t('entry.domestic'),
      ...(e.domestic === 'online' ? { tone: 'online' as const, value: t('entry.online') } : e.domestic === 'offline' ? offline : missing),
    },
    {
      key: 'vpn',
      label: t('entry.vpn'),
      ...(e.vpn.state === 'online' ? { tone: 'online' as const, value: vpnName(e.vpn, t) } : e.vpn.state === 'offline' ? offline : missing),
    },
    {
      key: 'bridge',
      label: t('entry.bridge'),
      tone: e.bridge === 'running' ? 'online' : 'idle',
      value: t(`entry.${e.bridge}`),
    },
  ]
}

/** Why entry is off, as items for the attention list. */
export function entryAttention(e: Entry, t: T): AttentionItem[] {
  const setup = [{ label: t('attn.openSetup'), href: PANEL_SETUP }]
  const out: AttentionItem[] = []
  if (!e.routerOS.ok)
    out.push({ tone: 'warn', title: t('attn.routerOS.title', { version: e.routerOS.version }), detail: t('attn.routerOS.detail', { min: e.routerOS.min }) })
  if (e.domestic === 'missing') out.push({ tone: 'warn', title: t('attn.noDomestic.title'), detail: t('attn.noDomestic.detail'), acts: setup })
  else if (e.domestic === 'offline') out.push({ tone: 'warn', title: t('attn.domesticDown.title'), detail: t('attn.domesticDown.detail'), acts: setup })
  if (e.vpn.state === 'missing') out.push({ tone: 'warn', title: t('attn.noVpn.title'), detail: t('attn.noVpn.detail'), acts: setup })
  else if (e.vpn.state === 'offline') out.push({ tone: 'warn', title: t('attn.vpnDown.title'), detail: t('attn.vpnDown.detail'), acts: setup })
  return out
}

/** "Entry is on" or "Entry is off", and the Domestic link, VPN and bridge behind it. */
export function EntryCard({ entry }: { entry: Entry }) {
  const { t, fmt } = useLang()
  const ago = entry.checkedAt ? fmt.relative(entry.checkedAt) : ''
  return (
    <VerdictCard
      title={entry.on ? t('entry.on') : t('entry.off')}
      note={!entry.checkedAt ? t('entry.notChecked') : t(entry.stale ? 'entry.stale' : 'entry.checked', { ago })}
      noteTone={entry.stale ? 'warn' : undefined}
      chips={entryChips(entry, t).map((c) => (
        <VerdictChip key={c.key} tone={c.tone} label={c.label} value={c.value} />
      ))}
    />
  )
}
