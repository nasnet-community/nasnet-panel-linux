import { ApiError } from '../lib/api'
import type { MsgKey, Params } from './translate'

export type T = (key: MsgKey, params?: Params) => string

// the codes the page has its own words for; anything else shows the server's English
const WORDED = new Set([
  'forbidden',
  'not_found',
  'node_limit',
  'subscription_limit',
  'name_taken',
  'entry_off',
  'node_unreachable',
  'needs_login',
  'version_mismatch',
  'agent_too_old',
  'host_key_changed',
  'server_has_xray',
  'server_has_agent',
  'server_is_node',
  'xray_not_stopped',
  'port_in_use',
  'login_refused',
  'needs_root',
  'busy',
  'backup_bad_passphrase',
  'backup_bad_file',
])

/** The page's words for an error code, or null for a code it has none for. */
export const codeText = (t: T, code: string | undefined): string | null => (code && WORDED.has(code) ? t(`error.${code}` as MsgKey) : null)

/** What to tell the user about a failed call: the page's wording for the error code, else the server's sentence. */
export function errorText(t: T, e: unknown): string {
  if (e instanceof ApiError && WORDED.has(e.code)) return t(`error.${e.code}` as MsgKey)
  return e instanceof Error ? e.message : String(e)
}
