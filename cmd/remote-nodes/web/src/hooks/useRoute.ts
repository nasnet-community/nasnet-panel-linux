import { useEffect, useState } from 'react'

export type RootTab = 'nodes' | 'subscriptions' | 'settings'
export type NodeTab = 'subscriptions' | 'status' | 'settings'

export type Route = { page: 'root'; tab: RootTab } | { page: 'node'; id: string; tab: NodeTab }

/** Where a link or navigate() goes; a node's id may be a number, its tab defaults to Subscriptions. */
export type Target = { page: 'root'; tab: RootTab } | { page: 'node'; id: string | number; tab?: NodeTab }

const NODE_TABS: NodeTab[] = ['subscriptions', 'status', 'settings']

export function parseRoute(hash: string): Route {
  const [, page, id, tab] = hash.replace(/^#/, '').split('/')
  if (page === 'nodes' && /^\d+$/.test(id ?? '')) {
    return { page: 'node', id, tab: NODE_TABS.includes(tab as NodeTab) ? (tab as NodeTab) : 'subscriptions' }
  }
  if (page === 'subscriptions' || page === 'settings') return { page: 'root', tab: page }
  return { page: 'root', tab: 'nodes' }
}

/** "#/", "#/subscriptions", "#/nodes/3", "#/nodes/3/status": the inverse of parseRoute. */
export function routeHash(to: Target): string {
  if (to.page === 'root') return to.tab === 'nodes' ? '#/' : `#/${to.tab}`
  return `#/nodes/${to.id}${!to.tab || to.tab === 'subscriptions' ? '' : `/${to.tab}`}`
}

export function navigate(to: Target) {
  window.location.hash = routeHash(to).slice(1)
}

export function useRoute(): Route {
  const [route, setRoute] = useState(() => parseRoute(window.location.hash))
  useEffect(() => {
    const onChange = () => setRoute(parseRoute(window.location.hash))
    window.addEventListener('hashchange', onChange)
    return () => window.removeEventListener('hashchange', onChange)
  }, [])
  return route
}
