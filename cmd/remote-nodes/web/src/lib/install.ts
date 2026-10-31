import type { InstallForm, Job, Node } from './api'

/**
 * An install that stopped and wasn't cleaned up: no node, and only Clean up and Forget work.
 * A record that is neither set up nor installing is one too, whatever the hub says of it.
 */
export const isUnfinished = (n: Node) => !!n.unfinished || (!n.registered && !n.installing)

/** The records that are nodes: set up, or with their install running. */
export const realNodes = (nodes: Node[]) => nodes.filter((n) => !isUnfinished(n))

/**
 * How a failed install ended, from its last steps: the server cleaned up, nothing changed on it, or
 * the install kept as unfinished because the cleanup failed too. Null for any other job, and for an
 * install that didn't get that far (a job the router lost).
 */
export type InstallOutcome = 'cleaned' | 'untouched' | 'kept'

export function installOutcome(job: Job): InstallOutcome | null {
  if (job.kind !== 'install' || job.state !== 'failed') return null
  const step = (key: string) => job.steps.find((s) => s.key === key)
  const clean = step('clean_server')
  const discard = step('discard_node')
  if (clean?.state === 'failed' || discard?.state === 'failed') return 'kept'
  if (discard?.state === 'ok') return clean?.state === 'ok' ? 'cleaned' : 'untouched'
  return null
}

/**
 * What Try again fills the Add node form in with: a failed install's form. Not for one that was kept,
 * whose server waits for a cleanup first.
 */
export const retryForm = (job: Job): InstallForm | null =>
  job.kind === 'install' && job.state === 'failed' && job.install && installOutcome(job) !== 'kept' ? job.install : null
