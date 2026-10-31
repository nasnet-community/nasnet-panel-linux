import { describe, expect, it } from 'vitest'
import { translate } from '../i18n/translate'
import type { Lang } from '../i18n/lang'
import type { Job } from './api'
import { makeFormat } from './format'
import { failedAtLink, jobLine, jobStep, jobTitle, stepLabel, stepPercent } from './jobs'

// the steps of every plan the hub has, as it names them: key, then English name
// (internal/remotenodes/manager/stepkeys.go is the table these are checked against there)
const HUB_STEPS: Record<string, string> = {
  check_server: 'Check the server',
  download_agent: 'Download the agent',
  create_certificate: 'Create the certificate',
  prepare_server: 'Prepare the server',
  upload_agent: 'Upload the agent',
  start_agent: 'Start the agent',
  check_agent: 'Check the agent',
  connect_agent: 'Connect to the agent',
  download_xray: 'Download Xray',
  upload_xray: 'Upload Xray',
  check_xray: 'Check Xray',
  choose_target: 'Choose a handshake target',
  create_keys: 'Create the keys',
  apply_config: 'Apply the configuration',
  save_node: 'Save the node',
  stop_agent: 'Stop the agent',
  remove_agent: 'Remove the agent',
  restart_agent: 'Restart the agent',
  renew_cert: 'Renew the certificate',
  forget_node: 'Forget the node',
  clean_server: 'Clean up the server',
  discard_node: 'Discard the node',
}

const job = (o: Partial<Job>): Job => ({ id: 'j1', kind: 'install', state: 'running', steps: [], startedAt: '2026-10-01T00:00:00Z', ...o })
const t = (lang: Lang) => (k: Parameters<typeof translate>[1], p?: Record<string, string | number>) => translate(lang, k, p)

describe('steps', () => {
  it('words the steps the hub keys, with the Xray version in the ones that name it', () => {
    expect(stepLabel(t('en'), { key: 'download_agent', name: 'Download the agent' })).toBe('Download the agent')
    expect(stepLabel(t('en'), { key: 'upload_xray', name: 'Upload Xray' }, '26.10.21')).toBe('Upload Xray 26.10.21')
    // before the config has loaded there is no version, and no stray space
    expect(stepLabel(t('en'), { key: 'upload_xray', name: 'Upload Xray' })).toBe('Upload Xray')
    expect(stepLabel(t('fa'), { key: 'upload_xray', name: 'Upload Xray' }, '26.10.21')).toBe('ارسال Xray 26.10.21')
    expect(stepLabel(t('fa'), { key: 'save_node', name: 'Save the node' })).toBe('ذخیرهٔ نود')
  })

  it('shows the hub\'s name for a key it has no words for, and for a step with no key', () => {
    expect(stepLabel(t('fa'), { key: 'rotate_the_moon', name: 'Rotate the moon' })).toBe('Rotate the moon')
    expect(stepLabel(t('en'), { name: 'Rotate the moon' })).toBe('Rotate the moon')
    expect(stepLabel(t('en'), { key: '', name: 'Rotate the moon' })).toBe('Rotate the moon')
    // a key is looked up as a key, not as a property of the table
    expect(stepLabel(t('en'), { key: 'constructor', name: 'Constructor' })).toBe('Constructor')
    expect(stepLabel(t('en'), { key: 'toString', name: 'To string' })).toBe('To string')
  })

  it('has words in both languages for every key, and none of them is the key or the English one', () => {
    for (const [key, name] of Object.entries(HUB_STEPS)) {
      const en = stepLabel(t('en'), { key, name }, '26.10.21')
      const fa = stepLabel(t('fa'), { key, name }, '26.10.21')
      // the page's English is the hub's, with the Xray version added to the steps that name Xray
      expect(en, key).toBe(name.endsWith('Xray') ? `${name} 26.10.21` : name)
      expect(fa, key).not.toBe(en)
      expect(fa, key).not.toMatch(/^step\./)
    }
  })

  it('finds the step a job is on and how far through it is', () => {
    const steps = [
      { key: 'a', name: 'a', state: 'ok' as const },
      { key: 'b', name: 'b', state: 'ok' as const },
      { key: 'c', name: 'c', state: 'running' as const, detail: '62% · 8.4 of 13.6 MB' },
      { key: 'd', name: 'd', state: 'pending' as const },
    ]
    expect(jobStep(job({ steps }))).toEqual({ n: 3, m: 4 })
    // between steps: the next one after those done
    expect(jobStep(job({ steps: steps.map((s) => (s.state === 'running' ? { ...s, state: 'pending' as const } : s)) }))).toEqual({ n: 3, m: 4 })
    expect(jobStep(job({ steps: [] }))).toEqual({ n: 0, m: 0 })
    expect(stepPercent('62% · 8.4 of 13.6 MB')).toBe(62)
    expect(stepPercent('100%')).toBe(100)
    expect(stepPercent('250% sent')).toBe(100)
    expect(stepPercent('v1.3.0')).toBeNull()
    expect(stepPercent(undefined)).toBeNull()
  })
})

describe('titles', () => {
  it('names an install by its node, and says how it ended', () => {
    expect(jobTitle(t('en'), job({}), 'Node 3')).toBe('Installing Node 3')
    expect(jobTitle(t('en'), job({ state: 'ok' }), 'Node 3')).toBe('Node 3 added')
    expect(jobTitle(t('en'), job({ state: 'failed' }), 'Node 3')).toBe('Installation failed · Node 3')
    expect(jobTitle(t('en'), job({ state: 'ok' }))).toBe('Node added')
    expect(jobTitle(t('fa'), job({}), 'نود ۳')).toBe('در حال نصب نود ۳')
  })

  it('puts the node after the title of any other job', () => {
    expect(jobTitle(t('en'), job({ kind: 'update-agent', state: 'ok' }), 'Node 1')).toBe('Agent updated · Node 1')
    expect(jobTitle(t('en'), job({ kind: 'update-xray', state: 'running' }))).toBe('Updating Xray')
    expect(jobTitle(t('en'), job({ kind: 'rotate-keys', state: 'failed' }), 'Node 2')).toBe('Rotating keys failed · Node 2')
  })

  it('has a title in both languages for every kind and state', () => {
    const kinds: Job['kind'][] = ['install', 'update-agent', 'update-xray', 'renew-cert', 'rotate-keys', 'change-settings', 'remove']
    for (const lang of ['en', 'fa'] as Lang[])
      for (const kind of kinds)
        for (const state of ['running', 'ok', 'failed'] as const) {
          const title = jobTitle(t(lang), job({ kind, state }), 'X')
          expect(title, `${kind} ${state} ${lang}`).not.toMatch(/^job\./)
        }
  })
})

describe('the line under a job\'s title', () => {
  const NOW = Date.parse('2026-10-01T00:01:02Z')
  const steps = [
    { key: 'a', name: 'a', state: 'ok' as const },
    { key: 'b', name: 'b', state: 'running' as const },
    { key: 'c', name: 'c', state: 'pending' as const },
  ]

  it('counts the steps and the time so far while it runs', () => {
    expect(jobLine(t('en'), makeFormat('en'), job({ steps }), NOW)).toBe('Step 2 of 3 · 1:02 so far')
    expect(jobLine(t('fa'), makeFormat('fa'), job({ steps }), NOW)).toBe('مرحله ۲ از ۳ · ۱:۰۲ تاکنون')
    expect(jobLine(t('en'), makeFormat('en'), job({}), NOW)).toBe('Starting · 1:02 so far')
  })

  it('says where it failed and how long it took when it is done', () => {
    const failed = [{ key: 'a', name: 'a', state: 'ok' as const }, { key: 'b', name: 'b', state: 'failed' as const }, { key: 'c', name: 'c', state: 'pending' as const }]
    expect(jobLine(t('en'), makeFormat('en'), job({ state: 'failed', steps: failed }), NOW)).toBe('Failed at step 2 of 3')
    expect(jobLine(t('en'), makeFormat('en'), job({ state: 'failed' }), NOW)).toBe('Failed')
    const done = job({ state: 'ok', steps: steps.map((s) => ({ ...s, state: 'ok' as const })), finishedAt: '2026-10-01T00:03:12Z' })
    expect(jobLine(t('en'), makeFormat('en'), done, NOW)).toBe('Took 3:12')
    expect(jobLine(t('fa'), makeFormat('fa'), done, NOW)).toBe('۳:۱۲ طول کشید')
    expect(jobLine(t('en'), makeFormat('en'), job({ state: 'ok' }), NOW)).toBe('Done')
  })
})

describe('a failure at the link', () => {
  const step = (key: string, state: 'ok' | 'failed' | 'pending') => ({ key, name: HUB_STEPS[key] ?? key, state })
  it('is one where the router itself reaches the node: the SSH login and the link to the agent', () => {
    expect(failedAtLink(job({ state: 'failed', steps: [step('check_server', 'failed'), step('download_agent', 'pending')] }))).toBe(true)
    expect(failedAtLink(job({ state: 'failed', steps: [step('check_server', 'ok'), step('check_agent', 'failed')] }))).toBe(true)
    expect(failedAtLink(job({ state: 'failed', steps: [step('connect_agent', 'failed')] }))).toBe(true)
  })

  it('is not one at any other step, however it fails', () => {
    for (const key of Object.keys(HUB_STEPS).filter((k) => !['check_server', 'check_agent', 'connect_agent'].includes(k)))
      expect(failedAtLink(job({ state: 'failed', steps: [step(key, 'failed')] })), key).toBe(false)
  })

  it('is not one for a step that worked, a job still running, or the names the page used to expect', () => {
    expect(failedAtLink(job({ state: 'failed', steps: [step('check_server', 'ok'), step('save_node', 'failed')] }))).toBe(false)
    expect(failedAtLink(job({ state: 'running', steps: [step('check_agent', 'failed')] }))).toBe(false)
    expect(failedAtLink(job({ state: 'failed', steps: [{ key: 'upload', name: 'upload', state: 'failed' }, { key: 'agent', name: 'agent', state: 'failed' }] }))).toBe(false)
  })
})
