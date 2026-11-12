import type { PlaceId } from './api'

/**
 * The subscriptions whose link for one place changed (a new port, a new name, new keys), to hand
 * the new links out from, with the ones already sent ticked. It is kept in the browser and nowhere
 * else: it is a reminder for the person at this screen, and the hub has nothing to do with it.
 */
export interface Handout {
  /** when the links changed */
  at: string
  /** the subscriptions that were switched on for the place then */
  subs: { id: number; name: string }[]
  /** the ids of those that have been given their new link */
  done: number[]
}

export const HANDOUT_KEY = 'remote-nodes.handout'

/** The browser's storage, or anything like it; null where there is none. */
export type Store = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> | null

const placeKey = (place: PlaceId) => (place === 'router' ? 'router' : `node:${place}`)

const isHandout = (v: unknown): v is Handout => {
  const h = v as Handout
  return (
    !!h &&
    typeof h === 'object' &&
    typeof h.at === 'string' &&
    Array.isArray(h.subs) &&
    h.subs.every((s) => !!s && typeof s.id === 'number' && typeof s.name === 'string') &&
    Array.isArray(h.done) &&
    h.done.every((id) => typeof id === 'number')
  )
}

/** Every list that is kept, by place. Storage that is blocked, or holds something else, has none. */
function readAll(storage: Store): Record<string, Handout> {
  try {
    const raw = storage?.getItem(HANDOUT_KEY)
    const all: unknown = raw ? JSON.parse(raw) : null
    if (!all || typeof all !== 'object' || Array.isArray(all)) return {}
    return Object.fromEntries(Object.entries(all).filter(([, v]) => isHandout(v))) as Record<string, Handout>
  } catch {
    return {}
  }
}

function writeAll(storage: Store, all: Record<string, Handout>) {
  try {
    if (Object.keys(all).length) storage?.setItem(HANDOUT_KEY, JSON.stringify(all))
    else storage?.removeItem(HANDOUT_KEY)
  } catch {
    // blocked or full: the list then lasts for this page view only
  }
}

/** The list of a place, or null when it has none. */
export const readHandout = (storage: Store, place: PlaceId): Handout | null => readAll(storage)[placeKey(place)] ?? null

/**
 * Starts a list for a place whose links just changed. An earlier list for it is replaced whole: a link
 * sent before this change is old again. With nobody on the place there is nothing to hand out, and no list.
 */
export function startHandout(storage: Store, place: PlaceId, subs: { id: number; name: string }[], now = Date.now()): Handout | null {
  const all = readAll(storage)
  const list: Handout | null = subs.length ? { at: new Date(now).toISOString(), subs: subs.map(({ id, name }) => ({ id, name })), done: [] } : null
  if (list) all[placeKey(place)] = list
  else delete all[placeKey(place)]
  writeAll(storage, all)
  return list
}

/** Ticks one subscription of a list, or takes the tick away. */
export function markHandout(storage: Store, place: PlaceId, list: Handout, subId: number, sent = true): Handout {
  const done = sent ? [...new Set([...list.done, subId])] : list.done.filter((id) => id !== subId)
  const next = { ...list, done }
  const all = readAll(storage)
  all[placeKey(place)] = next
  writeAll(storage, all)
  return next
}

/** Forgets the list of a place: every link was sent, or the person doesn't want the reminder. */
export function clearHandout(storage: Store, place: PlaceId) {
  const all = readAll(storage)
  delete all[placeKey(place)]
  writeAll(storage, all)
}

/** How many of a list still wait for their link. */
export const handoutLeft = (list: Handout) => list.subs.filter((s) => !list.done.includes(s.id)).length

/** The browser's own storage, or null where it is blocked. */
export function browserStore(): Store {
  try {
    return window.localStorage
  } catch {
    return null
  }
}
