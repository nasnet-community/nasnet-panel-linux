import type { Format } from './format'
import type { Job } from './api'
import type { MsgKey, T } from '../i18n'

// The steps the hub reports, by name. The page words them itself, so a name it doesn't
// know shows as the name, capitalised ("agent-upload" becomes "Agent upload").
const STEP_KEYS: Record<string, MsgKey> = {
  release: 'step.release',
  verify: 'step.verify',
  check: 'step.check',
  upload: 'step.upload',
  agent: 'step.agent',
  xray: 'step.xray',
  handshake: 'step.handshake',
  config: 'step.config',
  tunnel: 'step.tunnel',
  cert: 'step.cert',
  reconnect: 'step.reconnect',
  keys: 'step.keys',
  remove: 'step.remove',
  forget: 'step.forget',
}

/** A job step's name ("xray") as the step it is ("Send Xray 26.10.21"). `xray` is the version this plugin installs. */
export function stepLabel(t: T, name: string, xray = ''): string {
  const key = STEP_KEYS[name]
  if (key) return t(key, { version: xray }).trim()
  return name.charAt(0).toUpperCase() + name.slice(1).replace(/[-_]/g, ' ')
}

/** What a job is doing, or how it ended: "Installing Node 3", "Agent updated". */
export function jobTitle(t: T, j: Pick<Job, 'kind' | 'state'>, target = ''): string {
  if (j.kind === 'install') {
    if (!target) return t(j.state === 'ok' ? 'job.installDone' : j.state === 'failed' ? 'job.installFailed' : 'job.installing')
    return t(`job.install.${j.state}`, { target })
  }
  const title = t(`job.${j.kind}.${j.state}` as MsgKey)
  return target ? `${title} · ${target}` : title
}

/** "Step 5 of 11": the running step, else the next one after those done. */
export function jobStep(job: Job): { n: number; m: number } {
  const i = job.steps.findIndex((s) => s.state === 'running')
  const done = job.steps.filter((s) => s.state === 'ok').length
  return { n: i >= 0 ? i + 1 : Math.min(done + 1, job.steps.length), m: job.steps.length }
}

/** The share done of a step whose detail starts with a percentage ("38% · …"). */
export function stepPercent(detail?: string): number | null {
  const m = detail ? /^(\d{1,3})%/.exec(detail) : null
  return m ? Math.min(100, Number(m[1])) : null
}

/** "Step 5 of 8 · 2:14 so far", "Failed at step 4 of 8", "Took 3:41". */
export function jobLine(t: T, fmt: Format, job: Job, now: number): string {
  const { n, m } = jobStep(job)
  const start = Date.parse(job.startedAt)
  if (job.state === 'running') {
    const step = m ? t('job.step', { n, m }) : t('job.starting')
    return start ? t('job.soFar', { step, time: fmt.clock((now - start) / 1000) }) : step
  }
  if (job.state === 'failed') return m ? t('job.failedAt', { n, m }) : t('job.failed')
  const end = job.finishedAt ? Date.parse(job.finishedAt) : 0
  return start && end ? t('job.took', { time: fmt.clock((end - start) / 1000) }) : t('job.done')
}

/** The job failed where the router talks to the node: it could not reach it, or the address isn't on the domestic list. */
export const failedAtLink = (job: Job) =>
  job.state === 'failed' && job.steps.some((s) => (s.name === 'upload' || s.name === 'agent') && s.state === 'failed')

