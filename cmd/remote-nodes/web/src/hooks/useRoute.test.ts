import { describe, expect, it } from 'vitest'
import { parseRoute, routeHash } from './useRoute'

describe('parseRoute', () => {
  it('maps hashes to pages', () => {
    expect(parseRoute('')).toEqual({ page: 'root', tab: 'nodes' })
    expect(parseRoute('#/')).toEqual({ page: 'root', tab: 'nodes' })
    expect(parseRoute('#/subscriptions')).toEqual({ page: 'root', tab: 'subscriptions' })
    expect(parseRoute('#/settings')).toEqual({ page: 'root', tab: 'settings' })
    expect(parseRoute('#/nodes/3')).toEqual({ page: 'node', id: '3', tab: 'subscriptions' })
    expect(parseRoute('#/nodes/3/status')).toEqual({ page: 'node', id: '3', tab: 'status' })
    expect(parseRoute('#/nodes/3/settings')).toEqual({ page: 'node', id: '3', tab: 'settings' })
    expect(parseRoute('#/nodes/3/subscriptions')).toEqual({ page: 'node', id: '3', tab: 'subscriptions' })
  })

  it("reads the router's own page, which has a node's tabs and no id", () => {
    expect(parseRoute('#/router')).toEqual({ page: 'router', tab: 'subscriptions' })
    expect(parseRoute('#/router/status')).toEqual({ page: 'router', tab: 'status' })
    expect(parseRoute('#/router/settings')).toEqual({ page: 'router', tab: 'settings' })
    expect(parseRoute('#/router/nonsense')).toEqual({ page: 'router', tab: 'subscriptions' })
  })

  it('falls back to the first page for anything else', () => {
    expect(parseRoute('#/nonsense')).toEqual({ page: 'root', tab: 'nodes' })
    expect(parseRoute('#/nodes')).toEqual({ page: 'root', tab: 'nodes' })
    expect(parseRoute('#/nodes/abc')).toEqual({ page: 'root', tab: 'nodes' })
    expect(parseRoute('#/nodes/3/nonsense')).toEqual({ page: 'node', id: '3', tab: 'subscriptions' })
  })
})

describe('routeHash', () => {
  it('writes the hash parseRoute reads', () => {
    expect(routeHash({ page: 'root', tab: 'nodes' })).toBe('#/')
    expect(routeHash({ page: 'root', tab: 'settings' })).toBe('#/settings')
    expect(routeHash({ page: 'node', id: 3 })).toBe('#/nodes/3')
    expect(routeHash({ page: 'node', id: '3', tab: 'subscriptions' })).toBe('#/nodes/3')
    expect(routeHash({ page: 'node', id: 3, tab: 'status' })).toBe('#/nodes/3/status')
    expect(routeHash({ page: 'router' })).toBe('#/router')
    expect(routeHash({ page: 'router', tab: 'subscriptions' })).toBe('#/router')
    expect(routeHash({ page: 'router', tab: 'settings' })).toBe('#/router/settings')
    for (const hash of ['#/', '#/subscriptions', '#/settings', '#/nodes/7', '#/nodes/7/status', '#/nodes/7/settings', '#/router', '#/router/status', '#/router/settings']) {
      const r = parseRoute(hash)
      expect(routeHash(r)).toBe(hash)
    }
  })
})
