import { readFileSync } from 'node:fs'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { InstallRequest, Job, JobStep } from './api'
import {
  REFUSALS,
  fixFor,
  forAnotherTry,
  isRefusal,
  otherPort,
  withEntryPort,
  withHost,
  withHostKey,
  withLogin,
  withTakeOver,
  withTarget,
  withoutTarget,
} from './fixes'
import * as hub from './hub-fixtures'

const step = (key: string, state: JobStep['state']): JobStep => ({ key, name: key, state })

/** A failed install with this code and facts: it failed at its first step and the server was left as it was. */
const failed = (code: string | undefined, errorInfo?: Job['errorInfo'], tail: JobStep[] = [step('discard_node', 'ok')]): Job => ({
  id: 'j',
  kind: 'install',
  state: 'failed',
  nodeId: 3,
  target: 'Frankfurt',
  startedAt: '2026-10-30T12:00:00Z',
  error: 'x',
  code,
  errorInfo,
  steps: [step('check_server', 'failed'), step('download_agent', 'pending'), ...tail],
  install: { name: 'Frankfurt', host: '203.0.113.9', sshPort: 22, sshUser: 'root', entryPort: 443 },
})

const SENT: InstallRequest = {
  sshPort: 2222,
  sshUser: 'admin',
  password: 'MARKER-hunter2',
  hostKey: 'SHA256:agreedagreedagreedagreedagreedagreedagre',
  name: 'Frankfurt',
  host: '203.0.113.9',
  entryPort: 443,
  takeOverXray: true,
}

describe('what to do about a failed install', () => {
  it('is picked by the code, with the facts the hub sent', () => {
    expect(fixFor(failed('server_has_xray', { found: ['binary', 'unit'], xrayVersion: '26.9.1' }))).toEqual({
      kind: 'takeover',
      info: { found: ['binary', 'unit'], xrayVersion: '26.9.1' },
    })
    expect(fixFor(failed('xray_not_stopped'))).toEqual({ kind: 'stop' })
    expect(fixFor(failed('port_in_use', { port: 443, role: 'entry', process: 'nginx' }))).toEqual({ kind: 'port', role: 'entry', port: 443, process: 'nginx' })
    expect(fixFor(failed('port_in_use', { port: 9090, role: 'agent' }))).toEqual({ kind: 'port', role: 'agent', port: 9090, process: '' })
    expect(fixFor(failed('login_refused'))).toEqual({ kind: 'login', why: 'refused' })
    expect(fixFor(failed('needs_root'))).toEqual({ kind: 'login', why: 'root' })
    expect(fixFor(failed('host_key_changed', { pinnedKey: 'SHA256:a', shownKey: 'SHA256:b' }))).toEqual({ kind: 'hostkey', pinned: 'SHA256:a', shown: 'SHA256:b' })
    expect(fixFor(failed('server_has_agent'))).toEqual({ kind: 'server', why: 'agent', nodeId: 0, nodeName: '' })
    expect(fixFor(failed('server_is_node', { nodeId: 3, nodeName: 'Frankfurt' }))).toEqual({ kind: 'server', why: 'node', nodeId: 3, nodeName: 'Frankfurt' })
  })

  it('is another Reality target, or the node choosing, for a target the node could not use', () => {
    const why = "It doesn't use TLS 1.3."
    expect(fixFor(failed('reality_target_failed', { target: 'www.example.com:443', reason: why }))).toEqual({ kind: 'target', target: 'www.example.com:443', reason: why, tried: 0 })
    // the node chose among its sites and none worked: no one target to name, and how many were tried
    expect(fixFor(failed('reality_target_failed', { tried: 8 }))).toEqual({ kind: 'target', target: '', reason: '', tried: 8 })
    // from a hub that sends no facts it is still that fix
    expect(fixFor(failed('reality_target_failed'))).toEqual({ kind: 'target', target: '', reason: '', tried: 0 })
    // it happens after the agent and Xray were on the server, so a cleanup that failed too is waited for first
    expect(fixFor(failed('reality_target_failed', { target: 'a.example:443' }, [step('clean_server', 'failed')]))).toBeNull()
    expect(fixFor(failed('reality_target_failed', { target: 'a.example:443' }, [step('clean_server', 'ok'), step('discard_node', 'ok')]))).not.toBeNull()
  })

  it('reads the hub\'s own failed target from its fixtures', () => {
    const job = hub.jobs.find((j) => j.code === 'reality_target_failed')!
    expect(fixFor(job)).toEqual({ kind: 'target', target: 'www.example.com:443', reason: "It doesn't use TLS 1.3.", tried: 0 })
    // and the form it carries has the target that was asked for
    expect(job.install?.handshakeTarget).toBe('www.example.com:443')
  })

  it('is plain "again" for any other failure, and for a code whose facts the hub did not send', () => {
    expect(fixFor(failed(undefined))).toEqual({ kind: 'again' })
    expect(fixFor(failed('node_unreachable'))).toEqual({ kind: 'again' })
    expect(fixFor(failed('internal'))).toEqual({ kind: 'again' })
    // a port with no port, and a changed key with one of its keys missing, can't be fixed in place
    expect(fixFor(failed('port_in_use'))).toEqual({ kind: 'again' })
    expect(fixFor(failed('port_in_use', { role: 'entry' }))).toEqual({ kind: 'again' })
    expect(fixFor(failed('host_key_changed'))).toEqual({ kind: 'again' })
    expect(fixFor(failed('host_key_changed', { shownKey: 'SHA256:b' }))).toEqual({ kind: 'again' })
    // an Xray refusal from a hub that sends no facts is still one
    expect(fixFor(failed('server_has_xray'))).toEqual({ kind: 'takeover', info: {} })
  })

  it('is none for what is no failed install, and none for an install whose server waits for a cleanup', () => {
    expect(fixFor({ ...failed('server_has_xray'), state: 'running' })).toBeNull()
    expect(fixFor({ ...failed('server_has_xray'), state: 'ok' })).toBeNull()
    expect(fixFor({ ...failed('server_has_xray'), kind: 'remove' })).toBeNull()
    const kept = failed('port_in_use', { port: 443, role: 'entry' }, [step('clean_server', 'failed')])
    expect(fixFor(kept)).toBeNull()
    expect(fixFor(failed('port_in_use', { port: 443, role: 'entry' }, [step('clean_server', 'ok'), step('discard_node', 'ok')]))).not.toBeNull()
  })

  it('reads the hub\'s own refused install from its fixtures', () => {
    const refused = hub.jobs.find((j) => j.code === 'server_has_xray')!
    expect(fixFor(refused)).toEqual({ kind: 'takeover', info: { found: ['binary', 'unit'], xrayVersion: '26.9.1' } })
  })

  it('calls a server that is refused for what it is "refused", and no other failure', () => {
    expect([...REFUSALS].sort()).toEqual(['server_has_agent', 'server_has_xray', 'server_is_node'])
    for (const code of REFUSALS) expect(isRefusal(failed(code)), code).toBe(true)
    for (const code of ['xray_not_stopped', 'port_in_use', 'login_refused', 'host_key_changed', 'node_unreachable', 'reality_target_failed', undefined]) expect(isRefusal(failed(code)), String(code)).toBe(false)
    expect(isRefusal({ ...failed('server_has_xray'), state: 'running' })).toBe(false)
    expect(isRefusal({ ...failed('server_has_xray'), kind: 'update-xray' })).toBe(false)
  })
})

describe('sending an install again', () => {
  it('changes one thing, and leaves the install that was sent as it was', () => {
    const before = structuredClone(SENT)
    for (const next of [
      withTakeOver(SENT),
      forAnotherTry(SENT),
      withHost(SENT, ' 203.0.113.10 '),
      withEntryPort(SENT, 8443),
      withLogin(SENT, { sshPort: 22, sshUser: 'root', password: 'new', hostKey: '' }),
      withHostKey(SENT, 'SHA256:new'),
      withTarget(SENT, 'www.debian.org:443'),
      withoutTarget(SENT),
    ]) {
      expect(next).not.toBe(SENT)
    }
    expect(SENT).toEqual(before)
  })

  it('takes over the Xray when it is told to, and asks again for anything that is another try', () => {
    expect(withTakeOver({ ...SENT, takeOverXray: undefined }).takeOverXray).toBe(true)
    // another try and another server don't carry the agreement; the rest of the request goes as it was
    expect(forAnotherTry(SENT)).toEqual({ ...SENT, takeOverXray: undefined })
    expect('takeOverXray' in forAnotherTry(SENT)).toBe(false)
    expect(withHost(SENT, 'other.example')).toEqual({ ...SENT, host: 'other.example', hostKey: '', takeOverXray: undefined })
    expect('takeOverXray' in withHost(SENT, 'other.example')).toBe(false)
    // the same install with a port, a login or a key changed is the same install, agreement and all
    expect(withEntryPort(SENT, 8443)).toEqual({ ...SENT, entryPort: 8443 })
    expect(withHostKey(SENT, 'SHA256:new')).toEqual({ ...SENT, hostKey: 'SHA256:new' })
  })

  it('sets the Reality target, or takes it away so the node chooses, and changes nothing else', () => {
    expect(withTarget(SENT, 'www.debian.org:8443')).toEqual({ ...SENT, handshakeTarget: 'www.debian.org:8443' })
    const asked = { ...SENT, handshakeTarget: 'www.fail.example:443' }
    // another target replaces the one that failed; the login, the key and the takeover agreement go as they were
    expect(withTarget(asked, 'www.debian.org:443')).toEqual({ ...asked, handshakeTarget: 'www.debian.org:443' })
    expect(withoutTarget(asked)).toEqual(SENT)
    expect('handshakeTarget' in withoutTarget(asked)).toBe(false)
    expect(asked.handshakeTarget).toBe('www.fail.example:443')
    // none to take away is none
    expect('handshakeTarget' in withoutTarget(SENT)).toBe(false)
  })

  it('keeps the Reality target, which is no secret, in another try and in another server', () => {
    const asked = { ...SENT, handshakeTarget: 'www.debian.org:443' }
    expect(forAnotherTry(asked).handshakeTarget).toBe('www.debian.org:443')
    expect(withHost(asked, 'other.example').handshakeTarget).toBe('www.debian.org:443')
    expect(withEntryPort(asked, 8443).handshakeTarget).toBe('www.debian.org:443')
    expect(withLogin(asked, { sshPort: 22, sshUser: 'root', password: 'new', hostKey: '' }).handshakeTarget).toBe('www.debian.org:443')
  })

  it('gives another server no key until its own is read', () => {
    expect(withHost(SENT, 'other.example').hostKey).toBe('')
    expect(withHost(SENT, '  other.example ').host).toBe('other.example')
  })

  it('swaps the login for the one given, whole', () => {
    const key = withLogin(SENT, { sshPort: 22, sshUser: 'root', privateKey: 'KEY', passphrase: 'open', hostKey: '' })
    expect(key.password).toBeUndefined()
    expect('password' in key).toBe(false)
    expect(key).toMatchObject({ sshPort: 22, sshUser: 'root', privateKey: 'KEY', passphrase: 'open', hostKey: SENT.hostKey, name: 'Frankfurt', host: '203.0.113.9', takeOverXray: true })
    const back = withLogin(key, { sshPort: 22, sshUser: 'root', password: 'again', hostKey: '' })
    expect('privateKey' in back || 'passphrase' in back).toBe(false)
    expect(back.password).toBe('again')
  })

  it('suggests another port, never the one that is held or the one SSH uses', () => {
    expect(otherPort(443, 22)).toBe(8443)
    expect(otherPort(8443, 22)).toBe(8444)
    expect(otherPort(8442, 8443)).toBe(8444)
    expect(otherPort(65535, 22)).toBe(1025)
    expect(otherPort(65534, 65535)).toBe(1025)
  })
})

// Everything a person types for an install stays in the dialog that took it: nothing of this touches the
// page's storage, its address, its history or its log. This stands in a trap for each of them.
describe('the login stays out of every place a page can keep a thing', () => {
  const trap = (what: string) =>
    new Proxy(
      {},
      {
        get: (_t, prop) => {
          throw new Error(`${what}.${String(prop)} was touched`)
        },
        set: (_t, prop) => {
          throw new Error(`${what}.${String(prop)} was set`)
        },
      },
    )
  afterEach(() => vi.unstubAllGlobals())

  it('is only ever passed along and copied', () => {
    for (const what of ['localStorage', 'sessionStorage', 'indexedDB', 'history', 'location', 'document', 'console', 'navigator']) vi.stubGlobal(what, trap(what))
    let r = SENT
    r = withTakeOver(r)
    r = withHost(r, 'other.example')
    r = withEntryPort(r, 8443)
    r = withLogin(r, { sshPort: 22, sshUser: 'root', privateKey: 'KEY', hostKey: '' })
    r = withHostKey(forAnotherTry(r), 'SHA256:x')
    r = withoutTarget(withTarget(r, 'www.debian.org:443'))
    expect(r.privateKey).toBe('KEY')
    expect(fixFor(failed('login_refused'))).toEqual({ kind: 'login', why: 'refused' })
    expect(otherPort(443, 22)).toBe(8443)
  })

  // the files that handle the request never name a place to keep one
  it.each(['lib/fixes.ts', 'screens/AddNodeDialog.tsx', 'screens/FixPanel.tsx'])('is not kept by %s', (file) => {
    const code = readFileSync(new URL(`../${file}`, import.meta.url), 'utf8')
    for (const place of ['localStorage', 'sessionStorage', 'indexedDB', 'document.cookie', 'history.', 'location.', 'console.', 'BroadcastChannel', 'postMessage']) {
      expect(code, `${file} mentions ${place}`).not.toContain(place)
    }
    // and no address is built from it
    expect(code).not.toMatch(/navigate\([^)]*(held|req|password|privateKey)/)
  })
})
