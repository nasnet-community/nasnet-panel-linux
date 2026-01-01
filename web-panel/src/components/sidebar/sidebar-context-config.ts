export type ContextStatus = "ok" | "warn" | "down"

export type ContextConfigId =
    | "system"
    | "customers"
    // | "chats"
    | "nodes"
    | "certs"
    | "alerts"

export interface ContextConfig {
    id: ContextConfigId
    /** Label shown at the top of the panel, e.g. "NODES.STATUS". */
    label: string
    /** URL prefixes this config claims. Matching is longest-prefix. */
    prefixes: string[]
}

/**
 * Route-prefix → panel config. Order does not matter; matching is
 * longest-prefix to handle nested/overlapping prefixes cleanly.
 * Routes absent from this list render no panel.
 */
export const CONTEXT_CONFIGS: ContextConfig[] = [
    {
        id: "system",
        label: "SYSTEM.STATUS",
        prefixes: ["/dashboard"],
    },
    {
        id: "customers",
        label: "CUSTOMERS",
        prefixes: ["/users", "/subscriptions", "/accounts"],
    },
    // {
    //     id: "chats",
    //     label: "CHATS",
    //     prefixes: ["/chats"],
    // },
    {
        id: "nodes",
        label: "NODES.STATUS",
        prefixes: ["/server", "/nodes", "/hosts", "/access-logs", "/access-history"],
    },
    {
        id: "certs",
        label: "CERTS",
        prefixes: ["/certificates", "/domains"],
    },
    {
        id: "alerts",
        label: "ALERTS",
        prefixes: ["/alerts"],
    },
]

function pathMatchesPrefix(pathname: string, prefix: string): boolean {
    if (pathname === prefix) return true
    return pathname.startsWith(prefix + "/")
}

export function getContextConfig(pathname: string): ContextConfig | null {
    let best: { config: ContextConfig; length: number } | null = null
    for (const config of CONTEXT_CONFIGS) {
        for (const prefix of config.prefixes) {
            if (pathMatchesPrefix(pathname, prefix)) {
                if (!best || prefix.length > best.length) {
                    best = { config, length: prefix.length }
                }
            }
        }
    }
    return best?.config ?? null
}
