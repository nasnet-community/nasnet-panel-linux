import { useMemo, useState } from 'react'
import { Card, ErrorBox, Spinner, Switch, useToast } from '../components/ui'
import { usePlaces } from '../hooks/usePlaces'
import { usePoll } from '../hooks/usePoll'
import { useErrorText, useLang } from '../i18n'
import { subscriptions as subscriptionsApi, type AppConfig, type Subscription } from '../lib/api'
import {
  enabledAt,
  nameList,
  onNodes,
  onlineAt,
  readyNodes,
  sortByName,
  subscriptionStatus,
  switchedText,
  totals,
  trafficText,
  upDown,
  type Place,
} from '../lib/subscriptions'
import { shareTarget, useSubscriptionDialogs } from './SubscriptionDialogs'
import {
  AddButton,
  DaysCell,
  EmptyState,
  LimitsCell,
  Name,
  NodeHead,
  PlaceSwitch,
  QuotaSummary,
  RowButtons,
  StatusLine,
  Tile,
  Tiles,
  Updated,
  useLoadedAt,
  type RowActions,
} from './SubscriptionParts'

/** The Subscriptions tab: every subscription, a switch for each place it can be on (the router's own entry and the nodes), and what it has moved. */
export function SubscriptionsScreen({ config, onChanged }: { config: AppConfig | null; onChanged: () => void }) {
  const { t, fmt, lang } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const subs = usePoll(subscriptionsApi.list, 10000)
  const { places: nodes, error: placesError } = usePlaces()
  const loadedAt = useLoadedAt(subs.data)
  // the switches in flight, as "subscription:place", so each can't be flipped twice at once
  const [pending, setPending] = useState<Set<string>>(() => new Set())

  const ready = useMemo(() => readyNodes(nodes ?? []), [nodes])
  // with the router's own entry among them they are places, and not nodes alone
  const router = ready.some((n) => n.id === 'router')
  const installing = (nodes ?? []).filter((n) => n.installing).map((n) => n.name)
  const list = useMemo(() => sortByName(subs.data ?? [], lang), [subs.data, lang])
  const loaded = subs.data !== null && nodes !== null
  const max = config?.maxSubscriptions ?? 0
  const count = subs.data?.length ?? config?.subscriptionCount ?? 0
  const error = subs.error ?? placesError

  const dialogs = useSubscriptionDialogs({
    nodes,
    count,
    max,
    onChanged: () => {
      void subs.reload()
      onChanged()
    },
  })

  const flip = async (s: Subscription, n: Place, on: boolean): Promise<void> => {
    const key = `${s.id}:${n.id}`
    setPending((p) => new Set(p).add(key))
    try {
      await subscriptionsApi.setPlace(s.id, n.id, on)
      // the hub keeps the choice for a place that isn't answering and applies it when it is back
      toast(switchedText(t, on, n.unreachable, s.name, n.name, n.id === 'router'), 'ok', on ? undefined : { label: t('subs.toast.undo'), run: () => void flip(s, n, true) })
      await subs.reload()
    } catch (e) {
      toast(errorText(e), 'error')
    } finally {
      setPending((p) => {
        const next = new Set(p)
        next.delete(key)
        return next
      })
    }
  }

  const rows = list.map((s) => {
    const target = shareTarget(s, nodes ?? [])
    const actions: RowActions = { share: () => dialogs.share(target), edit: () => dialogs.edit(s) }
    return {
      s,
      status: subscriptionStatus(s, nodes ?? [], t, fmt, lang),
      traffic: trafficText(t, fmt, s.todayUp + s.todayDown, s.monthUp + s.monthDown),
      shareable: target.places.length > 0,
      actions,
    }
  })

  // a switch that is on for a place that isn't answering takes effect later
  const waits = (s: Subscription, n: Place) => enabledAt(s, n.id) && n.unreachable && !s.stopped

  // one place's switch for one subscription, with the dot that says it is connected there. On a phone
  // the "when it is back" goes under the place's name, where there is room for it.
  const toggle = (s: Subscription, n: Place, later = true) => {
    const on = enabledAt(s, n.id)
    return (
      <PlaceSwitch online={!s.stopped && onlineAt(s, n.id)} later={later && waits(s, n)}>
        <Switch checked={on} disabled={pending.has(`${s.id}:${n.id}`)} onChange={(next) => void flip(s, n, next)} label={t('subs.switch', { name: s.name, node: n.name })} />
      </PlaceSwitch>
    )
  }

  // the numbers above the list
  const online = list.filter((s) => s.online === true)
  // with no place (or no subscription) there is nothing to be unsure about
  const known = list.length === 0 || ready.length === 0 || list.some((s) => s.online !== null)
  const where = ready.filter((n) => online.some((s) => onlineAt(s, n.id))).map((n) => n.name)
  const sum = totals(list)

  return (
    <div className="flex flex-col gap-4">
      {loaded && (
        <Tiles>
          <Tile
            label={t('subs.tile.subscriptions')}
            value={fmt.num(count)}
            unit={max > 0 ? t('subs.tile.of', { total: max }) : undefined}
            sub={max > 0 ? t('subs.tile.left', { n: Math.max(0, max - count) }) : ''}
          />
          <Tile
            label={t('subs.tile.online')}
            value={known ? fmt.num(online.length) : '–'}
            sub={!known ? t('subs.tile.unknown') : online.length ? onNodes(t, lang, where, router) : t('subs.tile.nobody')}
          />
          <Tile label={t('subs.tile.today')} value={fmt.bytes(sum.todayUp + sum.todayDown)} sub={upDown(t, fmt, sum.todayUp, sum.todayDown)} />
          <Tile label={t('subs.tile.month')} value={fmt.bytes(sum.monthUp + sum.monthDown)} sub={upDown(t, fmt, sum.monthUp, sum.monthDown)} />
        </Tiles>
      )}

      <div className="relative flex flex-wrap items-center justify-between gap-3">
        <Updated at={loadedAt} />
        <AddButton count={count} max={max} onAdd={dialogs.add} />
      </div>

      {error != null && <ErrorBox>{errorText(error)}</ErrorBox>}
      {!loaded && error == null && <Spinner />}

      {loaded && list.length === 0 && <EmptyState noNodes={ready.length === 0} onAdd={dialogs.add} />}

      {loaded && list.length > 0 && (
        <>
          {/* a table on wide screens */}
          <Card className="hidden overflow-hidden md:block">
            {/* relative, so the hidden text of a cell scrolls with the table instead of widening the page */}
            <div className="relative overflow-x-auto">
              <table aria-label={t('tabs.subscriptions')} className="w-full text-[13px]">
                <thead className="border-b border-border text-xs text-muted">
                  <tr>
                    <th scope="col" rowSpan={2} className="py-2.5 pe-3 ps-4 text-start align-bottom font-medium">
                      {t('subs.col.subscription')}
                    </th>
                    {/* the places sit under one heading, so four columns of switches read as one thing */}
                    {ready.length > 0 && (
                      <th scope="colgroup" colSpan={ready.length} className="px-3 pb-1 pt-2.5 text-start font-medium">
                        {t('subs.col.places')}
                      </th>
                    )}
                    <th scope="col" rowSpan={2} className="px-3 py-2.5 text-start align-bottom font-medium">
                      {t('subs.col.traffic')}
                    </th>
                    <th scope="col" rowSpan={2} className="px-3 py-2.5 text-start align-bottom font-medium">
                      {t('subs.col.limits')}
                    </th>
                    <th scope="col" rowSpan={2} className="px-3 py-2.5 text-start align-bottom font-medium">
                      {t('subs.col.days')}
                    </th>
                    <th scope="col" rowSpan={2} className="px-3 py-2.5">
                      <span className="sr-only">{t('subs.col.actions')}</span>
                    </th>
                  </tr>
                  <tr>
                    {ready.map((n) => (
                      <th key={n.id} scope="col" className="whitespace-nowrap px-3 pb-2.5 text-start font-medium text-foreground">
                        <NodeHead node={n} />
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {rows.map(({ s, status, traffic, shareable, actions }) => (
                    <tr key={s.id} className="border-b border-border last:border-0">
                      <th scope="row" className="py-3 pe-3 ps-4 text-start font-medium">
                        <Name className="max-w-[220px]">{s.name}</Name>
                        <StatusLine status={status} className="mt-0.5" />
                      </th>
                      {ready.map((n) => (
                        <td key={n.id} className="px-3 py-3 align-top">
                          {toggle(s, n)}
                        </td>
                      ))}
                      <td className="tabular whitespace-nowrap px-3 py-3">
                        <div>
                          {traffic.today} <span className="text-xs text-faint">{t('time.today')}</span>
                        </div>
                        <div className="text-xs text-faint">{traffic.month}</div>
                      </td>
                      <td className="px-3 py-3">
                        <LimitsCell s={s} />
                      </td>
                      <td className="px-3 py-3">
                        <DaysCell s={s} />
                      </td>
                      <td className="px-3 py-3">
                        <RowButtons name={s.name} shareable={shareable} actions={actions} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>

          {/* cards on phones */}
          <ul aria-label={t('tabs.subscriptions')} className="flex flex-col gap-3 md:hidden">
            {rows.map(({ s, status, traffic, shareable, actions }) => (
              <li key={s.id} className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4">
                <div className="min-w-0">
                  <div className="font-medium">
                    <Name>{s.name}</Name>
                  </div>
                  <StatusLine status={status} className="mt-0.5" />
                </div>
                {ready.length > 0 && (
                  // two to a row, so a card stays short with the router and three nodes
                  <ul className="grid grid-cols-2 gap-x-4 rounded-md border border-border px-3">
                    {ready.map((n) => (
                      <li key={n.id} className="flex min-h-11 items-center justify-between gap-2 py-1 text-[13px]">
                        <span className="flex min-w-0 flex-col">
                          <NodeHead node={n} brief />
                          {waits(s, n) && <span className="text-[11px] leading-tight text-faint">{t('subs.whenBack')}</span>}
                        </span>
                        {toggle(s, n, false)}
                      </li>
                    ))}
                  </ul>
                )}
                <QuotaSummary s={s} />
                <div className="flex items-end gap-2">
                  <div className="tabular min-w-0 flex-1 text-xs text-muted">{traffic.line}</div>
                  <RowButtons name={s.name} shareable={shareable} actions={actions} className="-my-1" />
                </div>
              </li>
            ))}
          </ul>

          <p className="text-xs text-faint">
            {t('subs.legend')}
            {installing.length > 0 && ` ${t('subs.installing', { n: installing.length, nodes: nameList(lang, installing) })}`}
          </p>
        </>
      )}

      {dialogs.dialogs}
    </div>
  )
}
