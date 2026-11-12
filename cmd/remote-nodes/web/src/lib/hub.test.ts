import { describe, expect, it } from 'vitest'
import type { Lang } from '../i18n/lang'
import { translate, type MsgKey, type Params } from '../i18n/translate'
import { entryAttention, entryChips } from '../screens/Entry'
import { nodeVerdict } from '../screens/NodeStatusTab'
import { makeFormat } from './format'
import { health, issues, nodeNotes } from './health'
import * as hub from './hub-fixtures'
import { failedAtLink, jobLine, jobStep, jobTitle, stepLabel } from './jobs'
import { agentMemory, agentUptime, plot, renewLeadDays, roundTrip, settingsState } from './node'
import { hasLinks, routerHealth, routerNotes, routerVerdict } from './router'
import { nodeInfos, nodeSubscriptionStatus, statusText, subscriptionStatus } from './subscriptions'

// What the hub sends, as hub-fixtures.ts holds it (written by a Go test in internal/remotenodes/api
// from the hub's own setup), run through the page's own code. The file is typed with the page's types,
// so a shape that drifted fails the typecheck; these tests are for what only running it shows, such as
// an empty string where a date is read, or a list that is not one.

const speak = (lang: Lang) => ({ lang, t: (key: MsgKey, params?: Params) => translate(lang, key, params), fmt: makeFormat(lang) })
const en = speak('en')
const fa = speak('fa')
const NOW = Date.parse('2026-10-30T12:00:00Z')

const everyNode = { node: hub.node, installingNode: hub.installingNode, bareNode: hub.bareNode, unfinishedNode: hub.unfinishedNode }

describe('what the hub sends', () => {
  it('has a list wherever the page walks one, empty or not', () => {
    for (const config of [hub.config, hub.bareConfig]) expect(Array.isArray(config.entry.routes)).toBe(true)
    for (const subs of [hub.subscriptions, hub.bareSubscriptions]) for (const s of subs) expect(Array.isArray(s.nodes)).toBe(true)
    for (const job of hub.jobs) expect(Array.isArray(job.steps)).toBe(true)
    expect(Array.isArray(hub.nodeSubscriptions)).toBe(true)
    expect(Array.isArray(hub.traffic.samples)).toBe(true)
    // one node has its events, in a list of nodes none has them
    for (const n of Object.values(everyNode)) expect(Array.isArray(n.events), n.name).toBe(true)
    for (const n of hub.nodes) expect(n.events).toBeUndefined()
  })

  it('writes every time as something the page can read, and leaves the certificate empty only for a node that has none', () => {
    const times: string[] = []
    const walk = (v: unknown, key = '') => {
      if (typeof v === 'string' && /At$|^since$|^notAfter$|^renewsAt$/.test(key) && v) times.push(`${key}=${v}`)
      if (Array.isArray(v)) v.forEach((x) => walk(x, key))
      else if (v && typeof v === 'object') for (const [k, x] of Object.entries(v)) walk(x, k)
    }
    walk(hub)
    expect(times.length).toBeGreaterThan(20)
    for (const t of times) expect(Number.isNaN(Date.parse(t.slice(t.indexOf('=') + 1))), t).toBe(false)
    // a node whose install did not finish has no certificate yet, and the page never reads it
    expect(hub.unfinishedNode.cert).toEqual({ notAfter: '', validDays: 0, renewsAt: '' })
    expect(hub.node.cert.notAfter).not.toBe('')
    expect(renewLeadDays(hub.node.cert)).toBe(30)
  })
})

describe('the nodes', () => {
  it('read as what they are, in both languages, whichever of its parts the hub knows', () => {
    const word = (n: keyof typeof everyNode, lang = en) => health(everyNode[n], lang.t, lang.fmt, NOW).label
    expect(word('node')).toBe('Working')
    expect(word('installingNode')).toBe('Installing')
    expect(word('bareNode')).toBe('Starting')
    expect(word('unfinishedNode')).toBe('Not finished')
    for (const n of Object.keys(everyNode) as (keyof typeof everyNode)[]) expect(word(n, fa), n).not.toBe(word(n))
    expect(issues(hub.unfinishedNode)).toEqual(['notFinished'])
    expect(issues(hub.installingNode)).toEqual([])
  })

  it('give the node page notes and a verdict with no hole in them', () => {
    for (const lang of [en, fa]) {
      for (const [name, n] of Object.entries(everyNode)) {
        for (const note of nodeNotes(n, { ...lang, xray: hub.config.xray, now: NOW })) {
          expect(note.title, name).not.toBe('')
          expect(`${note.title} ${note.detail}`, name).not.toMatch(/undefined|NaN|\{\w+\}/)
        }
      }
      const v = nodeVerdict(hub.node, hub.config.entry, lang.t, lang.fmt, NOW)
      expect(v.title).not.toBe('')
      expect(v.chips.map((c) => c.value)).not.toContain('')
    }
  })

  // the manager has no source for these, and sends 0 and false: the page reads that as not known
  it('shows no zeros for what the hub has no source for', () => {
    const { tunnel, agent } = hub.node
    expect(tunnel.state).toBe('up')
    expect(tunnel.rttMs).toBe(0)
    expect(roundTrip(tunnel)).toBeNull()
    expect(agent).not.toBeNull()
    expect(agentUptime(agent!)).toBeNull()
    expect(agentMemory(agent!)).toBeNull()
    expect(settingsState(agent!)).toBeNull()
    // and what it does send about Xray is shown: it is running, and has been for ten minutes
    expect(agent!.xray).toMatchObject({ running: true, uptimeSec: 600, restarts: 2 })
  })

  it('has the step of an install that is running in its own words', () => {
    const i = hub.installingNode.installing!
    expect(i.steps).toBeGreaterThan(0)
    expect(stepLabel(fa.t, { key: i.key, name: i.label }, hub.config.xray)).toBe('ارسال ایجنت')
    expect(stepLabel(en.t, { key: i.key, name: i.label }, hub.config.xray)).toBe(i.label)
  })
})

describe('the jobs', () => {
  it('show every step in the page\'s words, and every step the hub keys has them', () => {
    for (const job of hub.jobs) {
      expect(jobTitle(en.t, job, job.target)).not.toMatch(/^job\./)
      expect(jobLine(en.t, en.fmt, job, NOW)).not.toMatch(/NaN|undefined/)
      for (const step of job.steps) {
        expect(step.key, step.name).not.toBe('')
        expect(stepLabel(en.t, step, hub.config.xray), step.key).toBe(step.name)
        expect(stepLabel(fa.t, step, hub.config.xray), step.key).not.toBe(step.name)
      }
    }
  })

  it('count the steps of the running install the way the hub counts them', () => {
    const install = hub.jobs.find((j) => j.kind === 'install')!
    expect(install.state).toBe('running')
    const { n, m } = jobStep(install)
    expect({ n, m }).toEqual({ n: hub.installingNode.installing!.step, m: hub.installingNode.installing!.steps })
    expect(install.nodeId).toBe(2)
  })

  it('end with a reason that is the hub\'s English, and name the step that failed', () => {
    const failed = hub.jobs.find((j) => j.state === 'failed' && j.kind === 'update-agent')!
    expect(failed.error).toBe('The connection broke.')
    expect(failed.steps.filter((s) => s.state === 'failed').map((s) => s.key)).toEqual(['upload_agent'])
    // the connection broke while sending, which is not where the router first reaches the node
    expect(failedAtLink(failed)).toBe(false)
    const atLogin = { ...failed, steps: failed.steps.map((s) => (s.key === 'upload_agent' ? { ...s, key: 'check_server' } : s)) }
    expect(failedAtLink(atLogin)).toBe(true)
    const done = hub.jobs.find((j) => j.state === 'ok')!
    expect(done.finishedAt).toBeDefined()
    expect(jobLine(en.t, en.fmt, done, NOW)).toMatch(/^Took /)
  })
})

describe('the subscriptions', () => {
  it('say where each stands, with what the hub knows of the nodes and with what it does not', () => {
    const nodes = nodeInfos(hub.nodes)
    const [on, off] = hub.subscriptions
    expect(subscriptionStatus(on, nodes, en.t, en.fmt, 'en', NOW).key).toBe('online')
    expect(subscriptionStatus(off, nodes, en.t, en.fmt, 'en', NOW).key).toBe('off')
    // no manager: nothing is known of a subscription that is switched on, and the page reads that
    const [unknown, bareOff] = hub.bareSubscriptions
    expect(unknown.online).toBeNull()
    expect(subscriptionStatus(unknown, nodes, en.t, en.fmt, 'en', NOW).key).toBe('unknown')
    expect(subscriptionStatus(bareOff, nodes, en.t, en.fmt, 'en', NOW).key).toBe('off')
    for (const s of hub.nodeSubscriptions) expect(nodeSubscriptionStatus(s, en.t, en.fmt, NOW).label).not.toBe('')
  })

  it('leave an installing node out of the columns, and a node that never finished out of the choices', () => {
    const infos = nodeInfos([hub.node, hub.installingNode, hub.unfinishedNode])
    expect(infos.map((n) => [n.name, n.installing])).toEqual([
      [hub.node.name, false],
      [hub.installingNode.name, true],
    ])
  })
})

describe('the router', () => {
  it('has nothing to complain of when it works, and says nothing of RouterOS before it has reported', () => {
    expect(entryAttention(hub.config.entry, en.t)).toEqual([])
    const bare = entryAttention(hub.bareConfig.entry, en.t).map((i) => i.title)
    expect(bare).toEqual(['This router has no Domestic link', 'This router has no VPN'])
    expect(hub.bareConfig.entry.routerOS).toMatchObject({ version: '', ok: false })
    expect(entryChips(hub.bareConfig.entry, en.t).map((c) => c.value)).toEqual(['missing', 'missing', 'stopped'])
  })

  it('draws a chart from the samples it has, and from none', () => {
    const drawn = plot(hub.traffic, Date.parse(hub.traffic.samples[1].at), 300, 80)
    expect(drawn.drawn).toBe(true)
    expect(drawn.last).toEqual({ up: 9, down: 10 })
    expect(plot({ stepSec: 5, samples: [] }, NOW, 300, 80).drawn).toBe(false)
  })
})

describe("the router's own entry", () => {
  it('reads as working, with a name and a port for its links, and as off on a router that never switched it on', () => {
    expect(routerHealth(hub.local, hub.config.entry, en.t)).toEqual({ tone: 'online', label: 'Working', reason: '' })
    expect(routerNotes(hub.local, hub.config.entry, en.t)).toEqual([])
    expect(hasLinks(hub.local)).toBe(true)
    expect(hub.config.local).toEqual({ enabled: true, state: 'working' })

    expect(routerHealth(hub.bareLocal, hub.bareConfig.entry, en.t).label).toBe('Off')
    expect(hasLinks(hub.bareLocal)).toBe(false)
    expect(hub.bareLocal.port).toBeNull()
    expect(hub.bareConfig.local).toEqual({ enabled: false, state: 'off' })
  })

  it('says when it last reported, and has its events in a list either way', () => {
    const v = routerVerdict(hub.local, hub.config.entry, en.t, en.fmt, NOW)
    expect(v.title).toBe('Everything is working')
    expect(v.note).toBe('Last report 60 s ago')
    expect(v.chips.map((c) => c.value)).toEqual(['open', 'set', 'running'])
    for (const l of [hub.local, hub.bareLocal]) expect(Array.isArray(l.events)).toBe(true)
  })

  it('has its list of subscriptions in the words for the router', () => {
    const subs = hub.localSubscriptions.map((s) => [s.name, statusText(nodeSubscriptionStatus(s, en.t, en.fmt, NOW, true))])
    expect(subs).toEqual([
      ['علی', 'Online · 2 connections'],
      ['sara', 'Off'],
    ])
  })

  it('gives each subscription its row on the entry while the entry is on, and none while it is off', () => {
    for (const s of hub.subscriptions) expect(s.local, s.name).not.toBeNull()
    for (const s of hub.bareSubscriptions) expect(s.local, s.name).toBeNull()
  })
})
