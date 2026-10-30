import { useMemo, useState } from 'react'
import { Card, ErrorBox, Spinner, Switch, useToast } from '../components/ui'
import { usePoll } from '../hooks/usePoll'
import { useErrorText, useLang } from '../i18n'
import { api, subscriptions as subscriptionsApi, type AppConfig, type Subscription } from '../lib/api'
import {
  nameList,
  nodeInfos,
  onNodes,
  readyNodes,
  sortByName,
  subscriptionStatus,
  switchedText,
  totals,
  trafficText,
  upDown,
  type NodeInfo,
} from '../lib/subscriptions'
import { shareTarget, useSubscriptionDialogs } from './SubscriptionDialogs'
import {
  AddButton,
  EmptyState,
  Name,
  NodeHead,
  RowMenu,
  ShareButton,
  StatusBadge,
  Tile,
  Tiles,
  Updated,
  useLoadedAt,
  type RowActions,
} from './SubscriptionParts'

/** The Subscriptions tab: every subscription, a switch for each node it can be on, and what it has moved. */
export function SubscriptionsScreen({ config, onChanged }: { config: AppConfig | null; onChanged: () => void }) {
  const { t, fmt, lang } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const subs = usePoll(subscriptionsApi.list, 10000)
  const nodeList = usePoll(api.nodes, 10000)
  const loadedAt = useLoadedAt(subs.data)
  // the switches in flight, as "subscription:node", so each can't be flipped twice at once
  const [pending, setPending] = useState<Set<string>>(() => new Set())

  const nodes = useMemo(() => (nodeList.data ? nodeInfos(nodeList.data) : null), [nodeList.data])
  const ready = useMemo(() => readyNodes(nodes ?? []), [nodes])
  const installing = (nodes ?? []).filter((n) => n.installing).map((n) => n.name)
  const list = useMemo(() => sortByName(subs.data ?? [], lang), [subs.data, lang])
  const loaded = subs.data !== null && nodes !== null
  const max = config?.maxSubscriptions ?? 0
  const count = subs.data?.length ?? config?.subscriptionCount ?? 0
  const error = subs.error ?? nodeList.error

  const dialogs = useSubscriptionDialogs({
    nodes,
    count,
    max,
    onChanged: () => {
      void subs.reload()
      onChanged()
    },
  })

  const flip = async (s: Subscription, n: NodeInfo, on: boolean): Promise<void> => {
    const key = `${s.id}:${n.id}`
    setPending((p) => new Set(p).add(key))
    try {
      await subscriptionsApi.setNode(s.id, n.id, on)
      // the hub keeps the choice for a node it can't reach and applies it when the node is back
      toast(switchedText(t, on, n.unreachable, s.name, n.name), 'ok', on ? undefined : { label: t('subs.toast.undo'), run: () => void flip(s, n, true) })
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
    const actions: RowActions = { share: () => dialogs.share(target), rename: () => dialogs.rename(s), remove: () => dialogs.remove(s) }
    return {
      s,
      status: subscriptionStatus(s, nodes ?? [], t, fmt, lang),
      traffic: trafficText(t, fmt, s.todayUp + s.todayDown, s.monthUp + s.monthDown),
      shareable: target.nodes.length > 0,
      actions,
      on: (n: NodeInfo) => s.nodes.find((x) => x.nodeId === n.id)?.enabled === true,
    }
  })

  const toggle = (s: Subscription, n: NodeInfo, on: boolean) => (
    <Switch
      checked={on}
      disabled={pending.has(`${s.id}:${n.id}`)}
      onChange={(next) => void flip(s, n, next)}
      label={t('subs.switch', { name: s.name, node: n.name })}
    />
  )

  // the numbers above the list
  const online = list.filter((s) => s.online === true)
  // with no node (or no subscription) there is nothing to be unsure about
  const known = list.length === 0 || ready.length === 0 || list.some((s) => s.online !== null)
  const where = ready.filter((n) => online.some((s) => s.nodes.some((x) => x.nodeId === n.id && x.online === true))).map((n) => n.name)
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
            sub={!known ? t('subs.tile.unknown') : online.length ? onNodes(t, lang, where) : t('subs.tile.nobody')}
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
            <div className="overflow-x-auto">
              <table aria-label={t('tabs.subscriptions')} className="w-full text-[13px]">
                <thead className="border-b border-border text-xs text-muted">
                  <tr>
                    <th scope="col" className="py-2.5 pe-3 ps-4 text-start font-medium">
                      {t('subs.col.subscription')}
                    </th>
                    {ready.map((n) => (
                      <th key={n.id} scope="col" className="px-3 py-2.5 text-start font-medium">
                        <NodeHead node={n} />
                      </th>
                    ))}
                    <th scope="col" className="px-3 py-2.5 text-start font-medium">
                      {t('subs.col.status')}
                    </th>
                    <th scope="col" className="px-3 py-2.5 text-start font-medium">
                      {t('subs.col.traffic')}
                    </th>
                    <th scope="col" className="px-3 py-2.5" />
                  </tr>
                </thead>
                <tbody>
                  {rows.map(({ s, status, traffic, shareable, actions, on }) => (
                    <tr key={s.id} className="border-b border-border last:border-0">
                      <th scope="row" className="py-3 pe-3 ps-4 text-start font-medium">
                        <Name className="max-w-[200px]">{s.name}</Name>
                      </th>
                      {ready.map((n) => (
                        <td key={n.id} className="px-3 py-3">
                          {toggle(s, n, on(n))}
                        </td>
                      ))}
                      <td className="px-3 py-3">
                        <div className="flex flex-col gap-0.5">
                          <StatusBadge status={status} />
                          <span className="text-xs text-faint">{status.sub}</span>
                        </div>
                      </td>
                      <td className="tabular whitespace-nowrap px-3 py-3">
                        <div>
                          {traffic.today} <span className="text-xs text-faint">{t('time.today')}</span>
                        </div>
                        <div className="text-xs text-faint">{traffic.month}</div>
                      </td>
                      <td className="px-3 py-3">
                        <div className="flex justify-end gap-1">
                          {shareable && <ShareButton onClick={actions.share} />}
                          <RowMenu name={s.name} shareOff={shareable ? null : t('subs.menu.shareOff')} actions={actions} />
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>

          {/* cards on phones */}
          <ul aria-label={t('tabs.subscriptions')} className="flex flex-col gap-3 md:hidden">
            {rows.map(({ s, status, traffic, shareable, actions, on }) => (
              <li key={s.id} className="flex flex-col gap-3 rounded-lg border border-border bg-card p-4">
                <div className="flex items-start gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="font-medium">
                      <Name>{s.name}</Name>
                    </div>
                    <div className="text-xs text-faint">{status.sub}</div>
                  </div>
                  <StatusBadge status={status} />
                  <div className="-me-2 -mt-1.5">
                    <RowMenu name={s.name} shareOff={shareable ? null : t('subs.menu.shareOff')} actions={actions} />
                  </div>
                </div>
                {ready.length > 0 && (
                  <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
                    {ready.map((n) => (
                      <li key={n.id} className="flex min-h-11 items-center justify-between gap-3 px-3 text-[13px]">
                        <NodeHead node={n} />
                        {toggle(s, n, on(n))}
                      </li>
                    ))}
                  </ul>
                )}
                <div className="flex items-end gap-2">
                  <div className="tabular min-w-0 flex-1 text-xs text-muted">{traffic.line}</div>
                  {shareable && <ShareButton className="-my-1" onClick={actions.share} />}
                </div>
              </li>
            ))}
          </ul>

          {installing.length > 0 && (
            <p className="text-xs text-faint">{t('subs.installing', { n: installing.length, nodes: nameList(lang, installing) })}</p>
          )}
        </>
      )}

      {dialogs.dialogs}
    </div>
  )
}
