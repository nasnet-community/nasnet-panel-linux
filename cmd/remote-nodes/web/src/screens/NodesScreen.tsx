import { useRef, useState } from 'react'
import { ChevronRight, Info, Plus, RemoteNodesLogo, TriangleAlert } from '../components/icons'
import { useJobs } from '../components/jobs'
import { RouteLink } from '../components/link'
import { Badge, Bar, Button, Card, ErrorBox, Spinner, UpdateChip, btn, cx } from '../components/ui'
import { usePoll } from '../hooks/usePoll'
import { navigate } from '../hooks/useRoute'
import { useConfig } from '../hooks/useConfig'
import { useErrorText, useLang } from '../i18n'
import { api, type AppConfig, type Node } from '../lib/api'
import { health } from '../lib/health'
import { jobStep, stepLabel, stepPercent } from '../lib/jobs'
import { AddNodeDialog } from './AddNodeDialog'

/** How far an install has got, from the job when the tray has it (in the page's words), else from the node. */
function InstallProgress({ node }: { node: Node }) {
  const { t, fmt } = useLang()
  const jobs = useJobs()
  const xray = useConfig()?.xray
  const i = node.installing!
  const job = jobs.get(i.jobId)
  const running = job?.steps.find((s) => s.state === 'running')
  const { n, m } = job ? jobStep(job) : { n: i.step, m: i.steps }
  const label = running ? stepLabel(t, running.name, xray) : i.label
  const pct = running ? stepPercent(running.detail) : i.percent
  const text = [t('job.step', { n, m }), label, pct === null ? '' : fmt.percent(pct)].filter(Boolean).join(' · ')
  return (
    <div className="flex flex-col gap-1.5">
      <Bar pct={pct ?? (m ? ((n - 1) / m) * 100 : 0)} />
      <div dir="auto" className="text-xs text-muted">
        {text}
      </div>
    </div>
  )
}

function NodeCard({ node }: { node: Node }) {
  const { t, fmt } = useLang()
  const jobs = useJobs()
  const h = health(node, t, fmt)
  const u = node.usage
  const started = node.installing ? jobs.get(node.installing.jobId)?.startedAt : undefined
  const num = (text: string) => <b className="text-lg font-semibold text-foreground">{text}</b>
  return (
    <RouteLink to={{ page: 'node', id: node.id }} className="group block">
      <Card className="flex h-full flex-col gap-3 p-4 transition group-hover:border-border-strong">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div dir="auto" className="truncate font-semibold">
              {node.name}
            </div>
            <div dir="ltr" className="truncate text-start font-mono text-xs text-muted">
              {node.host}
            </div>
          </div>
          <Badge tone={h.tone}>{h.label}</Badge>
        </div>
        {node.installing ? (
          <InstallProgress node={node} />
        ) : (
          <div className="tabular flex flex-wrap items-baseline gap-x-6 gap-y-1 text-[13px] text-muted">
            {u.live && node.subscriptionCount > 0 ? (
              <span>
                {num(fmt.num(u.online))} {t('node.onlineOf', { total: node.subscriptionCount })}
              </span>
            ) : (
              <span>
                {num(fmt.num(node.subscriptionCount))} {t('node.subscriptions', { n: node.subscriptionCount })}
              </span>
            )}
            <span>
              {num(fmt.bytes(u.todayUp + u.todayDown))} {t('time.today')}
            </span>
          </div>
        )}
        {h.reason && !node.installing && (
          <div className={cx('flex items-start gap-2 text-[13px]', h.tone === 'danger' ? 'text-bad-text' : 'text-warn-text')}>
            <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
            {h.reason}
          </div>
        )}
        <div className="mt-auto flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
          {node.installing ? (
            started && <span>{t('node.started', { ago: fmt.relative(started) })}</span>
          ) : (
            node.registered && (
              <span dir="ltr">
                agent {node.agentVersion || '–'} · Xray {node.xrayVersion || '–'}
              </span>
            )
          )}
          {(node.updates.agent || node.updates.xray) && <UpdateChip>{t('node.updateAvailable')}</UpdateChip>}
          <ChevronRight className="ms-auto h-4 w-4 text-faint transition group-hover:text-foreground rtl:-scale-x-100" />
        </div>
      </Card>
    </RouteLink>
  )
}

/** The Nodes tab: the node cards, how many of the cap are used, and the dialog that adds one. */
export function NodesScreen({ config, onChanged }: { config: AppConfig | null; onChanged: () => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  // an install moves every few seconds; otherwise the list can idle
  const pace = useRef(10000)
  const { data: nodes, error, loading, reload } = usePoll(api.nodes, pace.current)
  pace.current = nodes?.some((n) => n.installing) ? 2000 : 10000
  const [adding, setAdding] = useState(false)

  const max = config?.maxNodes ?? 0
  const count = nodes?.length ?? config?.nodeCount ?? 0
  const full = max > 0 && count >= max
  const add = () => !full && setAdding(true)

  return (
    <div className="flex flex-col gap-5">
      <div className="relative flex flex-wrap items-center justify-between gap-3">
        <p className="text-[13px] text-muted">{t('nodes.intro')}</p>
        {/* the button stays focusable at the limit, so the reason shows for keyboards and touch as well as the mouse */}
        <div className="group flex items-center gap-3">
          {max > 0 && <span className="text-xs text-faint">{t('nodes.count', { n: count, max })}</span>}
          {full && (
            <Badge tone="idle" icon={<Info className="h-3.5 w-3.5" />}>
              {t('nodes.limitReached')}
            </Badge>
          )}
          <button
            type="button"
            aria-disabled={full || undefined}
            aria-describedby={full ? 'cap-tip' : undefined}
            onClick={add}
            className={btn('primary', 'md', 'aria-disabled:cursor-not-allowed aria-disabled:opacity-50 aria-disabled:hover:bg-accent')}
          >
            <Plus className="h-4 w-4" /> {t('nodes.add')}
          </button>
          {full && (
            <div
              id="cap-tip"
              role="tooltip"
              className="absolute inset-x-0 top-full z-30 mt-1.5 hidden rounded-md border border-border-strong bg-surface px-3 py-2.5 text-[13px] leading-normal shadow-lg group-focus-within:block group-hover:block sm:inset-x-auto sm:end-0 sm:w-[300px]"
            >
              <b>{t('nodes.limit.title', { max })}</b> {t('nodes.limit.why')}
            </div>
          )}
        </div>
      </div>

      {error != null && <ErrorBox>{errorText(error)}</ErrorBox>}
      {loading && !nodes && <Spinner />}

      {nodes && nodes.length === 0 && (
        <Card className="flex flex-col items-center gap-3 px-6 py-14 text-center">
          <RemoteNodesLogo size={48} />
          <h2 className="text-base font-semibold">{t('nodes.empty.title')}</h2>
          <p className="max-w-md text-[13px] text-muted">{t('nodes.empty.text')}</p>
          <Button variant="primary" onClick={add}>
            <Plus className="h-4 w-4" /> {t('nodes.addFirst')}
          </Button>
        </Card>
      )}

      {nodes && nodes.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {nodes.map((n) => (
            <NodeCard key={n.id} node={n} />
          ))}
        </div>
      )}

      <AddNodeDialog
        open={adding}
        nodes={nodes}
        onClose={() => setAdding(false)}
        onInstalled={() => {
          void reload()
          onChanged()
        }}
        onOpenNode={(id) => {
          setAdding(false)
          navigate({ page: 'node', id })
        }}
      />
    </div>
  )
}
