import type { ErrorInfo, InstallRequest, Job, Login } from './api'
import { installOutcome } from './install'

// What the page can do about an install that failed, and how it sends the install again. The install a
// person sent is kept by the dialog that sent it, in the dialog's own state and nowhere else, so that a
// fix can edit it and send it again; nothing in here keeps, prints or stores any of it, and every change
// gives a new request and leaves the old one as it was.

/** The failures that mean this server can't be used as it is. */
export const REFUSALS = ['server_has_xray', 'server_has_agent', 'server_is_node']

/** An install that was refused for what its server is. */
export const isRefusal = (job: Job) => job.kind === 'install' && job.state === 'failed' && REFUSALS.includes(job.code ?? '')

/** What there is to do about a failed install, by what went wrong. */
export type Fix =
  /** the server runs an Xray of its own: take it over, or use another server */
  | { kind: 'takeover'; info: ErrorInfo }
  /** a takeover stopped the service and an Xray still runs: stop it there, then go on */
  | { kind: 'stop' }
  /** a port the node needs is held: another entry port, or free it; the agent's port is the hub's setting */
  | { kind: 'port'; role: 'entry' | 'agent'; port: number; process: string }
  /** the login didn't work: the server refused it, or it can't act as root */
  | { kind: 'login'; why: 'refused' | 'root' }
  /** the server shows another SSH key than the one that was agreed to */
  | { kind: 'hostkey'; pinned: string; shown: string }
  /** the server has an agent, or is a node already: use another */
  | { kind: 'server'; why: 'agent' | 'node'; nodeId: number; nodeName: string }
  /** the node can't use the Reality target: give another, or let the node choose (`target` is '' when the node chose and none worked) */
  | { kind: 'target'; target: string; reason: string; tried: number }
  /** nothing in particular: try again, or change the details */
  | { kind: 'again' }

/**
 * The fix for a failed install, or null when there is none to offer: it isn't a failed install, or its
 * cleanup failed too and its server waits for that first. A code the page knows with no facts to go
 * with it (a hub that doesn't send them) is the plain "again".
 */
export function fixFor(job: Job): Fix | null {
  if (job.kind !== 'install' || job.state !== 'failed' || installOutcome(job) === 'kept') return null
  const info = job.errorInfo ?? {}
  switch (job.code) {
    case 'server_has_xray':
      return { kind: 'takeover', info }
    case 'xray_not_stopped':
      return { kind: 'stop' }
    case 'port_in_use':
      return info.port ? { kind: 'port', role: info.role === 'agent' ? 'agent' : 'entry', port: info.port, process: info.process ?? '' } : { kind: 'again' }
    case 'login_refused':
      return { kind: 'login', why: 'refused' }
    case 'needs_root':
      return { kind: 'login', why: 'root' }
    case 'host_key_changed':
      return info.pinnedKey && info.shownKey ? { kind: 'hostkey', pinned: info.pinnedKey, shown: info.shownKey } : { kind: 'again' }
    case 'server_has_agent':
      return { kind: 'server', why: 'agent', nodeId: 0, nodeName: '' }
    case 'server_is_node':
      return { kind: 'server', why: 'node', nodeId: info.nodeId ?? 0, nodeName: info.nodeName ?? '' }
    case 'reality_target_failed':
      return { kind: 'target', target: info.target ?? '', reason: info.reason ?? '', tried: info.tried ?? 0 }
  }
  return { kind: 'again' }
}

// --- changes to the request ---------------------------------------------------------------

/** The same install, with the server's own Xray taken over. */
export const withTakeOver = (r: InstallRequest): InstallRequest => ({ ...r, takeOverXray: true })

/**
 * The same install for another try: the agreement to take over the server's Xray is asked again each
 * time, so it isn't carried. The Reality target is no secret, and stays.
 */
export function forAnotherTry(r: InstallRequest): InstallRequest {
  const { takeOverXray: _, ...rest } = r
  return rest
}

/**
 * The same install on another server. The host key that was agreed to belonged to the old one, so there is
 * none until the new server's is read, and the agreement to take over its Xray doesn't carry over either.
 */
export const withHost = (r: InstallRequest, host: string): InstallRequest => ({ ...forAnotherTry(r), host: host.trim(), hostKey: '' })

/** The same install with another entry port. */
export const withEntryPort = (r: InstallRequest, entryPort: number): InstallRequest => ({ ...r, entryPort })

/** The same install with another login: the port, user and secrets as given, and the host key that was agreed to. */
export function withLogin(r: InstallRequest, login: Login): InstallRequest {
  const { password: _p, privateKey: _k, passphrase: _s, ...rest } = r
  const next: InstallRequest = { ...rest, sshPort: login.sshPort, sshUser: login.sshUser }
  if (login.password !== undefined) next.password = login.password
  if (login.privateKey !== undefined) next.privateKey = login.privateKey
  if (login.passphrase !== undefined) next.passphrase = login.passphrase
  return next
}

/** The same install with this Reality target, host:port; the node checks it again. */
export const withTarget = (r: InstallRequest, handshakeTarget: string): InstallRequest => ({ ...r, handshakeTarget })

/** The same install with no Reality target of its own: the node chooses one. */
export function withoutTarget(r: InstallRequest): InstallRequest {
  const { handshakeTarget: _, ...rest } = r
  return rest
}

/** The same install, holding the server to the key it shows now. */
export const withHostKey = (r: InstallRequest, hostKey: string): InstallRequest => ({ ...r, hostKey })

/** A port to suggest in place of one that is held: the next one up, or 8443 for the usual 443, never the SSH port. */
export function otherPort(busy: number, ssh: number): number {
  let p = busy === 443 ? 8443 : busy + 1
  while (p === ssh || p > 65535) p = p > 65534 ? 1025 : p + 1
  return p
}
