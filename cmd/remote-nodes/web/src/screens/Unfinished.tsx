import { useRef, type ReactNode } from 'react'
import { ChevronLeft, TriangleAlert } from '../components/icons'
import { JobSteps } from '../components/job'
import { useJobs } from '../components/jobs'
import { RouteLink } from '../components/link'
import { Badge, Button, Card, ErrorBox, useToast } from '../components/ui'
import { useConfig } from '../hooks/useConfig'
import { useNow } from '../hooks/usePoll'
import { useLang, type T } from '../i18n'
import type { Job, Node } from '../lib/api'
import { isRefusal } from '../lib/fixes'
import { retryForm } from '../lib/install'
import { jobHeader, jobTitle, stepLabel } from '../lib/jobs'
import { askRetry } from '../lib/retry'
import { FixPanel } from './FixPanel'
import { ForgetDialog, RemoveDialog } from './NodeDialogs'

// An unfinished install is an install that failed and whose cleanup failed too. It is no node: the
// Nodes tab lists it apart, its page has no tabs, and Clean up and Forget are all it offers.

export type UnfinishedModal = { kind: 'cleanup' | 'forget'; node: Node }

/** "Stopped at Upload the agent", in the page's words, or "Stopped" when the hub can't say where. */
function stoppedAt(t: T, node: Node, xray?: string) {
  const u = node.unfinished
  return u?.step ? t('unfinished.stoppedAt', { step: stepLabel(t, { key: u.stepKey, name: u.step }, xray) }) : t('unfinished.stopped')
}

/** Clean up (a login, then the hub's cleanup) or Forget, and a toast when its job ends. */
export function UnfinishedDialogs({ modal, onClose, onEnded }: { modal: UnfinishedModal | null; onClose: () => void; onEnded: (job: Job) => void }) {
  const { t } = useLang()
  const toast = useToast()
  const jobs = useJobs()
  // the words of a toast are the language's at the moment the job ends
  const tRef = useRef(t)
  tRef.current = t
  if (!modal) return null
  const { kind, node } = modal
  const started = (jobId: string) => {
    jobs.watch(jobId, (j) => {
      const tt = tRef.current
      const ok = j.state === 'ok'
      if (kind === 'cleanup') toast(tt(ok ? 'cleanup.done' : 'cleanup.failed', { name: node.name }), ok ? 'ok' : 'error')
      else toast(ok ? tt('forget.done', { name: node.name }) : jobTitle(tt, j, node.name), ok ? 'ok' : 'error')
      onEnded(j)
    })
    // a cleanup takes a while and has steps to watch; forgetting is done at once
    if (kind === 'cleanup') jobs.open(jobId)
    onClose()
  }
  return kind === 'cleanup' ? (
    <RemoveDialog cleanup node={node} onClose={onClose} onStarted={started} />
  ) : (
    <ForgetDialog node={node} onClose={onClose} onStarted={started} />
  )
}

/** The Nodes tab's list of unfinished installs: a compact row each, never a node card. */
export function UnfinishedList({ nodes, onAct }: { nodes: Node[]; onAct: (modal: UnfinishedModal) => void }) {
  const { t, fmt } = useLang()
  const xray = useConfig()?.xray
  const now = useNow(30000)
  return (
    <section aria-labelledby="unfinished-title" className="flex flex-col gap-2">
      <div>
        <h2 id="unfinished-title" className="text-sm font-semibold">
          {t('nodes.unfinished.title')}
        </h2>
        <p className="text-[13px] text-muted">{t('nodes.unfinished.hint')}</p>
      </div>
      <Card>
        <ul className="divide-y divide-border">
          {nodes.map((n) => {
            const at = n.unfinished?.at ?? n.createdAt
            return (
              <li key={n.id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
                <TriangleAlert className="h-4 w-4 shrink-0 text-bad-text" />
                <div className="min-w-0 flex-1">
                  <RouteLink to={{ page: 'node', id: n.id }} dir="auto" className="block truncate font-medium hover:underline">
                    {n.name}
                  </RouteLink>
                  <div className="flex flex-wrap gap-x-2 text-xs text-muted">
                    <span dir="ltr" className="font-mono">
                      {n.host}
                    </span>
                    <span>{stoppedAt(t, n, xray)}</span>
                    <span>{fmt.relative(at, now)}</span>
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" onClick={() => onAct({ kind: 'cleanup', node: n })}>
                    {t('unfinished.cleanup')}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => onAct({ kind: 'forget', node: n })}>
                    {t('unfinished.forget')}
                  </Button>
                </div>
              </li>
            )
          })}
        </ul>
      </Card>
    </section>
  )
}

function Back() {
  const { t } = useLang()
  return (
    <RouteLink to={{ page: 'root', tab: 'nodes' }} className="inline-flex w-fit items-center gap-1.5 text-[13px] text-muted hover:text-foreground">
      <ChevronLeft className="h-4 w-4 rtl:-scale-x-100" />
      {t('node.all')}
    </RouteLink>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <h2 className="text-xs font-medium uppercase tracking-wide text-faint">{title}</h2>
      {children}
    </div>
  )
}

/** An unfinished install's page: what failed, why the cleanup did too, and Clean up and Forget. */
export function UnfinishedView({ node, onAct }: { node: Node; onAct: (modal: UnfinishedModal) => void }) {
  const { t } = useLang()
  const xray = useConfig()?.xray
  const u = node.unfinished
  return (
    <div className="flex flex-col gap-5">
      <Back />
      <Card className="flex flex-col gap-1 p-4">
        <div className="flex flex-wrap items-center gap-2">
          <h1 dir="auto" className="min-w-0 max-w-full truncate text-lg font-semibold">
            {t('unfinished.title', { name: node.name })}
          </h1>
          <Badge tone="danger">{t('node.notFinished')}</Badge>
        </div>
        <span dir="ltr" className="w-fit font-mono text-xs text-muted">
          {node.host}
        </span>
        <p className="mt-2 text-[13px] text-muted">{t('unfinished.desc')}</p>
      </Card>
      <Card className="flex flex-col gap-4 p-4">
        <Section title={t('unfinished.error')}>
          <div className="text-[13px] font-medium">{stoppedAt(t, node, xray)}</div>
          {u?.error && <ErrorBox>{u.error}</ErrorBox>}
        </Section>
        {u?.cleanup && (
          <Section title={t('unfinished.cleanupError')}>
            <ErrorBox>{u.cleanup}</ErrorBox>
          </Section>
        )}
        <p className="border-t border-border pt-3 text-[13px] text-muted">{t('unfinished.whatNow')}</p>
        <div role="group" aria-label={t('node.actions')} className="flex flex-wrap gap-2">
          <Button variant="primary" onClick={() => onAct({ kind: 'cleanup', node })}>
            {t('unfinished.cleanup')}
          </Button>
          <Button variant="danger" onClick={() => onAct({ kind: 'forget', node })}>
            {t('unfinished.forget')}
          </Button>
        </div>
      </Card>
    </div>
  )
}

/** The page of a node whose install failed and was cleaned away: how the install went, and another try. */
export function InstallGone({ job, target }: { job: Job; target: string }) {
  const { t, fmt } = useLang()
  const form = retryForm(job)
  const header = jobHeader(t, fmt, job, target, 0)
  const refused = isRefusal(job)
  return (
    <div className="flex flex-col gap-5">
      <Back />
      <Card className="flex flex-col gap-4 p-4">
        <div>
          <h1 dir="auto" className="text-lg font-semibold">
            {header.title}
          </h1>
          <p className="text-[13px] text-muted">{refused ? header.line : t('job.gone')}</p>
        </div>
        <JobSteps job={job} />
        {/* the dialog that sent the install is gone, and its login with it */}
        <FixPanel job={job} sent={null} />
        {form && (
          <div>
            <Button variant="primary" onClick={() => askRetry(form)}>
              {t(refused ? 'fix.change' : 'job.tryAgain')}
            </Button>
          </div>
        )}
      </Card>
    </div>
  )
}
