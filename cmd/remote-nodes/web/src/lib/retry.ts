import { navigate } from '../hooks/useRoute'
import type { InstallForm } from './api'

// Try again can be asked for from anywhere (the job tray, a node page), and the Add node dialog lives
// on the Nodes tab: the form waits here until that tab takes it.
let pending: InstallForm | null = null
const listeners = new Set<() => void>()

/** Opens Add node on the Nodes tab, filled in with what a failed install was asked for. */
export function askRetry(form: InstallForm) {
  pending = form
  navigate({ page: 'root', tab: 'nodes' })
  listeners.forEach((l) => l())
}

/** The form waiting for the dialog, once: taking it clears it. */
export function takeRetry(): InstallForm | null {
  const form = pending
  pending = null
  return form
}

/** Hears when a form is waiting; returns what stops it. */
export function onRetry(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}
