// The subscription calls of the mock API that change something: create, rename, delete, the
// per-node switch, and the links. Listing stays in mock-api.ts with the rest of the sample data.
//
// A subscription has one link per node, each with its own id. The id stays through a switch off
// and on again and changes when the link is replaced, as the contract says. Sara's link on
// Node 1 is the one the design boards show.
//
// Errors follow the contract: subscription_limit and name_taken (409), not_found (404), busy (409,
// a node still being installed), bad_request (400). Switching on a node that can't be reached is
// accepted: the hub keeps the choice and applies it when the node is back.
import { randomUUID } from 'node:crypto'
import type { Link, Node, Subscription } from './src/lib/api'

interface Sub {
  id: number
  name: string
  /** the nodes it is switched on for */
  on: Set<number>
}

interface HostNode {
  id: number
  registered: boolean
  /** the install job, while it runs */
  installJob?: string
}

export interface SubscriptionCall<S extends Sub, N extends HostNode> {
  method: string
  /** without the leading slash and the query: "subscriptions/3/nodes/1/link" */
  path: string
  body: Record<string, any>
  send: (status: number, data: unknown) => void
  fail: (status: number, code: string, error: string, extra?: object) => void
  /** the live list: this adds to it and removes from it */
  subs: S[]
  nodes: N[]
  max: number
  /** a subscription with the next free id, not yet in the list */
  make: (name: string, o: { createdAt: number; on: Set<number> }) => S
  view: (s: S) => Subscription
  nodeView: (n: N) => Node
}

const SARA_ON_NODE_1 = '3f0c9a52-7d1e-4b8a-9c44-0e5b2a1d6f73'

// by subscription object, so a reset (which makes new ones) starts every link afresh
const ids = new WeakMap<object, Map<number, string>>()

function linkId(s: Sub, nodeId: number, replace = false) {
  let own = ids.get(s)
  if (!own) ids.set(s, (own = new Map()))
  if (replace || !own.has(nodeId)) own.set(nodeId, !replace && s.id === 1 && nodeId === 1 ? SARA_ON_NODE_1 : randomUUID())
  return own.get(nodeId)!
}

const vless = (n: Node, id: string, name: string) =>
  `vless://${id}@${n.host}:${n.entryPort}?encryption=none&flow=xtls-rprx-vision&security=reality&sni=${n.sni}&fp=chrome&pbk=${n.publicKey}&sid=${n.shortId}&type=tcp#${encodeURIComponent(name)}`

/** Answers the call and returns true when it is one of these, else returns false and touches nothing. */
export function subscriptionRoutes<S extends Sub, N extends HostNode>(c: SubscriptionCall<S, N>): boolean {
  const { method, path, body, subs, nodes } = c
  const reply = (status: number, data: unknown) => (c.send(status, data), true)
  const refuse = (status: number, code: string, error: string) => (c.fail(status, code, error), true)
  const taken = (name: string, except?: S) => subs.some((x) => x !== except && x.name.toLowerCase() === name.toLowerCase())
  const name = typeof body.name === 'string' ? body.name.trim() : ''
  /** what is wrong with the name in the body, if anything */
  const badName = () =>
    !name ? refuse(400, 'bad_request', 'give the subscription a name')
    : name.length > 100 ? refuse(400, 'bad_request', 'the name must be at most 100 characters')
    : false

  if (path === 'subscriptions' && method === 'POST') {
    if (badName()) return true
    const want: unknown = body.nodeIds
    if (!Array.isArray(want) || !want.every((x) => Number.isInteger(x))) return refuse(400, 'bad_request', 'nodeIds must be a list of node ids')
    if (subs.length >= c.max) return refuse(409, 'subscription_limit', `this router holds at most ${c.max} subscriptions`)
    if (taken(name)) return refuse(409, 'name_taken', `a subscription named ${name} already exists`)
    for (const id of want as number[]) {
      const n = nodes.find((x) => x.id === id)
      if (!n || !n.registered) return refuse(404, 'not_found', `no such node: ${id}`)
      if (n.installJob) return refuse(409, 'busy', 'this node is still being installed')
    }
    const created = c.make(name, { createdAt: Date.now(), on: new Set(want as number[]) })
    subs.push(created)
    return reply(201, c.view(created))
  }

  const m = path.match(/^subscriptions\/(\d+)(?:\/nodes\/(\d+)(?:\/(link|replace-link))?)?$/)
  if (!m) return false
  const s = subs.find((x) => x.id === Number(m[1]))
  const nodeId = m[2] === undefined ? undefined : Number(m[2])
  const action = m[3]

  if (nodeId === undefined && method === 'PATCH') {
    if (!s) return refuse(404, 'not_found', 'no such subscription')
    if (badName()) return true
    if (taken(name, s)) return refuse(409, 'name_taken', `a subscription named ${name} already exists`)
    s.name = name
    return reply(200, c.view(s))
  }

  if (nodeId === undefined && method === 'DELETE') {
    if (!s) return refuse(404, 'not_found', 'no such subscription')
    subs.splice(subs.indexOf(s), 1)
    return reply(200, { ok: true })
  }

  // the switch: off removes the node's client, on adds it back
  if (nodeId !== undefined && !action && method === 'PUT') {
    if (!s) return refuse(404, 'not_found', 'no such subscription')
    const n = nodes.find((x) => x.id === nodeId)
    if (!n || !n.registered) return refuse(404, 'not_found', 'no such node')
    if (n.installJob) return refuse(409, 'busy', 'this node is still being installed')
    if (typeof body.enabled !== 'boolean') return refuse(400, 'bad_request', 'enabled must be true or false')
    if (body.enabled) s.on.add(nodeId)
    else s.on.delete(nodeId)
    return reply(200, c.view(s))
  }

  if (nodeId !== undefined && ((action === 'link' && method === 'GET') || (action === 'replace-link' && method === 'POST'))) {
    const n = nodes.find((x) => x.id === nodeId)
    // a link exists while the subscription is on for the node
    if (!s || !n || !n.registered || !s.on.has(nodeId)) return refuse(404, 'not_found', 'no link for this subscription on this node')
    const node = c.nodeView(n)
    const out: Link = { nodeId, nodeName: node.name, vless: vless(node, linkId(s, nodeId, action === 'replace-link'), s.name) }
    return reply(200, out)
  }

  return false
}
