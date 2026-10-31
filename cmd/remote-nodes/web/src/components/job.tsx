import { useConfig } from '../hooks/useConfig'
import { codeText, useLang } from '../i18n'
import { JOB_LOST, type Job } from '../lib/api'
import { installOutcome } from '../lib/install'
import { failedAtLink, stepLabel, stepPercent } from '../lib/jobs'
import { Circle, CircleCheck, CircleX } from './icons'
import { Bar, ErrorBox, Notice, Spinner, cx } from './ui'

/**
 * Whether the failed step's text already says what the job's error does. A failed install's error goes on
 * to say what was done about it, which the page words itself, so the step covering its start is enough.
 */
function covered(job: Job, detail: string | undefined, outcome: boolean) {
  if (!detail || !job.error) return false
  return job.error === detail || (outcome && !detail.endsWith('…') && job.error.startsWith(detail))
}

/**
 * A job's steps as they run, pending ones greyed, its error, what a failed install left behind, and the
 * domestic-list hint after a link failure.
 */
export function JobSteps({ job }: { job: Job | null }) {
  const { t } = useLang()
  const xray = useConfig()?.xray
  if (!job) return <Spinner />
  const failed = job.steps.find((s) => s.state === 'failed')
  const outcome = installOutcome(job)
  // a code the page has words for says it in the page's language
  const worded = codeText(t, job.code)
  // a job the plugin lost track of has no words of its own: the page supplies them
  const error = job.error === JOB_LOST ? t('job.lost') : worded ?? (covered(job, failed?.detail, !!outcome) ? null : job.error)
  return (
    <div className="flex flex-col gap-4">
      <ol className="flex flex-col gap-3">
        {job.steps.map((step, i) => {
          const pct = step.state === 'running' ? stepPercent(step.detail) : null
          return (
            <li key={i} className="flex gap-3">
              <span className="mt-0.5">
                {step.state === 'running' && <Spinner className="text-info" />}
                {step.state === 'ok' && <CircleCheck className="h-4 w-4 text-ok-text" />}
                {step.state === 'failed' && <CircleX className="h-4 w-4 text-bad-text" />}
                {step.state === 'pending' && <Circle className="h-4 w-4 text-border-strong" />}
              </span>
              <div className="min-w-0 flex-1">
                <div className={cx(step.state === 'running' && 'font-semibold', step.state === 'pending' && 'text-faint')}>
                  {stepLabel(t, step, xray)}
                  <span className="sr-only">{t('job.sr', { state: t(`job.state.${step.state}`) })}</span>
                </div>
                {step.detail && step.state !== 'pending' && (
                  // what the hub reports is English or technical, so it reads left to right
                  <div dir="auto" className={cx('whitespace-pre-wrap break-words text-xs', step.state === 'failed' ? 'text-bad-text' : 'text-muted')}>
                    {step.detail}
                  </div>
                )}
                {pct !== null && <Bar pct={pct} className="mt-1.5" />}
              </div>
            </li>
          )
        })}
        {job.state === 'running' && job.steps.length === 0 && (
          <li className="flex gap-3 text-muted">
            <Circle className="h-4 w-4" /> {t('job.starting')}…
          </li>
        )}
      </ol>
      {/* the failed step usually carries the same text already */}
      {job.state === 'failed' && error && error !== failed?.detail && <ErrorBox>{error}</ErrorBox>}
      {outcome && <Notice tone={outcome === 'kept' ? 'warn' : 'info'}>{t(`job.outcome.${outcome}`)}</Notice>}
      {failedAtLink(job) && <Notice tone="warn">{t('job.domesticHint')}</Notice>}
    </div>
  )
}
