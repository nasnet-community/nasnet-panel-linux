import type { Format } from './format'
import type { Job } from './api'
import type { MsgKey, T } from '../i18n'

// The steps the hub reports, by key. The hub sends each step with a stable key and its English
// name, the page words the key itself, and a key it doesn't know shows as the hub's name.
// internal/remotenodes/manager/stepkeys.go lists the keys, and a test there fails for one
// that has no words here.
const STEP_KEYS: Record<string, MsgKey> = {
  check_server: 'step.check_server',
  download_agent: 'step.download_agent',
  create_certificate: 'step.create_certificate',
  prepare_server: 'step.prepare_server',
  upload_agent: 'step.upload_agent',
  start_agent: 'step.start_agent',
  check_agent: 'step.check_agent',
  connect_agent: 'step.connect_agent',
  download_xray: 'step.download_xray',
  upload_xray: 'step.upload_xray',
  check_xray: 'step.check_xray',
  choose_target: 'step.choose_target',
  create_keys: 'step.create_keys',
  apply_config: 'step.apply_config',
  save_node: 'step.save_node',
  stop_agent: 'step.stop_agent',
  remove_agent: 'step.remove_agent',
  restart_agent: 'step.restart_agent',
  renew_cert: 'step.renew_cert',
  forget_node: 'step.forget_node',
  clean_server: 'step.clean_server',
  discard_node: 'step.discard_node',
}

/** The words for a step key, or undefined for one the page has none for. */
const stepWords = (key: string) => (Object.prototype.hasOwnProperty.call(STEP_KEYS, key) ? STEP_KEYS[key] : undefined)

/** A job step as the step it is ("Upload Xray 26.10.21"): worded by its key, else the hub's name. `xray` is the version this plugin installs. */
export function stepLabel(t: T, step: { key?: string; name: string }, xray = ''): string {
  const words = step.key ? stepWords(step.key) : undefined
  return words ? t(words, { version: xray }).trim() : step.name
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
  if (job.state === 'failed') {
    // the step that failed, not the one after the steps that worked: a cleanup may have run after it
    const at = job.steps.findIndex((s) => s.state === 'failed')
    return m ? t('job.failedAt', { n: at >= 0 ? at + 1 : n, m }) : t('job.failed')
  }
  const end = job.finishedAt ? Date.parse(job.finishedAt) : 0
  return start && end ? t('job.took', { time: fmt.clock((end - start) / 1000) }) : t('job.done')
}

// The steps where the router itself reaches the node: the SSH login, and the link to the agent.
const LINK_STEPS = ['check_server', 'check_agent', 'connect_agent']

// What the server turned out to have, which the router could only find by reaching it.
const REACHED = ['server_has_xray', 'server_has_agent']

/** The job failed where the router talks to the node: it could not reach it, or the address isn't on the domestic list. */
export const failedAtLink = (job: Job) =>
  job.state === 'failed' && !REACHED.includes(job.code ?? '') && job.steps.some((s) => s.state === 'failed' && LINK_STEPS.includes(s.key))
