import { useRef, useState } from 'react'
import { ChevronLeft, Ellipsis, KeyRound, RefreshCw, Trash2 } from '../components/icons'
import { RouteLink } from '../components/link'
import { Badge, Button, Card, ErrorBox, Menu, Spinner, Tabs, type MenuItem } from '../components/ui'
import { useConfigState } from '../hooks/useConfig'
import { usePoll } from '../hooks/usePoll'
import { navigate, type NodeTab } from '../hooks/useRoute'
import { useErrorText, useLang, type MsgKey } from '../i18n'
import { local as localApi, type Local } from '../lib/api'
import { hasLinks, routerDown, routerHealth, routerNotes, type RouterAct } from '../lib/router'
import { AttentionList, type AttentionItem } from './Attention'
import { useHandout } from './Handout'
import { PlaceSubscriptions } from './NodeSubscriptionsTab'
import { NewPortDialog, OwnNameDialog, RotateKeysDialog, RouterServerNameDialog, RouterTargetDialog, TurnOffDialog, TurnOnDialog } from './RouterDialogs'
import { RouterSettingsTab } from './RouterSettingsTab'
import { RouterStatusTab } from './RouterStatusTab'

type Modal = { kind: 'on' | 'off' | 'port' | 'rotate' | 'name' | 'target' } | { kind: 'sni'; sni: string }

/** What This router is, for a router that has it switched off, with the way to switch it on. */
export function RouterIntro({ onTurnOn, steps = false }: { onTurnOn: () => void; steps?: boolean }) {
  const { t } = useLang()
  return (
    <div className="flex flex-col gap-3">
      <p className="max-w-2xl text-[13px] text-muted">{t('router.intro')}</p>
      {steps && (
        <ol className="grid gap-3 text-[13px] sm:grid-cols-3">
          {([1, 2, 3] as const).map((n) => (
            <li key={n} className="flex gap-2.5">
              <span className="tabular flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-track text-xs font-semibold">{n}</span>
              <span>
                <b className="block font-medium">{t(`router.step${n}.title` as MsgKey)}</b>
                <span className="text-muted">{t(`router.step${n}.text` as MsgKey)}</span>
              </span>
            </li>
          ))}
        </ol>
      )}
      <div>
        <Button variant="primary" onClick={onTurnOn}>
          {t('router.turnOn')}
        </Button>
      </div>
    </div>
  )
}

/** The page of the router's own entry: a way back, its header card, what is in the way, and the tabs a node has. */
export function RouterScreen({ tab }: { tab: NodeTab }) {
  const { t, fmt } = useLang()
  const errorText = useErrorText()
  const config = useConfigState()
  const [modal, setModal] = useState<Modal | null>(null)
  // the router's script runs every minute, so a start is watched more closely than a steady state
  const pace = useRef(5000)
  const detail = usePoll(localApi.get, pace.current)
  const local = detail.data
  pace.current = local?.state === 'starting' ? 2000 : 5000

  // new links can be made once it has a name and a port again, and the port is the new one
  const ready = !!local && hasLinks(local) && local.state !== 'starting'
  const handout = useHandout({ id: 'router', name: t('router.name') }, ready ? null : t('handout.wait.router'))

  // a change answers with the entry as it is afterwards; a poll already on its way may be older, so it is asked for again
  const take = (fresh: Local, links = false) => {
    detail.setData(fresh)
    void detail.reload()
    config.reload()
    if (!links) return
    // every link of the entry is new: the ones who had the old one are listed, to hand the new ones out from.
    // A new port isn't open yet, so that list waits behind its note until the router has it.
    localApi
      .subscriptions()
      .then((subs) => handout.start(subs.filter((s) => s.enabled), fresh.state !== 'starting'))
      .catch(() => {})
  }
  const close = () => setModal(null)

  const back = (
    <RouteLink to={{ page: 'root', tab: 'nodes' }} className="inline-flex w-fit items-center gap-1.5 text-[13px] text-muted hover:text-foreground">
      <ChevronLeft className="h-4 w-4 rtl:-scale-x-100" />
      {t('node.all')}
    </RouteLink>
  )
  if (!local) {
    return (
      <div className="flex flex-col gap-5">
        {back}
        {detail.error != null ? <ErrorBox>{errorText(detail.error)}</ErrorBox> : <Spinner />}
      </div>
    )
  }

  const entry = config.data?.entry ?? null
  const h = routerHealth(local, entry, t)
  const tabs: [NodeTab, string][] = [
    ['subscriptions', t('tabs.subscriptions')],
    ['status', t('tabs.status')],
    ['settings', t('tabs.settings')],
  ]
  const menu: MenuItem[] = [
    { label: t('router.menu.port'), icon: <RefreshCw />, hint: t('router.menu.linksHint'), onSelect: () => setModal({ kind: 'port' }) },
    { label: t('menu.rotateKeys'), icon: <KeyRound />, hint: t('router.menu.linksHint'), onSelect: () => setModal({ kind: 'rotate' }) },
    { label: t('router.set.off.action'), icon: <Trash2 />, danger: true, divide: true, onSelect: () => setModal({ kind: 'off' }) },
  ]
  const acts: Record<RouterAct, () => void> = {
    setName: () => setModal({ kind: 'name' }),
    setTarget: () => setModal({ kind: 'target' }),
    openSettings: () => navigate({ page: 'root', tab: 'settings' }),
  }
  const notes: AttentionItem[] = routerNotes(local, entry, t).map((note) => ({
    tone: note.tone,
    title: note.title,
    detail: note.detail,
    acts: note.acts.map((a) => ({ label: t(`router.act.${a.id}` as MsgKey), primary: a.primary, run: acts[a.id] })),
  }))

  return (
    <div className="flex flex-col gap-5">
      {back}

      <Card className="flex flex-col gap-4 p-4 sm:flex-row sm:items-center">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="min-w-0 max-w-full truncate text-lg font-semibold">{t('router.name')}</h1>
            <Badge tone={h.tone}>{h.label}</Badge>
          </div>
          {local.enabled && (
            <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted">
              {local.host && (
                <span dir="ltr" className="break-all font-mono">
                  {local.host}
                </span>
              )}
              {local.port !== null && <span>{t('node.port', { port: String(local.port) })}</span>}
              {local.xrayVersion && <span dir="ltr">Xray {local.xrayVersion}</span>}
              {local.state === 'working' && local.uptimeSec > 0 && <span>{t('node.up', { time: fmt.duration(local.uptimeSec) })}</span>}
            </div>
          )}
        </div>
        {local.enabled && (
          <div role="group" aria-label={t('router.actions')} className="flex justify-end">
            <Menu label={t('node.moreActions')} items={menu}>
              <Ellipsis className="h-4 w-4" />
            </Menu>
          </div>
        )}
      </Card>

      {detail.error != null && <ErrorBox>{errorText(detail.error)}</ErrorBox>}

      {!local.enabled ? (
        <Card className="p-4">
          <RouterIntro steps onTurnOn={() => setModal({ kind: 'on' })} />
        </Card>
      ) : (
        <>
          <AttentionList list={handout.note ? [...notes, handout.note] : notes} />

          <Tabs label={t('router.tabs')} idBase="router" tabs={tabs} value={tab} onChange={(next) => navigate({ page: 'router', tab: next })} />

          <div role="tabpanel" id="router-panel" aria-labelledby={`router-${tab}`}>
            {tab === 'subscriptions' && <PlaceSubscriptions place={{ id: 'router', name: t('router.name'), down: routerDown(local), usage: local.usage }} />}
            {tab === 'status' && <RouterStatusTab local={local} />}
            {tab === 'settings' && (
              <RouterSettingsTab
                local={local}
                onChangeName={() => setModal({ kind: 'name' })}
                onNewPort={() => setModal({ kind: 'port' })}
                onChangeTarget={() => setModal({ kind: 'target' })}
                onRotate={() => setModal({ kind: 'rotate' })}
                onChangeSni={(sni) => setModal({ kind: 'sni', sni })}
                onTurnOff={() => setModal({ kind: 'off' })}
              />
            )}
          </div>
        </>
      )}

      {modal?.kind === 'on' && <TurnOnDialog onClose={close} onChanged={take} />}
      {modal?.kind === 'off' && <TurnOffDialog local={local} onClose={close} onChanged={take} />}
      {modal?.kind === 'port' && <NewPortDialog local={local} onClose={close} onChanged={take} />}
      {modal?.kind === 'rotate' && <RotateKeysDialog local={local} onClose={close} onChanged={take} />}
      {modal?.kind === 'name' && <OwnNameDialog local={local} onClose={close} onChanged={take} />}
      {modal?.kind === 'target' && <RouterTargetDialog local={local} onClose={close} onChanged={take} />}
      {modal?.kind === 'sni' && <RouterServerNameDialog local={local} sni={modal.sni} onClose={close} onChanged={take} />}
      {handout.dialog}
    </div>
  )
}
