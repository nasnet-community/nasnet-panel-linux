// Typed calls to the plugin's API. URLs are relative, so they resolve against
// the <base href> the server injects (the panel's plugin path).

import { backupFileName } from './backup'

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly body: Record<string, unknown> = {},
  ) {
    super(message)
  }

  /** The server's error code ("node_limit", "host_key_changed", ...), or "" when it sent none. */
  get code() {
    return typeof this.body.code === 'string' ? this.body.code : ''
  }

  /** The node is down and the call needs an SSH login (or force). */
  get needLogin() {
    return this.body.needLogin === true || this.code === 'needs_login'
  }
}

// Another site's page can't send this without a preflight, which the panel refuses.
// Every call carries it except the job event stream (see followJob).
const OWN = { 'X-RemoteNodes-UI': '1' }

function failure(status: number, data: Record<string, unknown> | null) {
  const message = typeof data?.error === 'string' && data.error ? data.error : undefined
  return new ApiError(message ?? `Request failed (${status})`, status, data ?? {})
}

const call = (method: string, path: string, body?: unknown) =>
  fetch(`api/${path}`, {
    method,
    headers: body === undefined ? OWN : { ...OWN, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await call(method, path, body)
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw failure(res.status, data)
  return data as T
}

export interface Started {
  jobId: string
}

export type Reach = 'online' | 'offline' | 'missing'

export interface Entry {
  /** the domestic link and the VPN are both usable */
  on: boolean
  domestic: Reach
  vpn: { state: Reach; kind: 'wireguard' | 'l2tp' | 'openvpn' | 'other' | ''; label: string }
  bridge: 'running' | 'waiting' | 'stopped'
  routerOS: { version: string; min: string; ok: boolean }
  routes: { address: string; usedFor: 'downloads' | 'entry-exit' | 'nodes'; via: 'foreign' | 'vpn' | 'domestic'; ready: boolean }[]
  /** when the router script last reported */
  checkedAt: string | null
  /** the report is older than 15 minutes */
  stale: boolean
}

export interface AppConfig {
  version: string
  commit: string
  maxNodes: number
  nodeCount: number
  maxSubscriptions: number
  subscriptionCount: number
  entry: Entry
  backup: { lastExportAt: string | null }
  /** the one Xray version this plugin installs, such as "26.10.21" */
  xray: string
}

export interface ProbeRequest {
  host: string
  sshPort: number
  sshUser: string
}

export interface ProbeResult {
  fingerprint: string
  status: 'new' | 'known' | 'changed'
}

export interface Login {
  sshPort: number
  sshUser: string
  password?: string
  privateKey?: string
  passphrase?: string
  hostKey: string
}

export interface InstallRequest extends Login {
  name: string
  host: string
  /** 443 when left out */
  entryPort?: number
  /**
   * The person agreed that an Xray the server runs of its own may be taken over: stopped, its config, service
   * and drop-ins removed with no copy kept, its binary replaced unless it is the pinned build. Left out is no.
   */
  takeOverXray?: boolean
  /**
   * The Reality target the node is to use, host:port. The node checks it during the install, which fails at that
   * step if the node can't use it (reality_target_failed). Left out, the node chooses one.
   */
  handshakeTarget?: string
}

export interface RemoveRequest {
  /** forget the node without touching the server */
  force?: boolean
  login?: Login
}

export type JobState = 'running' | 'ok' | 'failed'

export interface JobStep {
  /** what the page words the step by: stable, lower-case words joined by underscores ("upload_agent") */
  key: string
  /** the step in the hub's English, which the page shows for a key it has no words for */
  name: string
  state: JobState | 'pending'
  /** may start with a percentage: "62% · 8.4 of 13.6 MB" */
  detail?: string
}

export type JobKind = 'install' | 'update-agent' | 'update-xray' | 'renew-cert' | 'rotate-keys' | 'change-settings' | 'remove'

/** What an install was asked for, but the login's secrets: enough to offer the same install again. */
export interface InstallForm {
  name: string
  host: string
  sshPort: number
  sshUser: string
  entryPort: number
  /** the Reality target that was asked for, host:port; missing when the node was to choose */
  handshakeTarget?: string
}

/**
 * The facts of a failed job's error that the page needs beyond its code, sent by the hub from its typed errors.
 * Every field is optional, a job has the ones its error has, and none holds a secret.
 */
export interface ErrorInfo {
  /** port_in_use: the port, whose it is, and the process that holds it on the server when the server says */
  port?: number
  role?: 'agent' | 'entry'
  process?: string
  /** host_key_changed: the fingerprint that was agreed to, and the one the server shows now */
  pinnedKey?: string
  shownKey?: string
  /** server_has_xray: what was found, the version of the binary at the standard path, and whether it is the pinned build */
  found?: ('binary' | 'unit' | 'process')[]
  xrayVersion?: string
  sameBuild?: boolean
  /** server_is_node: the node that is on the server already */
  nodeId?: number
  nodeName?: string
  /** reality_target_failed: the Reality target the node couldn't use, host:port, and why in the node's own words */
  target?: string
  reason?: string
  /** reality_target_failed with no one target: how many sites the node tried, and none worked */
  tried?: number
  /** the install took over the server's own Xray before it failed: that Xray is gone and nothing puts it back */
  tookOver?: boolean
}

export interface Job {
  id: string
  kind: JobKind
  state: JobState
  steps: JobStep[]
  error?: string
  /** a failed job's error code, when it has one the page words ("server_has_xray") */
  code?: string
  /** the facts that go with the code, for the errors that have some */
  errorInfo?: ErrorInfo
  /** 0 or missing while the job has no node (yet) */
  nodeId?: number
  /** the install's node name, else "" */
  target?: string
  startedAt: string
  finishedAt?: string
  /** an install job's form, for "Try again" */
  install?: InstallForm
}

export interface LinkConn {
  name: string
  up: boolean
  since: string | null
  rttMs: number
  streams: number
  lastError: string
}

export interface TunnelState {
  state: 'up' | 'down' | 'off'
  since: string | null
  rttMs: number
  streams: number
  carried: number
  bytesToSubs: number
  bytesFromSubs: number
  dialFailures: number
  reason: '' | 'xray_mismatch' | 'entry_off' | 'bridge_down' | 'agent_down'
}

export interface AgentState {
  version: string
  xrayVersion: string
  uptimeSec: number
  rssBytes: number
  reportedAt: string
  xray: { running: boolean; restarts: number; uptimeSec: number; lastExit: string; crashLoop: boolean }
  settingsApplied: boolean
  lastPushError: string
}

export interface NodeEvent {
  at: string
  kind: string
  detail: string
}

export interface NodeUsage {
  /** the agent reports connections and the numbers are fresh */
  live: boolean
  online: number
  connections: number
  /** bytes per second */
  rateUp: number
  rateDown: number
  todayUp: number
  todayDown: number
  monthUp: number
  monthDown: number
  updatedAt: string | null
}

export interface Node {
  id: number
  name: string
  host: string
  sshPort: number
  sshUser: string
  /** false: installing, or an install that didn't finish (see unfinished) */
  registered: boolean
  createdAt: string
  entryPort: number
  /** host:port of the Reality target */
  handshakeTarget: string
  /** how the hub's last look at the target went. Not in the contract yet: the Settings tab shows the badge only when it is here. */
  handshakeCheck?: { ok: boolean; checkedAt: string }
  sni: string
  /** false: the same as the Reality target's host */
  sniCustom: boolean
  publicKey: string
  shortId: string
  arch: string
  agentVersion: string
  /** as the node reports it */
  xrayVersion: string
  lastSeenAt: string | null
  /** enabled on this node */
  subscriptionCount: number
  tunnel: TunnelState
  agent: AgentState | null
  cert: { notAfter: string; validDays: number; renewsAt: string }
  updates: { agent: boolean; xray: boolean; agentVersion: string; xrayVersion: string }
  usage: NodeUsage
  events?: NodeEvent[]
  /** present while the install job runs: the running step by its key and its English name */
  installing?: { jobId: string; step: number; steps: number; key: string; label: string; percent: number | null }
  /** present for an install that stopped and wasn't cleaned up: no node, only Clean up and Forget work */
  unfinished?: Unfinished
}

/** Why an install stopped: the step by its key and English name, the error, and why the cleanup failed ("" when none ran). */
export interface Unfinished {
  stepKey: string
  step: string
  error: string
  cleanup: string
  at: string | null
}

export interface HandshakeCheck {
  ok: boolean
  tls13: boolean
  x25519: boolean
  handshakeBytes: number
  latencyMs: number
  error: string
}

export interface SubscriptionNode {
  nodeId: number
  enabled: boolean
  /** null: the node is unreachable, so it isn't known */
  online: boolean | null
  connections: number | null
  lastSeenAt: string | null
  todayUp: number
  todayDown: number
  monthUp: number
  monthDown: number
}

/** Why the hub took a subscription off the nodes: its traffic limit was reached, or its end date. */
export type StoppedBy = 'limit' | 'expired'

export interface Subscription {
  id: number
  name: string
  createdAt: string
  /** bytes it may move in all, both ways on every node; 0 is no limit */
  trafficLimit: number
  /** bytes it has moved since its count was last reset */
  trafficUsed: number
  /** when it stops; null is never */
  expiresAt: string | null
  /** set while the hub keeps it off the nodes; its switches stay as they were */
  stopped: StoppedBy | ''
  online: boolean | null
  lastSeenAt: string | null
  todayUp: number
  todayDown: number
  monthUp: number
  monthDown: number
  /** one per registered node */
  nodes: SubscriptionNode[]
}

/** A subscription as one node's list shows it. */
export interface NodeSubscription {
  id: number
  name: string
  enabled: boolean
  /** set while the hub keeps the subscription off the nodes */
  stopped: StoppedBy | ''
  online: boolean | null
  connections: number | null
  lastSeenAt: string | null
  todayUp: number
  todayDown: number
  monthUp: number
  monthDown: number
}

export interface CreateSubscription {
  name: string
  /** the nodes it is switched on for */
  nodeIds: number[]
  /** bytes; left out for no limit */
  trafficLimit?: number
  /** an instant, RFC 3339; left out for no end date */
  expiresAt?: string
}

/** What a change to a subscription may carry. What is left out stays; null takes the limit or the date away. */
export interface SubscriptionPatch {
  name?: string
  trafficLimit?: number | null
  expiresAt?: string | null
}

export interface Link {
  nodeId: number
  nodeName: string
  vless: string
}

export interface Traffic {
  stepSec: number
  /** down is the way to the subscriptions */
  samples: { at: string; up: number; down: number }[]
}

export interface NodeSettings {
  entryPort?: number
  handshakeTarget?: string
  sni?: string
}

/** The longest a node or subscription name may be, in characters: the hub's model.MaxNameRunes. */
export const MAX_NAME_LENGTH = 64

const node = (id: number) => `nodes/${id}`
const sub = (id: number) => `subscriptions/${id}`

export const api = {
  config: () => request<AppConfig>('GET', 'config'),
  probe: (req: ProbeRequest) => request<ProbeResult>('POST', 'probe', req),
  job: (jobId: string) => request<Job>('GET', `jobs/${encodeURIComponent(jobId)}`),
  jobs: () => request<Job[]>('GET', 'jobs'),
  nodes: () => request<Node[]>('GET', 'nodes'),
  install: (req: InstallRequest) => request<Started>('POST', 'nodes', req),
  node: (id: number) => request<Node>('GET', node(id)),
  rename: (id: number, name: string) => request<Node>('PATCH', node(id), { name }),
  settings: (id: number, s: NodeSettings) => request<Started>('PATCH', `${node(id)}/settings`, s),
  handshakeCheck: (id: number, target: string) => request<HandshakeCheck>('POST', `${node(id)}/handshake-check`, { target }),
  checkUpdates: (id: number) => request<Node>('POST', `${node(id)}/check-updates`),
  update: (id: number, kind: 'agent' | 'xray') => request<Started>('POST', `${node(id)}/update`, { kind }),
  renewCert: (id: number) => request<Started>('POST', `${node(id)}/renew-cert`),
  rotateKeys: (id: number) => request<Started>('POST', `${node(id)}/rotate-keys`),
  remove: (id: number, req: RemoveRequest) => request<Started>('POST', `${node(id)}/remove`, req),
  nodeSubscriptions: (id: number) => request<NodeSubscription[]>('GET', `${node(id)}/subscriptions`),
  traffic: (id: number) => request<Traffic>('GET', `${node(id)}/traffic`),
}

export const subscriptions = {
  list: () => request<Subscription[]>('GET', 'subscriptions'),
  create: (s: CreateSubscription) => request<Subscription>('POST', 'subscriptions', s),
  rename: (id: number, name: string) => request<Subscription>('PATCH', sub(id), { name }),
  update: (id: number, patch: SubscriptionPatch) => request<Subscription>('PATCH', sub(id), patch),
  /** starts its traffic count again from zero */
  resetUsage: (id: number) => request<Subscription>('POST', `${sub(id)}/reset-usage`),
  remove: (id: number) => request<{ ok: boolean }>('DELETE', sub(id)),
  /** switches it on or off for one node; off removes the client, on adds it back with the same id */
  setNode: (id: number, nodeId: number, enabled: boolean) => request<Subscription>('PUT', `${sub(id)}/nodes/${nodeId}`, { enabled }),
  link: (id: number, nodeId: number) => request<Link>('GET', `${sub(id)}/nodes/${nodeId}/link`),
  /** a new id: the old link stops working */
  replaceLink: (id: number, nodeId: number) => request<Link>('POST', `${sub(id)}/nodes/${nodeId}/replace-link`),
}

export interface BackupFile {
  name: string
  blob: Blob
}

export const backup = {
  /** The encrypted file, named as the hub names it. */
  async export(passphrase: string): Promise<BackupFile> {
    const res = await call('POST', 'backup/export', { passphrase })
    if (!res.ok) throw failure(res.status, await res.json().catch(() => ({})))
    return { name: backupFileName(res.headers.get('Content-Disposition')), blob: await res.blob() }
  },
  /** Only an empty install takes one. `data` is the file, base64. */
  import: (passphrase: string, data: string) => request<{ ok: boolean; nodes: number }>('POST', 'backup/import', { passphrase, data }),
}

/** The error of a job the plugin no longer knows; the page words it itself. */
export const JOB_LOST = 'job_lost'

/**
 * Follows a job until it ends: over its event stream, or by polling when the
 * stream fails or stays silent. Returns a function that stops following.
 *
 * The stream is an EventSource, which can't send the page's header, so the hub
 * lets this one read-only call through without it (see the API contract).
 */
export function followJob(
  jobId: string,
  onUpdate: (job: Job) => void,
  { pollMs = 1000, silentMs = 5000 } = {},
): () => void {
  let stopped = false
  let polling = false
  let last: Job | null = null
  let source: EventSource | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  let watchdog: ReturnType<typeof setTimeout> | undefined

  const deliver = (job: Job) => {
    if (stopped) return
    last = job
    onUpdate(job)
    if (job.state !== 'running') stop()
  }

  const poll = async () => {
    if (stopped) return
    try {
      deliver(await api.job(jobId))
    } catch (e) {
      // a restarted plugin forgets its jobs, so this one will never end
      if (e instanceof ApiError && e.status === 404) {
        return deliver({
          ...(last ?? { id: jobId, kind: 'install', steps: [], startedAt: '' }),
          state: 'failed',
          error: JOB_LOST,
        })
      }
      // anything else is a blip; the next tick tries again
    }
    if (!stopped) timer = setTimeout(poll, pollMs)
  }

  const fallBack = () => {
    clearTimeout(watchdog)
    source?.close()
    source = null
    if (stopped || polling) return
    polling = true
    void poll()
  }

  const stop = () => {
    stopped = true
    clearTimeout(timer)
    clearTimeout(watchdog)
    source?.close()
    source = null
  }

  if (typeof EventSource === 'undefined') {
    fallBack()
  } else {
    source = new EventSource(`api/jobs/${encodeURIComponent(jobId)}/events`)
    source.onmessage = (e) => {
      clearTimeout(watchdog)
      let job: Job
      try {
        job = JSON.parse(e.data)
      } catch {
        return fallBack()
      }
      deliver(job)
    }
    // the stream closes after the final state, which deliver() has already seen
    source.onerror = fallBack
    // a proxy that buffers the stream would show nothing until the job ends
    watchdog = setTimeout(fallBack, silentMs)
  }
  return stop
}
