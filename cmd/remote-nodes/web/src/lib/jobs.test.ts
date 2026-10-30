import { describe, expect, it } from 'vitest'
import { translate } from '../i18n/translate'
import type { Lang } from '../i18n/lang'
import type { Job } from './api'
import { makeFormat } from './format'
import { failedAtLink, jobLine, jobStep, jobTitle, stepLabel, stepPercent } from './jobs'

const job = (o: Partial<Job>): Job => ({ id: 'j1', kind: 'install', state: 'running', steps: [], startedAt: '2026-10-01T00:00:00Z', ...o })
const t = (lang: Lang) => (k: Parameters<typeof translate>[1], p?: Record<string, string | number>) => translate(lang, k, p)

describe('steps', () => {
  it('words the steps the hub names, with the Xray version in the one that sends it', () => {
    expect(stepLabel(t('en'), 'release')).toBe('Fetch the signed release')
    expect(stepLabel(t('en'), 'agent')).toBe('Start the agent')
    expect(stepLabel(t('en'), 'xray', '26.10.21')).toBe('Send Xray 26.10.21')
    // before the config has loaded there is no version, and no stray space
    expect(stepLabel(t('en'), 'xray')).toBe('Send Xray')
    expect(stepLabel(t('fa'), 'xray', '26.10.21')).toBe('ارسال Xray 26.10.21')
  })

  it('shows a name it does not know as the name', () => {
    expect(stepLabel(t('en'), 'agent-upload')).toBe('Agent upload')
    expect(stepLabel(t('fa'), 'rotate_keys')).toBe('Rotate keys')
  })

  it('finds the step a job is on and how far through it is', () => {
    const steps = [
      { name: 'a', state: 'ok' as const },
      { name: 'b', state: 'ok' as const },
      { name: 'c', state: 'running' as const, detail: '62% · 8.4 of 13.6 MB' },
      { name: 'd', state: 'pending' as const },
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
    { name: 'a', state: 'ok' as const },
    { name: 'b', state: 'running' as const },
    { name: 'c', state: 'pending' as const },
  ]

  it('counts the steps and the time so far while it runs', () => {
    expect(jobLine(t('en'), makeFormat('en'), job({ steps }), NOW)).toBe('Step 2 of 3 · 1:02 so far')
    expect(jobLine(t('fa'), makeFormat('fa'), job({ steps }), NOW)).toBe('مرحله ۲ از ۳ · ۱:۰۲ تاکنون')
    expect(jobLine(t('en'), makeFormat('en'), job({}), NOW)).toBe('Starting · 1:02 so far')
  })

  it('says where it failed and how long it took when it is done', () => {
    const failed = [{ name: 'a', state: 'ok' as const }, { name: 'b', state: 'failed' as const }, { name: 'c', state: 'pending' as const }]
    expect(jobLine(t('en'), makeFormat('en'), job({ state: 'failed', steps: failed }), NOW)).toBe('Failed at step 2 of 3')
    expect(jobLine(t('en'), makeFormat('en'), job({ state: 'failed' }), NOW)).toBe('Failed')
    const done = job({ state: 'ok', steps: steps.map((s) => ({ ...s, state: 'ok' as const })), finishedAt: '2026-10-01T00:03:12Z' })
    expect(jobLine(t('en'), makeFormat('en'), done, NOW)).toBe('Took 3:12')
    expect(jobLine(t('fa'), makeFormat('fa'), done, NOW)).toBe('۳:۱۲ طول کشید')
    expect(jobLine(t('en'), makeFormat('en'), job({ state: 'ok' }), NOW)).toBe('Done')
  })
})

describe('a failure at the link', () => {
  const step = (name: string, state: 'ok' | 'failed' | 'pending') => ({ name, state })
  it('is one where the router talks to the node', () => {
    expect(failedAtLink(job({ state: 'failed', steps: [step('release', 'ok'), step('agent', 'failed')] }))).toBe(true)
    expect(failedAtLink(job({ state: 'failed', steps: [step('upload', 'failed')] }))).toBe(true)
    expect(failedAtLink(job({ state: 'failed', steps: [step('handshake', 'failed')] }))).toBe(false)
    expect(failedAtLink(job({ state: 'running', steps: [step('agent', 'failed')] }))).toBe(false)
  })
})
