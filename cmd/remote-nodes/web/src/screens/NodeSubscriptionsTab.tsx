import { useMemo, useState } from 'react'
import { RouteLink } from '../components/link'
import { Card, ErrorBox, Spinner, Switch, btn, cx, useToast } from '../components/ui'
import { useConfig, useConfigState } from '../hooks/useConfig'
import { usePlaces } from '../hooks/usePlaces'
import { usePoll } from '../hooks/usePoll'
import { useErrorText, useLang } from '../i18n'
import { api, local, subscriptions as subscriptionsApi, type Node, type NodeSubscription, type NodeUsage, type PlaceId } from '../lib/api'
import { unreachable } from '../lib/health'
import { nodeSubscriptionStatus, sortByName, switchedText, trafficText, upDown } from '../lib/subscriptions'
import { useSubscriptionDialogs } from './SubscriptionDialogs'
import {
  AddButton,
  EditButton,
  EmptyState,
  ExpiryCell,
  LimitCell,
  Name,
  QuotaSummary,
  RowMenu,
  ShareButton,
  StatusBadge,
  Tile,
  Tiles,
  Updated,
  useLoadedAt,
  type RowActions,
} from './SubscriptionParts'

/** The place a list of subscriptions is for: a node, or the router's own entry. */
export interface ListPlace {
  id: PlaceId
  name: string
  /** it isn't answering: a switch is kept and takes effect when it is back */
  down: boolean
  usage: NodeUsage
}

/** A node's Subscriptions tab: every subscription of the router, and whether it is switched on for this node. */
export const NodeSubscriptionsTab = ({ node }: { node: Node }) => (
  <PlaceSubscriptions place={{ id: node.id, name: node.name, down: unreachable(node), usage: node.usage }} />
)

/** Every subscription of the router, and whether it is switched on for one place. */
export function PlaceSubscriptions({ place: node }: { place: ListPlace }) {
  const { t, fmt, lang } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const config = useConfig()
  const { reload: reloadConfig } = useConfigState()
  const router = node.id === 'router'
  const list = usePoll(() => (node.id === 'router' ? local.subscriptions() : api.nodeSubscriptions(node.id)), 10000, [node.id])
  const { places: nodes } = usePlaces()
  const loadedAt = useLoadedAt(list.data)
  const [pending, setPending] = useState<Set<number>>(() => new Set())

  const rows = useMemo(() => sortByName(list.data ?? [], lang), [list.data, lang])
  const down = node.down
  const max = config?.maxSubscriptions ?? 0
  const count = list.data?.length ?? config?.subscriptionCount ?? 0

  const dialogs = useSubscriptionDialogs({
    nodes,
    count,
    max,
    onChanged: () => {
      void list.reload()
      reloadConfig()
    },
  })

  const flip = async (s: NodeSubscription, on: boolean): Promise<void> => {
    setPending((p) => new Set(p).add(s.id))
    try {
      await subscriptionsApi.setPlace(s.id, node.id, on)
      // the hub keeps the choice for a place that isn't answering and applies it when it is back
      toast(switchedText(t, on, down, s.name, node.name, router), 'ok', on ? undefined : { label: t('subs.toast.undo'), run: () => void flip(s, true) })
      await list.reload()
    } catch (e) {
      toast(errorText(e), 'error')
    } finally {
      setPending((p) => {
        const next = new Set(p)
        next.delete(s.id)
        return next
      })
    }
  }

  const view = rows.map((s) => {
    const actions: RowActions = {
      share: () => dialogs.share({ id: s.id, name: s.name, places: [{ id: node.id, name: node.name, unreachable: down }] }),
      edit: () => dialogs.edit(s),
      reset: () => dialogs.reset(s),
      remove: () => dialogs.remove(s),
    }
    return {
      s,
      status: nodeSubscriptionStatus(s, t, fmt, Date.now(), router),
      traffic: trafficText(t, fmt, s.todayUp + s.todayDown, s.monthUp + s.monthDown),
      actions,
      // a link exists only while it is switched on here
      shareOff: s.enabled ? null : t(router ? 'subs.menu.shareOffRouter' : 'subs.menu.shareOffHere'),
      switch: (
        <Switch
          checked={s.enabled}
          disabled={pending.has(s.id)}
          onChange={(on) => void flip(s, on)}
          label={t('subs.switch', { name: s.name, node: node.name })}
        />
      ),
    }
  })

  // the numbers above the list
  const off = rows.filter((s) => !s.enabled)
  const online = rows.filter((s) => s.enabled && s.online === true)
  const u = node.usage

  return (
    <div className="flex flex-col gap-4">
      {list.data && (
        <Tiles>
          <Tile
            label={t(router ? 'subs.tile.thisRouter' : 'subs.tile.thisNode')}
            value={fmt.num(rows.length - off.length)}
            unit={t('subs.tile.of', { total: rows.length })}
            sub={
              !rows.length ? ''
              : !off.length ? t('subs.tile.allOn')
              : off.length === 1 ? t('subs.tile.offOne', { name: off[0].name })
              : t('subs.tile.offMany', { n: off.length })
            }
          />
          <Tile
            label={t('subs.tile.online')}
            value={u.live ? fmt.num(online.length) : '–'}
            sub={
              !u.live ? (down ? t(router ? 'subs.tile.unknownRouter' : 'subs.tile.unknown') : t('subs.tile.notYet'))
              : online.length ? t('subs.tile.connections', { n: online.reduce((sum, s) => sum + (s.connections ?? 0), 0) })
              : t('subs.tile.nobody')
            }
          />
          <Tile label={t('subs.tile.today')} value={fmt.bytes(u.todayUp + u.todayDown)} sub={upDown(t, fmt, u.todayUp, u.todayDown)} />
          <Tile label={t('subs.tile.month')} value={fmt.bytes(u.monthUp + u.monthDown)} sub={upDown(t, fmt, u.monthUp, u.monthDown)} />
        </Tiles>
      )}

      <div className="relative flex flex-wrap items-center justify-between gap-3">
        <Updated at={loadedAt} />
        <div className="ms-auto flex flex-wrap items-center justify-end gap-3">
          <RouteLink to={{ page: 'root', tab: 'subscriptions' }} className={btn('ghost', 'sm')}>
            {t('subs.allSubscriptions')}
          </RouteLink>
          <AddButton count={count} max={max} onAdd={dialogs.add} showCount={false} />
        </div>
      </div>

      {list.error != null && <ErrorBox>{errorText(list.error)}</ErrorBox>}
      {!list.data && list.error == null && <Spinner />}

      {list.data && rows.length === 0 && <EmptyState noNodes={false} onAdd={dialogs.add} />}

      {rows.length > 0 && (
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
                    <th scope="col" className="px-3 py-2.5 text-start font-medium">
                      {t('subs.col.status')}
                    </th>
                    <th scope="col" className="px-3 py-2.5 text-start font-medium">
                      {t('subs.col.traffic')}
                    </th>
                    <th scope="col" className="px-3 py-2.5 text-start font-medium">
                      {t('subs.col.limit')}
                    </th>
                    <th scope="col" className="px-3 py-2.5 text-start font-medium">
                      {t('subs.col.expires')}
                    </th>
                    <th scope="col" className="px-3 py-2.5" />
                  </tr>
                </thead>
                <tbody>
                  {view.map(({ s, status, traffic, actions, shareOff, switch: toggle }) => (
                    <tr key={s.id} className={cx('border-b border-border last:border-0', !s.enabled && 'text-muted')}>
                      <th scope="row" className="py-3 pe-3 ps-4 text-start font-medium">
                        <Name className="max-w-[240px]">{s.name}</Name>
                      </th>
                      <td className="px-3 py-3">
                        <div className="flex items-center gap-2.5">
                          {toggle}
                          <div className="flex min-w-0 flex-col gap-0.5">
                            <StatusBadge status={status} />
                            <span className="text-xs text-faint">{status.sub}</span>
                          </div>
                        </div>
                      </td>
                      <td className="tabular whitespace-nowrap px-3 py-3">
                        <div>
                          {traffic.today} <span className="text-xs text-faint">{t('time.today')}</span>
                        </div>
                        <div className="text-xs text-faint">{traffic.month}</div>
                      </td>
                      <td className="px-3 py-3">
                        <LimitCell s={s} />
                      </td>
                      <td className="px-3 py-3">
                        <ExpiryCell s={s} />
                      </td>
                      <td className="px-3 py-3">
                        <div className="flex justify-end gap-1">
                          {s.enabled && <ShareButton onClick={actions.share} />}
                          <EditButton name={s.name} onClick={actions.edit} />
                          <RowMenu name={s.name} shareOff={shareOff} actions={actions} />
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
            {view.map(({ s, status, traffic, actions, shareOff, switch: toggle }) => (
              <li key={s.id} className={cx('flex flex-col gap-2.5 rounded-lg border border-border bg-card p-4', !s.enabled && 'text-muted')}>
                <div className="flex items-center gap-3">
                  {toggle}
                  <div className="min-w-0 flex-1 font-medium">
                    <Name>{s.name}</Name>
                  </div>
                  <StatusBadge status={status} />
                  <div className="-my-2 -me-2">
                    <RowMenu name={s.name} shareOff={shareOff} actions={actions} />
                  </div>
                </div>
                <div className="text-xs text-faint">{status.sub}</div>
                <QuotaSummary s={s} />
                <div className="flex items-end gap-2">
                  <div className="tabular min-w-0 flex-1 text-xs text-muted">{traffic.line}</div>
                  {s.enabled && <ShareButton className="-my-1" onClick={actions.share} />}
                  <EditButton name={s.name} className="-my-1" onClick={actions.edit} />
                </div>
              </li>
            ))}
          </ul>

          <p className="text-xs text-faint">{t('subs.showing', { n: rows.length, total: rows.length })}</p>
        </>
      )}

      {dialogs.dialogs}
    </div>
  )
}
