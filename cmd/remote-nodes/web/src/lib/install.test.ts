import { describe, expect, it } from 'vitest'
import { codeText } from '../i18n'
import type { Lang } from '../i18n/lang'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import type { Job, JobStep } from './api'
import { makeFormat } from './format'
import * as hub from './hub-fixtures'
import { installOutcome, isUnfinished, realNodes, retryForm } from './install'
import { failedAtLink, jobLine } from './jobs'
import { nodeInfos } from './subscriptions'

const speak = (lang: Lang) => ({ t: (key: MsgKey, params?: Params) => translate(lang, key, params), fmt: makeFormat(lang) })
const en = speak('en')
const fa = speak('fa')

const step = (key: string, state: JobStep['state']): JobStep => ({ key, name: key, state })
const FORM = { name: 'Frankfurt', host: '203.0.113.9', sshPort: 2222, sshUser: 'admin', entryPort: 8443 }

/** A failed install whose last steps are these, after one that failed at the upload. */
const failedInstall = (...tail: JobStep[]): Job => ({
  id: 'j',
  kind: 'install',
  state: 'failed',
  nodeId: 3,
  target: 'Frankfurt',
  startedAt: '2026-10-30T12:00:00Z',
  finishedAt: '2026-10-30T12:01:00Z',
  error: "Couldn't upload the agent: the connection broke. The server was cleaned up and nothing was kept.",
  steps: [step('check_server', 'ok'), step('upload_agent', 'failed'), step('start_agent', 'pending'), ...tail],
  install: FORM,
})

describe('an unfinished install', () => {
  it('is what the hub sends with its reason, and also a record neither set up nor installing', () => {
    expect(isUnfinished(hub.stoppedNode)).toBe(true)
    expect(isUnfinished(hub.unfinishedNode)).toBe(true)
    expect(isUnfinished(hub.node)).toBe(false)
    expect(isUnfinished(hub.installingNode)).toBe(false)
    expect(realNodes(hub.nodes).map((n) => n.id)).toEqual([1, 2])
  })

  it('is no node a subscription is offered on', () => {
    expect(nodeInfos(hub.nodes).map((n) => n.id)).toEqual([1, 2])
  })
})

describe('a failed install', () => {
  it('ends cleaned up, untouched or kept, which its last steps say', () => {
    expect(installOutcome(failedInstall(step('clean_server', 'ok'), step('discard_node', 'ok')))).toBe('cleaned')
    expect(installOutcome(failedInstall(step('discard_node', 'ok')))).toBe('untouched')
    expect(installOutcome(failedInstall(step('clean_server', 'failed')))).toBe('kept')
    expect(installOutcome(failedInstall(step('clean_server', 'ok'), step('discard_node', 'failed')))).toBe('kept')
    // one that never got there (a job the router lost), and any other job, has none
    expect(installOutcome(failedInstall())).toBeNull()
    expect(installOutcome({ ...failedInstall(step('discard_node', 'ok')), kind: 'remove' })).toBeNull()
    expect(installOutcome({ ...failedInstall(step('discard_node', 'ok')), state: 'ok' })).toBeNull()
    // the hub's own refused install
    const refused = hub.jobs.find((j) => j.code === 'server_has_xray')!
    expect(installOutcome(refused)).toBe('untouched')
  })

  it('offers Try again with what was asked, but not when it was kept for a cleanup', () => {
    expect(retryForm(failedInstall(step('clean_server', 'ok'), step('discard_node', 'ok')))).toEqual(FORM)
    expect(retryForm(failedInstall(step('clean_server', 'failed')))).toBeNull()
    expect(retryForm({ ...failedInstall(step('discard_node', 'ok')), install: undefined })).toBeNull()
    expect(retryForm({ ...failedInstall(step('discard_node', 'ok')), state: 'running' })).toBeNull()
    const refused = hub.jobs.find((j) => j.code === 'server_has_xray')!
    expect(retryForm(refused)).toEqual({ name: 'third', host: '203.0.113.4', sshPort: 22, sshUser: 'root', entryPort: 443 })
  })

  it('says the step it failed at, whatever ran after it', () => {
    const job = failedInstall(step('clean_server', 'ok'), step('discard_node', 'ok'))
    expect(jobLine(en.t, en.fmt, job, 0)).toBe('Failed at step 2 of 5')
    expect(jobLine(fa.t, fa.fmt, job, 0)).toBe('ناموفق در مرحله ۲ از ۵')
  })

  it("has no domestic-list hint when the router reached the server to refuse it", () => {
    const refused = hub.jobs.find((j) => j.code === 'server_has_xray')!
    expect(failedAtLink(refused)).toBe(false)
    expect(failedAtLink({ ...refused, code: undefined })).toBe(true)
    expect(failedAtLink({ ...refused, code: 'node_unreachable' })).toBe(true)
  })

  it('is refused in words of its own for a server with an Xray or an agent', () => {
    expect(codeText(en.t, 'server_has_xray')).toBe('This server already runs an Xray that Remote Nodes did not install. Use a server without one.')
    expect(codeText(fa.t, 'server_has_agent')).toContain('Remote Nodes')
    expect(codeText(en.t, 'internal')).toBeNull()
    expect(codeText(en.t, undefined)).toBeNull()
  })
})
