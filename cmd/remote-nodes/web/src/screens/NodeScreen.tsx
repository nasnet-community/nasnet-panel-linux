import { useRef, useState, type ReactNode } from 'react'
import { ChevronLeft, Download, Ellipsis, KeyRound, Pencil, RefreshCw, Trash2 } from '../components/icons'
import { useJobs } from '../components/jobs'
import { RouteLink } from '../components/link'
import { Badge, Button, Card, ErrorBox, Menu, Spinner, Tabs, useToast, type MenuItem } from '../components/ui'
import { useConfigState } from '../hooks/useConfig'
import { usePoll } from '../hooks/usePoll'
import { navigate, parseRoute, type NodeTab } from '../hooks/useRoute'
import { useErrorText, useLang, type MsgKey } from '../i18n'
import { ApiError, api, type Job, type Node } from '../lib/api'
import { health, nodeNotes, unreachable, type NodeAct } from '../lib/health'
import { isUnfinished } from '../lib/install'
import { jobTitle } from '../lib/jobs'
import { agentUptime } from '../lib/node'
import { AttentionList, type AttentionItem } from './Attention'
import { HandshakeDialog, PortDialog, RemoveDialog, RenameDialog, RenewDialog, RotateDialog, ServerNameDialog, UpdateDialog, type StartJob } from './NodeDialogs'
import { NodeSettingsTab } from './NodeSettingsTab'
import { NodeStatusTab } from './NodeStatusTab'
import { NodeSubscriptionsTab } from './NodeSubscriptionsTab'
import { InstallGone, UnfinishedDialogs, UnfinishedView, type UnfinishedModal } from './Unfinished'

type Modal =
  | { kind: 'rename' | 'remove' | 'rotate' | 'renew' | 'port' | 'target' }
  | { kind: 'update'; what: 'agent' | 'xray' }
  | { kind: 'sni'; sni: string }

/** A chip that says an update is waiting and opens its confirmation. Small to look at, a 44 px target for a thumb. */
function UpdateButton({ children, disabled, title, onClick }: { children: ReactNode; disabled: boolean; title?: string; onClick: () => void }) {
  return (
    <button
      type="button"
      disabled={disabled}
      title={title}
      onClick={onClick}
      className="relative inline-flex h-6 items-center gap-1 whitespace-nowrap rounded-full border border-border-strong px-2 text-[11.5px] font-medium text-foreground before:absolute before:-inset-x-1 before:-inset-y-2.5 before:content-[''] hover:bg-card2 disabled:cursor-not-allowed disabled:opacity-60 sm:h-5 sm:before:hidden"
    >
      <Download className="h-3 w-3" />
      {children}
    </button>
  )
}

/** A tab that has nothing to show yet: the node is still being installed, or never was. */
function Waiting({ node }: { node: Node }) {
  const { t } = useLang()
  return (
    <Card className="px-6 py-10 text-center text-[13px] text-muted">{node.installing ? t('node.waitInstall') : t('node.waitNone')}</Card>
  )
}

/** A node's page: a way back, its header card, what needs attention, and the Subscriptions, Status and Settings tabs. */
export function NodeScreen({ id, tab }: { id: string; tab: NodeTab }) {
  const { t, fmt } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const jobs = useJobs()
  const config = useConfigState()
  const nid = Number(id)
  const [modal, setModal] = useState<Modal | null>(null)
  const [unfinishedModal, setUnfinishedModal] = useState<UnfinishedModal | null>(null)
  const [checking, setChecking] = useState(false)
  // a running install moves every few seconds; otherwise the page can idle
  const pace = useRef(5000)
  const detail = usePoll(() => api.node(nid), pace.current, [nid])
  const node = detail.data
  pace.current = node?.installing ? 2000 : 5000
  // a removal's last event can land after the node is gone and this page is left, so it leans on this
  const last = useRef<Node | null>(null)
  if (node) last.current = node
  // the words of a toast are the language's at the moment the job ends
  const tRef = useRef(t)
  tRef.current = t

  const jobDone = (j: Job) => {
    const name = last.current?.name ?? ''
    toast(jobTitle(tRef.current, j, name), j.state === 'ok' ? 'ok' : 'error')
    config.reload()
    if (j.kind === 'remove' && j.state === 'ok') {
      const here = parseRoute(window.location.hash)
      if (here.page === 'node' && here.id === id) navigate({ page: 'root', tab: 'nodes' })
      return
    }
    void detail.reload()
  }

  const start: StartJob = async (call) => {
    const { jobId } = await call()
    jobs.watch(jobId, jobDone)
    jobs.open(jobId)
    setModal(null)
  }

  // the node's data, with what it had that a shorter reply (a rename, an update check) leaves out;
  // a poll already on its way may be older than the reply, so it is asked for again
  const take = (fresh: Node) => {
    detail.setData((old) => ({ ...fresh, events: fresh.events ?? old?.events }))
    void detail.reload()
  }

  const check = async () => {
    setChecking(true)
    try {
      const fresh = await api.checkUpdates(nid)
      take(fresh)
      const what = [fresh.updates.agent && `agent ${fresh.updates.agentVersion}`, fresh.updates.xray && `Xray ${fresh.updates.xrayVersion}`].filter(Boolean).join(' · ')
      toast(what ? t('node.check.found', { what }) : t('node.check.none'))
    } catch (e) {
      toast(errorText(e), 'error')
    } finally {
      setChecking(false)
    }
  }

  const checkAgain = async () => {
    try {
      const fresh = await api.node(nid)
      take(fresh)
      toast(unreachable(fresh) ? t('node.stillDown', { name: fresh.name }) : t('node.backUp', { name: fresh.name }), unreachable(fresh) ? 'error' : 'ok')
    } catch (e) {
      toast(errorText(e), 'error')
    }
  }

  const tabs: [NodeTab, string][] = [
    ['subscriptions', t('tabs.subscriptions')],
    ['status', t('tabs.status')],
    ['settings', t('tabs.settings')],
  ]

  const back = (
    <RouteLink to={{ page: 'root', tab: 'nodes' }} className="inline-flex w-fit items-center gap-1.5 text-[13px] text-muted hover:text-foreground">
      <ChevronLeft className="h-4 w-4 rtl:-scale-x-100" />
      {t('node.all')}
    </RouteLink>
  )
  // a node that is gone may be an install that failed and was cleaned away: its job says how it went
  const gone = detail.error instanceof ApiError && detail.error.status === 404
  const failedInstall = gone ? jobs.list.find((j) => j.kind === 'install' && j.nodeId === nid && j.state === 'failed') : undefined
  if (failedInstall) return <InstallGone job={failedInstall} target={jobs.target(failedInstall) || last.current?.name || ''} />
  if (!node || gone) {
    return (
      <div className="flex flex-col gap-5">
        {back}
        {detail.error != null ? <ErrorBox>{errorText(detail.error)}</ErrorBox> : <Spinner />}
      </div>
    )
  }

  // an install that didn't finish is no node: no tabs, no subscriptions, no updates, no settings
  if (isUnfinished(node)) {
    return (
      <>
        <UnfinishedView node={node} onAct={setUnfinishedModal} />
        <UnfinishedDialogs
          modal={unfinishedModal}
          onClose={() => setUnfinishedModal(null)}
          onEnded={(j) => {
            config.reload()
            if (j.state !== 'ok') return void detail.reload()
            const here = parseRoute(window.location.hash)
            if (here.page === 'node' && here.id === id) navigate({ page: 'root', tab: 'nodes' })
          }}
        />
      </>
    )
  }

  const h = health(node, t, fmt)
  const down = unreachable(node)
  const { updates, agent } = node
  // the agent has to answer for anything but renaming and removing
  const canAct = node.registered && !node.installing && !down
  const why = canAct ? undefined : node.installing ? t('node.afterInstall') : !node.registered ? t('node.reason.notFinished') : t('node.needsLink')
  // the Xray the router uses, from the node's own update info or the config
  const pin = updates.xrayVersion || config.data?.xray || ''
  // a node on another Xray than the router's is due the change, whichever of the two is ahead
  const xrayDue = updates.xray || node.tunnel.reason === 'xray_mismatch'
  const close = () => setModal(null)
  const open = (what: 'agent' | 'xray') => setModal({ kind: 'update', what })

  const updateItem = (what: 'agent' | 'xray'): MenuItem => {
    const due = what === 'agent' ? updates.agent : xrayDue
    const next = what === 'agent' ? updates.agentVersion : pin
    const label = what === 'agent' ? 'menu.updateAgent' : 'menu.updateXray'
    return {
      label: due && next ? t(`${label}To` as MsgKey, { version: next }) : t(label),
      icon: <Download />,
      disabled: !canAct || !due,
      hint: why ?? (due ? undefined : t('menu.upToDate')),
      onSelect: () => open(what),
    }
  }
  const menu: MenuItem[] = [
    updateItem('agent'),
    updateItem('xray'),
    { label: t('menu.renewCert'), icon: <RefreshCw />, disabled: !canAct, hint: why, onSelect: () => setModal({ kind: 'renew' }) },
    { label: t('menu.rotateKeys'), icon: <KeyRound />, disabled: !canAct, hint: why ?? t('menu.keysHint'), onSelect: () => setModal({ kind: 'rotate' }) },
    { label: t('menu.rename'), icon: <Pencil />, onSelect: () => setModal({ kind: 'rename' }) },
    { label: t('menu.remove'), icon: <Trash2 />, danger: true, divide: true, onSelect: () => setModal({ kind: 'remove' }) },
  ]

  const chip = (what: 'agent' | 'xray') => {
    const next = what === 'agent' ? updates.agentVersion : pin
    const have = what === 'agent' ? node.agentVersion : node.xrayVersion
    return (
      <UpdateButton disabled={!canAct} title={why} onClick={() => open(what)}>
        {next && next !== have ? t('node.updateTo', { version: next }) : t('node.updateAvailable')}
      </UpdateButton>
    )
  }

  const acts: Record<NodeAct, () => void> = {
    updateXray: () => open('xray'),
    checkAgain: () => void checkAgain(),
    remove: () => setModal({ kind: 'remove' }),
    showJob: () => node.installing && jobs.open(node.installing.jobId),
    openSettings: () => navigate({ page: 'root', tab: 'settings' }),
    renewCert: () => setModal({ kind: 'renew' }),
  }
  const notes: AttentionItem[] = nodeNotes(node, { t, fmt, xray: config.data?.xray }).map((note) => ({
    tone: note.tone,
    title: note.title,
    detail: note.detail,
    acts: note.acts.map((a) => ({ label: t(`act.${a.id}` as MsgKey), primary: a.primary, run: acts[a.id] })),
  }))

  return (
    <div className="flex flex-col gap-5">
      {back}

      <Card className="flex flex-col gap-4 p-4 sm:flex-row sm:items-center">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 dir="auto" className="min-w-0 max-w-full truncate text-lg font-semibold">
              {node.name}
            </h1>
            <Badge tone={h.tone}>{h.label}</Badge>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted">
            <span dir="ltr" className="font-mono">
              {node.host}
            </span>
            <span>{t('node.port', { port: String(node.entryPort) })}</span>
            {node.agentVersion && (
              <span className="inline-flex items-center gap-1.5">
                <span dir="ltr">agent {node.agentVersion}</span>
                {updates.agent && chip('agent')}
              </span>
            )}
            {node.xrayVersion && (
              <span className="inline-flex items-center gap-1.5">
                <span dir="ltr">Xray {node.xrayVersion}</span>
                {xrayDue && chip('xray')}
              </span>
            )}
            {agent && !down && agentUptime(agent) !== null && <span>{t('node.up', { time: fmt.duration(agent.uptimeSec) })}</span>}
          </div>
        </div>
        <div role="group" aria-label={t('node.actions')} className="grid grid-cols-[1fr_auto] gap-2 sm:flex">
          <Button size="sm" busy={checking} disabled={!canAct} title={why} onClick={check}>
            {!checking && <RefreshCw className="h-3.5 w-3.5" />} {t('node.checkUpdates')}
          </Button>
          <Menu label={t('node.moreActions')} items={menu}>
            <Ellipsis className="h-4 w-4" />
          </Menu>
        </div>
      </Card>

      {detail.error != null && <ErrorBox>{errorText(detail.error)}</ErrorBox>}
      <AttentionList list={notes} />

      <Tabs label={t('tabs.node')} idBase="node" tabs={tabs} value={tab} onChange={(next) => navigate({ page: 'node', id, tab: next })} />

      <div role="tabpanel" id="node-panel" aria-labelledby={`node-${tab}`}>
        {tab === 'subscriptions' && <NodeSubscriptionsTab node={node} />}
        {tab === 'status' && (node.installing || !node.registered ? <Waiting node={node} /> : <NodeStatusTab node={node} />)}
        {tab === 'settings' &&
          (node.installing || !node.registered ? (
            <Waiting node={node} />
          ) : (
            <NodeSettingsTab
              node={node}
              onChangePort={() => setModal({ kind: 'port' })}
              onChangeTarget={() => setModal({ kind: 'target' })}
              onRotate={() => setModal({ kind: 'rotate' })}
              onRenew={() => setModal({ kind: 'renew' })}
              onChangeName={(sni) => setModal({ kind: 'sni', sni })}
            />
          ))}
      </div>

      {modal?.kind === 'rename' && <RenameDialog node={node} onClose={close} onRenamed={take} />}
      {modal?.kind === 'remove' && <RemoveDialog node={node} onClose={close} onStarted={(jobId) => void start(async () => ({ jobId }))} />}
      {modal?.kind === 'rotate' && <RotateDialog node={node} start={start} onClose={close} />}
      {modal?.kind === 'renew' && <RenewDialog node={node} start={start} onClose={close} />}
      {modal?.kind === 'update' && <UpdateDialog node={node} what={modal.what} start={start} onClose={close} />}
      {modal?.kind === 'port' && <PortDialog node={node} start={start} onClose={close} />}
      {modal?.kind === 'target' && <HandshakeDialog node={node} start={start} onClose={close} />}
      {modal?.kind === 'sni' && <ServerNameDialog node={node} sni={modal.sni} start={start} onClose={close} />}
    </div>
  )
}
