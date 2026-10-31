import { useState } from 'react'
import { useConfig } from '../hooks/useConfig'
import { useLang, type T } from '../i18n'
import type { Job, JobStep } from '../lib/api'
import { isRefusal } from '../lib/fixes'
import { installOutcome, shownSteps } from '../lib/install'
import { failedAtLink, failureText, stepLabel, stepPercent } from '../lib/jobs'
import { Circle, CircleCheck, CircleX, ChevronDown } from './icons'
import { Bar, ErrorBox, Notice, Spinner, cx } from './ui'

/**
 * A job's steps as they run, and when it failed what went wrong once, what was done about it, and what a
 * failed install left behind. The steps a failed job never got to are one line that opens, and a step that
 * says nothing a person needs (the one that forgets a node that was never made) is left out.
 */
export function JobSteps({ job }: { job: Job | null }) {
  const { t } = useLang()
  const xray = useConfig()?.xray
  const [open, setOpen] = useState(false)
  if (!job) return <Spinner />
  const failed = job.state === 'failed'
  const outcome = installOutcome(job)
  const text = failureText(t, job)
  const steps = shownSteps(job)
  const waiting = failed ? steps.filter((s) => s.state === 'pending') : []
  const first = waiting.length ? steps.indexOf(waiting[0]) : -1
  // a refusal says why in the dialog's title, where it reads first
  const reasonHere = !isRefusal(job)
  return (
    <div className="flex flex-col gap-4">
      <ol className="flex flex-col gap-3">
        {steps.map((step, i) => {
          if (failed && step.state === 'pending') {
            return i === first ? (
              <li key="waiting" className="flex flex-col gap-3">
                <button
                  type="button"
                  aria-expanded={open}
                  onClick={() => setOpen(!open)}
                  className="flex min-h-[44px] items-center gap-3 text-start text-faint focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent sm:min-h-0"
                >
                  <Circle className="h-4 w-4 shrink-0 text-border-strong" />
                  <span>{t('job.notStarted', { n: waiting.length })}</span>
                  <ChevronDown className={cx('h-4 w-4 shrink-0 transition', open && 'rotate-180')} />
                </button>
                {open && (
                  <ol className="ms-7 flex flex-col gap-3">
                    {waiting.map((w, k) => (
                      <StepRow key={k} step={w} t={t} xray={xray} stepText />
                    ))}
                  </ol>
                )}
              </li>
            ) : null
          }
          return <StepRow key={i} step={step} t={t} xray={xray} stepText={text.stepText} />
        })}
        {job.state === 'running' && job.steps.length === 0 && (
          <li className="flex gap-3 text-muted">
            <Circle className="h-4 w-4" /> {t('job.starting')}…
          </li>
        )}
      </ol>
      {failed && reasonHere && text.reason && <ErrorBox>{text.reason}</ErrorBox>}
      {failed && text.detail && (
        // the hub's own words, English or technical, for what the sentence above doesn't say
        <p dir="auto" className="whitespace-pre-wrap break-words text-xs text-muted">
          {text.detail}
        </p>
      )}
      {job.errorInfo?.tookOver && <Notice tone="warn">{t('job.tookOver')}</Notice>}
      {outcome && <Notice tone={outcome === 'kept' ? 'warn' : 'info'}>{t(`job.outcome.${outcome}`)}</Notice>}
      {failedAtLink(job) && <Notice tone="warn">{t('job.domesticHint')}</Notice>}
    </div>
  )
}

/** One step: its mark, its words, and its text when there is some a person needs. */
function StepRow({ step, t, xray, stepText }: { step: JobStep; t: T; xray?: string; stepText: boolean }) {
  const pct = step.state === 'running' ? stepPercent(step.detail) : null
  // the text of a step that failed is in a sentence of the page's own, once, when the page has one
  const showDetail = step.detail && step.state !== 'pending' && (step.state !== 'failed' || stepText)
  return (
    <li className="flex gap-3">
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
        {showDetail && (
          // what the hub reports is English or technical, so it reads left to right
          <div dir="auto" className={cx('whitespace-pre-wrap break-words text-xs', step.state === 'failed' ? 'text-bad-text' : 'text-muted')}>
            {step.detail}
          </div>
        )}
        {pct !== null && <Bar pct={pct} className="mt-1.5" />}
      </div>
    </li>
  )
}
