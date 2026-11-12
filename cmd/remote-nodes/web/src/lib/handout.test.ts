import { describe, expect, it } from 'vitest'
import { HANDOUT_KEY, clearHandout, handoutLeft, markHandout, readHandout, startHandout, type Store } from './handout'

/** A storage in memory, like the browser's. */
function memory(seed: Record<string, string> = {}) {
  const data = new Map(Object.entries(seed))
  const store: Store = {
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
    removeItem: (k) => void data.delete(k),
  }
  return { store, data }
}

/** A storage that refuses everything, as a browser with storage blocked does. */
const blocked: Store = {
  getItem: () => {
    throw new Error('blocked')
  },
  setItem: () => {
    throw new Error('blocked')
  },
  removeItem: () => {
    throw new Error('blocked')
  },
}

const NOW = Date.parse('2026-11-12T10:00:00Z')
const subs = [
  { id: 1, name: 'Sara' },
  { id: 2, name: 'Ali' },
  { id: 4, name: 'Mina' },
]

describe('the list of links to hand out', () => {
  it('is kept per place, and read back as it was written', () => {
    const { store } = memory()
    const made = startHandout(store, 'router', subs, NOW)
    expect(made).toEqual({ at: '2026-11-12T10:00:00.000Z', subs, done: [] })
    expect(readHandout(store, 'router')).toEqual(made)
    // a node has its own, and none until its links change
    expect(readHandout(store, 1)).toBeNull()
    startHandout(store, 1, [subs[0]], NOW)
    expect(readHandout(store, 1)?.subs).toEqual([subs[0]])
    expect(readHandout(store, 'router')?.subs).toEqual(subs)
    expect(readHandout(store, 2)).toBeNull()
  })

  it('keeps only the id and the name of a subscription', () => {
    const { store, data } = memory()
    startHandout(store, 'router', [{ id: 1, name: 'Sara', vless: 'vless://secret' } as { id: number; name: string }], NOW)
    expect(data.get(HANDOUT_KEY)).not.toContain('secret')
  })

  it('ticks what was sent, once, and can take a tick back', () => {
    const { store } = memory()
    let list = startHandout(store, 'router', subs, NOW)!
    expect(handoutLeft(list)).toBe(3)
    list = markHandout(store, 'router', list, 2)
    list = markHandout(store, 'router', list, 2)
    expect(list.done).toEqual([2])
    expect(handoutLeft(list)).toBe(2)
    expect(readHandout(store, 'router')?.done).toEqual([2])
    list = markHandout(store, 'router', list, 2, false)
    expect(handoutLeft(list)).toBe(3)
    expect(readHandout(store, 'router')?.done).toEqual([])
  })

  it('starts again from nothing when the links change once more', () => {
    const { store } = memory()
    const first = markHandout(store, 'router', startHandout(store, 'router', subs, NOW)!, 1)
    expect(first.done).toEqual([1])
    // a link sent after the first change is old again after the second
    const second = startHandout(store, 'router', subs.slice(0, 2), NOW + 60e3)
    expect(second).toEqual({ at: '2026-11-12T10:01:00.000Z', subs: subs.slice(0, 2), done: [] })
  })

  it('has no list for a place nobody is on, and drops the one it had', () => {
    const { store, data } = memory()
    startHandout(store, 'router', subs, NOW)
    expect(startHandout(store, 'router', [], NOW)).toBeNull()
    expect(readHandout(store, 'router')).toBeNull()
    // nothing is left behind once the last list is gone
    expect(data.has(HANDOUT_KEY)).toBe(false)
  })

  it('is forgotten for one place and kept for the others', () => {
    const { store, data } = memory()
    startHandout(store, 'router', subs, NOW)
    startHandout(store, 3, subs, NOW)
    clearHandout(store, 'router')
    expect(readHandout(store, 'router')).toBeNull()
    expect(readHandout(store, 3)?.subs).toEqual(subs)
    clearHandout(store, 3)
    expect(data.has(HANDOUT_KEY)).toBe(false)
  })

  it('reads nothing from storage that holds something else', () => {
    for (const junk of ['', 'not json', '[]', '"text"', 'null', '{"router":{"at":1,"subs":[],"done":[]}}', '{"router":{"at":"x","subs":[{"id":"1","name":"a"}],"done":[]}}']) {
      const { store } = memory({ [HANDOUT_KEY]: junk })
      expect(readHandout(store, 'router'), junk).toBeNull()
    }
    // a good list beside a bad one is still read
    const { store } = memory({ [HANDOUT_KEY]: JSON.stringify({ router: 'junk', 'node:1': { at: 'x', subs: [{ id: 1, name: 'Sara' }], done: [1] } }) })
    expect(readHandout(store, 1)?.done).toEqual([1])
    expect(readHandout(store, 'router')).toBeNull()
  })

  it('still works for this page view where storage is blocked or missing', () => {
    for (const store of [blocked, null]) {
      const list = startHandout(store, 'router', subs, NOW)!
      expect(list.subs).toEqual(subs)
      expect(markHandout(store, 'router', list, 1).done).toEqual([1])
      expect(readHandout(store, 'router')).toBeNull()
      expect(() => clearHandout(store, 'router')).not.toThrow()
    }
  })
})
