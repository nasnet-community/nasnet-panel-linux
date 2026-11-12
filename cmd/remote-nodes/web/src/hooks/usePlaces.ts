import { useMemo } from 'react'
import { useLang } from '../i18n'
import { api, local as localApi } from '../lib/api'
import { nodeInfos, placesOf, routerPlace, type NodeInfo, type Place } from '../lib/subscriptions'
import { usePoll } from './usePoll'

export interface Places {
  /** the router's own entry first (while it is on), then the nodes; null until both have loaded */
  places: Place[] | null
  nodes: NodeInfo[] | null
  /** what the last load of either threw */
  error: unknown
}

/** The places a subscription can be on, kept fresh: the nodes, and the router's own entry while it is switched on. */
export function usePlaces(): Places {
  const { t } = useLang()
  const nodeList = usePoll(api.nodes, 10000)
  const router = usePoll(localApi.get, 10000)
  const nodes = useMemo(() => (nodeList.data ? nodeInfos(nodeList.data) : null), [nodeList.data])
  const on = router.data?.enabled
  const state = router.data?.state
  const places = useMemo(() => (nodes && on !== undefined && state !== undefined ? placesOf(routerPlace(t, { enabled: on, state }), nodes) : null), [nodes, on, state, t])
  return { places, nodes, error: nodeList.error ?? router.error }
}
