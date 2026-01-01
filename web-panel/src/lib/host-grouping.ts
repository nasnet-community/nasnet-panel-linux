import type { HostWithRelations } from "@/lib/types"

export interface HostGroup {
    key: string              // "info" | "node-{id}"
    label: string            // "Info Hosts" | node name
    countryCode?: string     // for flag emoji
    nodeId?: number
    hosts: HostWithRelations[]
}

function isInfoHost(host: HostWithRelations): boolean {
    return !!host.plan_id && !host.inbound_id
}

export function countryCodeToFlag(code: string): string {
    if (!code || code.length !== 2) return ""
    const offset = 0x1F1E6
    return String.fromCodePoint(
        code.toUpperCase().charCodeAt(0) - 65 + offset,
        code.toUpperCase().charCodeAt(1) - 65 + offset
    )
}

export function groupHostsByNode(hosts: HostWithRelations[]): HostGroup[] {
    const infoHosts: HostWithRelations[] = []
    const nodeMap = new Map<number, { name: string; countryCode: string; hosts: HostWithRelations[] }>()

    for (const host of hosts) {
        if (isInfoHost(host)) {
            infoHosts.push(host)
        } else {
            const nodeId = host.inbound?.node_id ?? 0
            const nodeName = host.inbound?.node?.name ?? `Node ${nodeId}`
            const countryCode = host.inbound?.node?.country_code ?? ""

            if (!nodeMap.has(nodeId)) {
                nodeMap.set(nodeId, { name: nodeName, countryCode, hosts: [] })
            }
            nodeMap.get(nodeId)!.hosts.push(host)
        }
    }

    const sortHosts = (a: HostWithRelations, b: HostWithRelations) =>
        a.priority - b.priority || a.id - b.id

    const groups: HostGroup[] = []

    // Info group always first
    if (infoHosts.length > 0) {
        groups.push({
            key: "info",
            label: "Info Hosts",
            hosts: infoHosts.sort(sortHosts),
        })
    }

    // Node groups sorted alphabetically
    const nodeEntries = Array.from(nodeMap.entries()).sort((a, b) =>
        a[1].name.localeCompare(b[1].name)
    )

    for (const [nodeId, data] of nodeEntries) {
        groups.push({
            key: `node-${nodeId}`,
            label: data.name,
            countryCode: data.countryCode,
            nodeId,
            hosts: data.hosts.sort(sortHosts),
        })
    }

    return groups
}
