import { codeText, type MsgKey, type T } from '../i18n'
import { translate } from '../i18n/translate'
import { JOB_LOST, type ErrorInfo, type Job } from './api'
import { isRefusal } from './fixes'
import type { Format } from './format'
import { installOutcome } from './install'

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
  take_over_xray: 'step.take_over_xray',
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
const REACHED = ['server_has_xray', 'server_has_agent', 'server_is_node']

/** The job failed where the router talks to the node: it could not reach it, or the address isn't on the domestic list. */
export const failedAtLink = (job: Job) =>
  job.state === 'failed' && !REACHED.includes(job.code ?? '') && job.steps.some((s) => s.state === 'failed' && LINK_STEPS.includes(s.key))

// --- what went wrong, said once -----------------------------------------------------------

/**
 * The hub's own words for what failed, without what it did about it: the failed step's text, when the job's
 * error starts with it (the rest being the outcome, which the page words itself), else the job's error.
 */
export function causeOf(job: Job): string {
  const text = job.steps.find((s) => s.state === 'failed')?.detail
  if (text && !text.endsWith('…') && job.error?.startsWith(text)) return text
  return job.error ?? ''
}

/** How a failed job says what went wrong. */
export interface FailureText {
  /** the sentence for it, in the page's words ("" when the failed step already says it) */
  reason: string
  /** the hub's own words, when they say more than `reason` does: what was found, which node, which login */
  detail: string
  /** the failed step shows its own text, because no sentence above says it */
  stepText: boolean
}

const normalize = (s: string) => s.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, ' ').trim()

// the codes whose facts the page words itself, so the hub's English adds nothing to them
const OWN_WORDS = ['server_has_xray', 'host_key_changed', 'port_in_use', 'server_is_node']

/** The sentence a code's facts make, where they say more than the code's own words do. */
function specificReason(t: T, code: string | undefined, info: ErrorInfo | undefined): string {
  if (code === 'port_in_use' && info?.port) {
    // a port is a name to type, not a count: no thousands, and the digits it has in the command that sets it
    return t(info.role === 'agent' ? 'fix.port.agent' : 'fix.port.entry', { port: String(info.port), process: info.process || t('fix.port.another') })
  }
  if (code === 'server_is_node' && info?.nodeName) return t('fix.server.node', { name: info.nodeName })
  return ''
}

/**
 * What a failed job says is wrong, once. A code the page has words for is said by the page, in its language,
 * from the facts the hub sent where there are some; the hub's own sentence then only comes beside it for what
 * it adds. A failure with no code of the page's is the hub's sentence on the step that failed, and in a box
 * only when the step doesn't say it.
 */
export function failureText(t: T, job: Job): FailureText {
  if (job.error === JOB_LOST) return { reason: t('job.lost'), detail: '', stepText: false }
  const cause = causeOf(job)
  const generic = codeText(t, job.code)
  if (!generic) {
    const step = job.steps.find((s) => s.state === 'failed')
    const text = step?.detail
    const covered = !!text && !!job.error && (job.error === text || (!!installOutcome(job) && !text.endsWith('…') && job.error.startsWith(text)))
    return { reason: covered ? '' : cause, detail: '', stepText: true }
  }
  const specific = specificReason(t, job.code, job.errorInfo)
  if (specific) return { reason: specific, detail: '', stepText: false }
  const own = !!job.errorInfo && OWN_WORDS.includes(job.code ?? '')
  const adds = normalize(cause) !== normalize(translate('en', `error.${job.code}` as MsgKey))
  return { reason: generic, detail: adds && !own ? cause : '', stepText: false }
}

/**
 * The title and the line under it for a failed or running install's dialog. A server that can't be used is
 * titled so, with the reason on the line; anything else is the install's title and how far it got.
 */
export function jobHeader(t: T, fmt: Format, job: Job, target: string, now: number): { title: string; line: string } {
  if (isRefusal(job)) return { title: t('job.refused.title'), line: failureText(t, job).reason }
  return { title: jobTitle(t, job, target), line: jobLine(t, fmt, job, now) }
}
