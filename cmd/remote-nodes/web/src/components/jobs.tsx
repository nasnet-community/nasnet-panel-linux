import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { navigate } from '../hooks/useRoute'
import { useNow } from '../hooks/usePoll'
import { useConfig } from '../hooks/useConfig'
import { codeText, useLang } from '../i18n'
import { JOB_LOST, api, followJob, type Job } from '../lib/api'
import { isRefusal } from '../lib/fixes'
import { installOutcome, retryForm } from '../lib/install'
import { jobHeader, jobStep, jobTitle, stepLabel, stepPercent } from '../lib/jobs'
import { askRetry } from '../lib/retry'
import { FixPanel } from '../screens/FixPanel'
import { CircleCheck, CircleX, History } from './icons'
import { JobSteps } from './job'
import { Bar, Button, Dialog, Spinner, cx, useToast } from './ui'

interface Jobs {
  /** running jobs and those that ended lately, newest first */
  list: Job[]
  get: (id: string | null) => Job | null
  /** follows a job live; `onEnd` runs once when it ends, even with its dialog closed */
  watch: (id: string, onEnd?: (job: Job) => void) => void
  /** shows a job's dialog */
  open: (id: string) => void
  /** the node a job is for */
  target: (job: Job) => string
}

const JobsContext = createContext<Jobs>(null!)

export const useJobs = () => useContext(JobsContext)

/** Keeps every job in view across pages and closed dialogs, and reports how each one ended. */
export function JobsProvider({ children }: { children: ReactNode }) {
  const toast = useToast()
  const { t } = useLang()
  const [listed, setListed] = useState<Job[]>([])
  const [live, setLive] = useState<Record<string, Job>>({})
  const [names, setNames] = useState<Record<number, string>>({})
  const [openId, setOpenId] = useState<string | null>(null)
  // followed jobs and what to do when each ends
  const ends = useRef(new Map<string, ((job: Job) => void) | undefined>())
  const namesRef = useRef(names)
  namesRef.current = names
  const namedAt = useRef(0)
  // the words of a toast are the language's at the moment the job ends
  const tRef = useRef(t)
  tRef.current = t

  const target = useCallback((j: Job) => j.target || (j.nodeId ? namesRef.current[j.nodeId] ?? '' : ''), [])

  const watch = useCallback(
    (id: string, onEnd?: (job: Job) => void) => {
      if (ends.current.has(id)) {
        if (onEnd) ends.current.set(id, onEnd)
        return
      }
      ends.current.set(id, onEnd)
      followJob(id, (j) => {
        setLive((l) => ({ ...l, [id]: j }))
        if (j.state === 'running') return
        const end = ends.current.get(id)
        // a job this page did not start (another tab, or before a reload) still says how it went
        if (end) end(j)
        else toast(jobTitle(tRef.current, j, target(j)), j.state === 'ok' ? 'ok' : 'error')
      })
    },
    [toast, target],
  )

  const load = useCallback(async () => {
    try {
      const jobs = await api.jobs()
      setListed(jobs)
      for (const j of jobs) if (j.state === 'running') watch(j.id)
      if (jobs.some((j) => j.nodeId && !namesRef.current[j.nodeId]) && Date.now() - namedAt.current > 30000) {
        namedAt.current = Date.now()
        const nodes = await api.nodes()
        setNames(Object.fromEntries(nodes.map((n) => [n.id, n.name])))
      }
    } catch {
      // without the list the tray just stays empty; jobs started here are still followed
    }
  }, [watch])

  const list = useMemo(() => {
    const byId = new Map(listed.map((j) => [j.id, j]))
    for (const j of Object.values(live)) byId.set(j.id, j)
    return [...byId.values()].sort((a, b) => Date.parse(b.startedAt) - Date.parse(a.startedAt))
  }, [listed, live])

  const busy = list.some((j) => j.state === 'running')
  useEffect(() => {
    void load()
    const timer = setInterval(() => document.visibilityState === 'visible' && void load(), busy ? 2000 : 15000)
    return () => clearInterval(timer)
  }, [load, busy])

  const value = useMemo<Jobs>(
    () => ({
      list,
      get: (id) => (id ? list.find((j) => j.id === id) ?? null : null),
      watch: (id, onEnd) => {
        watch(id, onEnd)
        void load()
      },
      open: setOpenId,
      target,
    }),
    [list, watch, load, target],
  )

  const shown = openId ? list.find((j) => j.id === openId) : null
  return (
    <JobsContext.Provider value={value}>
      {children}
      {shown && <JobDialog job={shown} target={target(shown)} onClose={() => setOpenId(null)} />}
    </JobsContext.Provider>
  )
}

function JobDialog({ job, target, onClose }: { job: Job; target: string; onClose: () => void }) {
  const { t, fmt } = useLang()
  const now = useNow()
  const running = job.state === 'running'
  const form = retryForm(job)
  const header = jobHeader(t, fmt, job, target, now)
  return (
    <Dialog
      onClose={onClose}
      title={header.title}
      description={header.line}
      closeLabel={running ? t('job.closeKeepsGoing') : t('common.close')}
      footer={
        <>
          {job.kind === 'install' && job.nodeId && (job.state === 'ok' || installOutcome(job) === 'kept') ? (
            // a node that was set up, or an install kept unfinished for a cleanup
            <Button
              variant="primary"
              onClick={() => {
                onClose()
                navigate({ page: 'node', id: job.nodeId! })
              }}
            >
              {t(job.state === 'ok' ? 'job.openNode' : 'job.openUnfinished')}
            </Button>
          ) : null}
          {form && (
            <Button
              variant="primary"
              onClick={() => {
                onClose()
                askRetry(form)
              }}
            >
              {t(isRefusal(job) ? 'fix.change' : 'job.tryAgain')}
            </Button>
          )}
          <Button variant={running || job.kind === 'install' ? 'secondary' : 'primary'} onClick={onClose}>
            {running ? t('common.close') : t('job.done')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <JobSteps job={job} />
        {/* no login is held here: the install was sent from a dialog that is closed by now */}
        <FixPanel job={job} sent={null} />
      </div>
      {running && <p className="mt-4 border-t border-border pt-3 text-[13px] text-muted">{t('job.keepsGoing')}</p>}
    </Dialog>
  )
}

/** The header chip: running jobs with their step and clock, and the last few results. */
export function JobTray() {
  const { list, open, target } = useJobs()
  const { t, fmt } = useLang()
  const xray = useConfig()?.xray
  const now = useNow()
  const [show, setShow] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const running = list.filter((j) => j.state === 'running')
  const recent = list.filter((j) => j.state !== 'running').slice(0, 5)

  useEffect(() => {
    if (!show) return
    const onDown = (e: MouseEvent) => !box.current?.contains(e.target as Node) && setShow(false)
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setShow(false)
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [show])

  if (!list.length) return null
  const caption =
    running.length === 1 ? jobTitle(t, running[0], target(running[0])) : running.length ? t('tray.running', { n: running.length }) : t('tray.recentJobs')
  const openJob = (id: string) => {
    setShow(false)
    open(id)
  }

  return (
    <div ref={box} className="sm:relative">
      <button
        type="button"
        aria-label={caption}
        aria-expanded={show}
        aria-controls="job-tray"
        onClick={() => setShow(!show)}
        className="inline-flex h-11 max-w-[320px] items-center gap-2 rounded-full border border-border bg-card2 px-3 text-[12.5px] focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent sm:h-8"
      >
        {running.length ? <Spinner className="text-info" /> : <History className="h-4 w-4 text-muted" />}
        <span className="hidden truncate sm:inline">{caption}</span>
        {running.length > 0 && <span className="tabular sm:hidden">{fmt.num(running.length)}</span>}
      </button>
      {show && (
        <section
          id="job-tray"
          aria-label={t('tray.title')}
          className="absolute inset-x-2 top-[60px] z-30 max-h-[75vh] overflow-y-auto rounded-lg border border-border bg-surface p-3 shadow-lg sm:inset-x-auto sm:end-0 sm:top-10 sm:w-[380px]"
        >
          <h2 className="px-1 pb-2 font-semibold">{t('tray.title')}</h2>
          <div className="flex flex-col gap-2">
            {running.map((j) => {
              const { n, m } = jobStep(j)
              const step = j.steps.find((s) => s.state === 'running')
              const pct = stepPercent(step?.detail) ?? (m ? ((n - 1) / m) * 100 : 0)
              const name = target(j)
              return (
                <div key={j.id} className="flex flex-col gap-2 rounded-md border border-border bg-card2 p-3">
                  <div className="flex items-start gap-3">
                    <Spinner className="mt-0.5 text-info" />
                    <div className="min-w-0 flex-1">
                      <div dir="auto" className="font-medium">
                        {j.kind === 'install' ? jobTitle(t, j, name) : jobTitle(t, j)}
                      </div>
                      <div className="text-xs text-muted">
                        {[j.kind === 'install' ? '' : name, m ? t('job.step', { n, m }) : '', fmt.clock((now - Date.parse(j.startedAt)) / 1000)]
                          .filter(Boolean)
                          .join(' · ')}
                      </div>
                    </div>
                    <Button size="sm" onClick={() => openJob(j.id)}>
                      {t('tray.open')}
                    </Button>
                  </div>
                  <Bar pct={pct} />
                  {step && (
                    <div dir="auto" className="truncate text-xs text-muted">
                      {stepLabel(t, step, xray)}
                      {step.detail ? ` · ${step.detail}` : ''}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
          {recent.length > 0 && (
            <>
              <div className="px-1 pb-1 pt-3 text-xs font-medium uppercase tracking-wide text-faint">{t('tray.recent')}</div>
              <ul className="divide-y divide-border">
                {recent.map((j) => {
                  const ok = j.state === 'ok'
                  const at = j.finishedAt ?? j.startedAt
                  const ago = fmt.relative(at, now)
                  const error = j.error === JOB_LOST ? t('job.lost') : (codeText(t, j.code) ?? j.error)
                  return (
                    <li key={j.id} className="flex items-start gap-3 px-1 py-2.5">
                      {ok ? <CircleCheck className="mt-0.5 h-4 w-4 shrink-0 text-ok-text" /> : <CircleX className="mt-0.5 h-4 w-4 shrink-0 text-bad-text" />}
                      <div className="min-w-0 flex-1">
                        <div dir="auto" className="text-[13px]">
                          {jobTitle(t, j, target(j))}
                        </div>
                        <div className={cx('text-xs text-muted', !ok && 'line-clamp-2')}>
                          {ok && j.finishedAt
                            ? t('tray.took', { ago, time: fmt.clock((Date.parse(j.finishedAt) - Date.parse(j.startedAt)) / 1000) })
                            : !ok && error
                              ? `${ago} · ${error}`
                              : ago}
                        </div>
                      </div>
                      {!ok && (
                        <div className="flex shrink-0 flex-col items-end gap-1">
                          <Button size="sm" variant="ghost" onClick={() => openJob(j.id)}>
                            {t('tray.open')}
                          </Button>
                          {retryForm(j) && (
                            <Button
                              size="sm"
                              variant="ghost"
                              onClick={() => {
                                setShow(false)
                                askRetry(retryForm(j)!)
                              }}
                            >
                              {t(isRefusal(j) ? 'fix.change' : 'job.tryAgain')}
                            </Button>
                          )}
                        </div>
                      )}
                    </li>
                  )
                })}
              </ul>
            </>
          )}
        </section>
      )}
    </div>
  )
}
