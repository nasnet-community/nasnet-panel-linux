import { describe, expect, it } from 'vitest'
import type { Lang } from '../i18n/lang'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import { JOB_LOST, type Job, type JobStep } from './api'
import { makeFormat } from './format'
import * as hub from './hub-fixtures'
import { shownSteps } from './install'
import { causeOf, failureText, jobHeader } from './jobs'

const speak = (lang: Lang) => ({ t: (key: MsgKey, params?: Params) => translate(lang, key, params), fmt: makeFormat(lang) })
const en = speak('en')
const fa = speak('fa')

const step = (key: string, state: JobStep['state'], detail?: string): JobStep => ({ key, name: key, state, ...(detail ? { detail } : {}) })

const NOTHING = 'Nothing was changed on the server, and nothing was kept.'

/** A failed install that stopped at its first step, as the hub words it, with the outcome after the cause. */
const refused = (code: string | undefined, cause: string, extra: Partial<Job> = {}): Job => ({
  id: 'j',
  kind: 'install',
  state: 'failed',
  nodeId: 3,
  target: 'Frankfurt',
  startedAt: '2026-10-30T12:00:00Z',
  code,
  error: `${cause} ${NOTHING}`,
  steps: [step('check_server', 'failed', cause), step('download_agent', 'pending'), step('save_node', 'pending'), step('discard_node', 'ok')],
  install: { name: 'Frankfurt', host: '203.0.113.9', sshPort: 22, sshUser: 'root', entryPort: 443 },
  ...extra,
})

describe('what a failed install says is wrong, once', () => {
  it("is the hub's cause without what was done about it", () => {
    const job = refused('node_unreachable', "The agent doesn't answer on TCP port 9090.")
    expect(causeOf(job)).toBe("The agent doesn't answer on TCP port 9090.")
    // a cause the job's step cut short is not trusted to be the whole of it
    const cut = refused(undefined, 'x'.repeat(10), { steps: [step('check_server', 'failed', 'long text…')] })
    expect(causeOf(cut)).toBe(cut.error)
    expect(causeOf({ ...cut, error: undefined })).toBe('')
  })

  it("is the page's sentence for a code it has words for, in its language, and the failed step keeps quiet", () => {
    const job = refused('node_unreachable', "The agent doesn't answer on TCP port 9090 (dial tcp 203.0.113.9:9090: i/o timeout).")
    const text = failureText(en.t, job)
    expect(text.reason).toBe("The router can't reach this node. Check that it is on and its address is on the domestic list.")
    expect(text.stepText).toBe(false)
    expect(failureText(fa.t, job).reason).toBe(fa.t('error.node_unreachable'))
    expect(failureText(fa.t, job).reason).not.toBe(text.reason)
  })

  it("puts the hub's own sentence beside it only where it says more", () => {
    // what the page says already: nothing more to show, whatever the language
    const same = refused('login_refused', en.t('error.login_refused'))
    expect(failureText(en.t, same).detail).toBe('')
    expect(failureText(fa.t, same).detail).toBe('')
    // punctuation and case are no difference
    expect(failureText(en.t, refused('login_refused', en.t('error.login_refused').toUpperCase().replace(/\.$/, ''))).detail).toBe('')
    // what the hub says that the page's sentence doesn't: the hub's, in English, as it is
    const says = refused('needs_root', "This login isn't root, and the server has no sudo. Sign in as root, or as a user that can use sudo.")
    expect(failureText(en.t, says).detail).toBe("This login isn't root, and the server has no sudo. Sign in as root, or as a user that can use sudo.")
    expect(failureText(fa.t, says).detail).toBe(failureText(en.t, says).detail)
    expect(failureText(fa.t, says).reason).toBe(fa.t('error.needs_root'))
  })

  it("leaves the facts to the page's own words when the hub sent them, and does not repeat the hub's sentence", () => {
    const xray = refused('server_has_xray', 'This server already runs an Xray (/usr/local/bin/xray). It is Xray 26.9.1. Use a server without one, or take it over.', {
      errorInfo: { found: ['binary'], xrayVersion: '26.9.1' },
    })
    expect(failureText(en.t, xray)).toEqual({ reason: en.t('error.server_has_xray'), detail: '', stepText: false })
    // from a hub that sends no facts the same sentence is shown beside the page's
    const bare = { ...xray, errorInfo: undefined }
    expect(failureText(en.t, bare).detail).toBe(causeOf(bare))
    // a port, said with who holds it
    const port = refused('port_in_use', 'TCP port 443, which the entry needs, is already in use on the server by nginx.', { errorInfo: { port: 443, role: 'entry', process: 'nginx' } })
    expect(failureText(en.t, port)).toEqual({ reason: 'Port 443 on the server is used by nginx.', detail: '', stepText: false })
    expect(failureText(en.t, { ...port, errorInfo: { port: 443, role: 'entry' } }).reason).toBe('Port 443 on the server is used by another program.')
    expect(failureText(en.t, { ...port, errorInfo: { port: 9090, role: 'agent', process: 'python3' } }).reason).toContain('Port 9090, which the agent uses, is used by python3')
    expect(failureText(en.t, { ...port, errorInfo: { port: 9090, role: 'agent', process: 'python3' } }).reason).toContain("can't be set for one node")
    expect(failureText(fa.t, port).reason).toContain('443')
    expect(failureText(fa.t, port).reason).toContain('nginx')
    // the node a server already is
    const node = refused('server_is_node', 'This server is already the node "Frankfurt" here.', { errorInfo: { nodeId: 3, nodeName: 'Frankfurt' } })
    expect(failureText(en.t, node).reason).toContain('already the node Frankfurt here')
    expect(failureText(en.t, node).detail).toBe('')
    // the keys are shown by the page itself
    const key = refused('host_key_changed', 'The SSH host key of 203.0.113.9 has changed.', { errorInfo: { pinnedKey: 'SHA256:a', shownKey: 'SHA256:b' } })
    expect(failureText(en.t, key).detail).toBe('')
    expect(failureText(en.t, { ...key, errorInfo: undefined }).detail).toBe('The SSH host key of 203.0.113.9 has changed.')
  })

  it('is the hub\'s sentence on the step for a failure the page has no words for, and a box only when the step does not say it', () => {
    const plain = refused(undefined, 'Something odd happened.')
    expect(failureText(en.t, plain)).toEqual({ reason: '', detail: '', stepText: true })
    // a job-level error that its step does not carry is shown, and the step still says its own
    const apart = { ...plain, error: 'The job itself broke.' }
    expect(failureText(en.t, apart)).toEqual({ reason: 'The job itself broke.', detail: '', stepText: true })
    // a job the router lost has words of the page's own
    expect(failureText(en.t, { ...plain, error: JOB_LOST }).reason).toBe(en.t('job.lost'))
    expect(failureText(fa.t, { ...plain, error: JOB_LOST }).reason).toBe(fa.t('job.lost'))
  })

  it('names the Reality target the node could not use, and gives the node\'s own reason beside it', () => {
    const why = "It doesn't use TLS 1.3."
    const cause = `The site www.example.com:443 doesn't work as a Reality target from this server: it doesn't use TLS 1.3.`
    const job = refused('reality_target_failed', cause, { errorInfo: { target: 'www.example.com:443', reason: why } })
    const text = failureText(en.t, job)
    expect(text).toEqual({ reason: "The Reality target www.example.com:443 doesn't work from this server.", detail: why, stepText: false })
    // the page's sentence is in the page's language, the node's reason is its own English
    const persian = failureText(fa.t, job)
    expect(persian.reason).toBe(fa.t('fix.target.failed', { target: 'www.example.com:443' }))
    expect(persian.reason).not.toBe(text.reason)
    expect(persian.detail).toBe(why)
    // the node chose among its sites and none worked: how many, and no reason to add
    const none = refused('reality_target_failed', 'None of the 8 sites tried works as a Reality target from this server.', { errorInfo: { tried: 8 } })
    expect(failureText(en.t, none)).toEqual({ reason: 'None of the 8 sites the node tried works as a Reality target from this server.', detail: '', stepText: false })
    // from a hub that sends no facts: the page's sentence for the code, and the hub's own beside it
    const bare = refused('reality_target_failed', cause)
    expect(failureText(en.t, bare)).toEqual({ reason: en.t('error.reality_target_failed'), detail: cause, stepText: false })
    // and it is no refusal of the server: the title is the failure's
    expect(jobHeader(en.t, en.fmt, job, 'Frankfurt', 0).title).toBe('Installation failed · Frankfurt')
  })

  it('says it from the hub\'s own failed target', () => {
    const job = hub.jobs.find((j) => j.code === 'reality_target_failed')!
    expect(failureText(en.t, job)).toEqual({ reason: "The Reality target www.example.com:443 doesn't work from this server.", detail: "It doesn't use TLS 1.3.", stepText: false })
  })

  it('says it from the hub\'s own refused install', () => {
    const job = hub.jobs.find((j) => j.code === 'server_has_xray')!
    expect(failureText(en.t, job).reason).toBe('This server already runs an Xray that Remote Nodes did not install. Use a server without one, or take it over.')
    expect(failureText(en.t, job).detail).toBe('')
  })
})

describe("a failed install's title", () => {
  it('says a server that is refused for what it is cannot be used, with the reason on the line', () => {
    for (const code of ['server_has_xray', 'server_has_agent', 'server_is_node']) {
      const h = jobHeader(en.t, en.fmt, refused(code, 'x'), '', 0)
      expect(h.title, code).toBe("This server can't be used")
      expect(h.line, code).toBe(failureText(en.t, refused(code, 'x')).reason)
      expect(h.line, code).toBe(en.t(`error.${code}` as MsgKey))
    }
    expect(jobHeader(fa.t, fa.fmt, refused('server_has_xray', 'x'), '', 0).title).toBe('این سرور قابل استفاده نیست')
  })

  it('stays the failure and how far it got for any other', () => {
    const h = jobHeader(en.t, en.fmt, refused('xray_not_stopped', 'x'), '', 0)
    expect(h).toEqual({ title: 'Installation failed', line: 'Failed at step 1 of 4' })
    expect(jobHeader(en.t, en.fmt, refused('port_in_use', 'x'), 'Frankfurt', 0).title).toBe('Installation failed · Frankfurt')
    expect(jobHeader(fa.t, fa.fmt, refused(undefined, 'x'), '', 0).line).toBe('ناموفق در مرحله ۱ از ۴')
    // and a job that is not a failed install is never "refused"
    expect(jobHeader(en.t, en.fmt, { ...refused('server_has_xray', 'x'), state: 'running' }, '', 0).title).toBe('Installing a node')
  })
})

describe('the steps a failed install shows', () => {
  it('leave out the one that forgets a node that was never made, and only then', () => {
    const untouched = refused('server_has_xray', 'x')
    expect(shownSteps(untouched).map((s) => s.key)).toEqual(['check_server', 'download_agent', 'save_node'])
    // the job keeps it
    expect(untouched.steps.map((s) => s.key)).toContain('discard_node')
    // after a cleanup it is the end of what was done, and is shown
    const cleaned = refused(undefined, 'x', { steps: [step('upload_agent', 'failed'), step('clean_server', 'ok'), step('discard_node', 'ok')] })
    expect(shownSteps(cleaned).map((s) => s.key)).toEqual(['upload_agent', 'clean_server', 'discard_node'])
    // and when it is the thing that failed
    const kept = refused(undefined, 'x', { steps: [step('upload_agent', 'failed'), step('clean_server', 'ok'), step('discard_node', 'failed')] })
    expect(shownSteps(kept).map((s) => s.key)).toContain('discard_node')
    // a job that is not an install has its own
    expect(shownSteps({ ...untouched, kind: 'remove' }).map((s) => s.key)).toContain('discard_node')
  })
})
