import type { MtprotoAllowlist, MtprotoNode } from "@/lib/api/mtproto"
import type { Node } from "@/lib/types"
import type { VPNPoolStatus } from "@/lib/types/network"

export type IssueLevel = "error" | "warn" | "info"

/** What the operator can do about an issue, from the card it shows on. */
export type IssueAction = "repair" | "restart" | "start-proxy" | "update-agent" | "update-proxy" | "refresh-list" | "tunnels" | "settings" | "logs" | "vpn"

export interface Issue {
    id: string
    level: IssueLevel
    title: string
    detail?: string
    action?: IssueAction
}

// past two minutes the link handshake fails, so warn well before that
export const SKEW_WARN_MS = 60_000

export function worstSkew(r: MtprotoNode): number {
    return (r.link?.conns ?? []).reduce((w, c) => (Math.abs(c.skew_ms ?? 0) > Math.abs(w) ? (c.skew_ms ?? 0) : w), 0)
}

/** The start of a build's SHA-256: enough to tell two builds apart. */
export function shortBuild(build?: string): string {
    return build ? build.slice(0, 8) : ""
}

/** The version an agent update brings, or "" when it's a new build of the one running. */
export function newAgentVersion(r: MtprotoNode): string {
    const running = (r.agent?.agent_version || r.agent_version || "").replace(/^v/, "")
    const latest = r.latest?.agent ?? ""
    return latest && latest.replace(/^v/, "") !== running ? latest : ""
}

/** The panel's own traffic, Telegram's included, leaves through the VPN when one is set up: true when none of its tunnels is connected. */
export function vpnDown(vpn?: Pick<VPNPoolStatus, "tunnels">): boolean {
    return !!vpn && vpn.tunnels.length > 0 && !vpn.tunnels.some((t) => t.connected)
}

const ORDER: Record<IssueLevel, number> = { error: 0, warn: 1, info: 2 }

/** Everything worth the operator's attention, worst first. Empty means all is well. */
export function mtprotoIssues(node: Node, r: MtprotoNode, allow?: MtprotoAllowlist, noVPN = false): Issue[] {
    const out: Issue[] = []
    const conns = r.link?.conns ?? []
    const up = r.link?.up ?? 0
    const a = r.agent

    if (!node.is_active) {
        out.push({ id: "disabled", level: "info", title: "This node is disabled", detail: "Its users can't connect until you enable it.", action: "settings" })
    } else if (!r.running) {
        out.push({ id: "not-running", level: "warn", title: "The panel isn't running this node yet", detail: "It starts within half a minute." })
    } else if (up === 0) {
        const err = conns.find((c) => c.error)?.error
        out.push({ id: "link-down", level: "error", title: "The panel can't reach the MTProto agent", detail: err || "No link connection is up.", action: "repair" })
    } else if (up < conns.length) {
        const down = conns.filter((c) => !c.up)
        out.push({
            id: "link-partial", level: "warn",
            title: `${down.length} of ${conns.length} link connections ${down.length === 1 ? "is" : "are"} down`,
            detail: down.find((c) => c.error)?.error,
        })
    }

    // one cause, one line: the list's lookup error would only say "i/o timeout"
    if (noVPN) {
        out.push({ id: "vpn-down", level: "error", title: "The VPN isn't connected", detail: "The panel reaches Telegram through it, so users can't reach Telegram and the address list can't refresh.", action: "vpn" })
    }
    if (allow?.placeholder) {
        out.push({ id: "placeholder", level: "error", title: "The Telegram address list is a placeholder", detail: "Users can't reach Telegram until it's replaced." })
    } else if (allow?.last_error && !noVPN) {
        out.push({ id: "list-error", level: "warn", title: "The Telegram address list didn't refresh", detail: allow.last_error, action: "refresh-list" })
    }

    if (a && up > 0) {
        if (r.proxy_stopped && !a.telemt.running) {
            out.push({ id: "proxy-off", level: "info", title: "The proxy is stopped", detail: "Users can't connect until you start it.", action: "start-proxy" })
        } else if (r.proxy_stopped && r.desired_rev && r.applied_rev === r.desired_rev) {
            // it took the state and kept running: an agent from before Stop
            out.push({ id: "stop-ignored", level: "warn", title: "The proxy is still running", detail: "This agent can't stop it. Update the agent first.", action: "update-agent" })
        } else if (a.telemt.crash_loop) {
            out.push({ id: "crash-loop", level: "error", title: "The proxy keeps crashing", detail: a.telemt.last_exit || "No reason given.", action: "logs" })
        } else if (!a.telemt.running && a.telemt.held) {
            out.push({ id: "held", level: "warn", title: "The agent is holding the proxy", detail: capital(a.telemt.held), action: "logs" })
        } else if (!a.telemt.running && r.users === 0) {
            out.push({ id: "no-users", level: "info", title: "The proxy is waiting for users", detail: "It starts when the MTProto tunnel has a user.", action: "tunnels" })
        } else if (!a.telemt.running && r.users === undefined) {
            out.push({ id: "proxy-stopped", level: "info", title: "The proxy is stopped", detail: "It stops by itself while the tunnel has no users.", action: "tunnels" })
        } else if (!a.telemt.running) {
            out.push({ id: "proxy-down", level: "warn", title: "The proxy isn't running", detail: a.telemt.last_exit, action: "restart" })
        }
        if (!a.capture.rules) {
            out.push({ id: "capture", level: "error", title: "The traffic capture rules are missing", detail: "Telegram traffic can't reach the panel. A repair puts them back.", action: "repair" })
        }
        if (a.disguise.checked_at && !a.disguise.ok) {
            out.push({ id: "cover", level: "warn", title: `The cover site ${a.disguise.domain} isn't answering`, detail: a.disguise.err, action: "tunnels" })
        }
    }

    if (r.desired_rev && r.applied_rev && r.desired_rev !== r.applied_rev) {
        out.push({ id: "drift", level: "warn", title: "The agent hasn't applied the latest settings", detail: r.last_push_error })
    }
    if (r.cover_pending) {
        out.push({ id: "cover-pending", level: "warn", title: "The new cover site isn't in use yet", detail: r.cover_pending })
    }
    const skew = worstSkew(r)
    if (Math.abs(skew) > SKEW_WARN_MS) {
        out.push({ id: "skew", level: "warn", title: `The node's clock is off by ${Math.round(Math.abs(skew) / 1000)} s`, detail: "Turn on NTP on the node: past two minutes the link stops working." })
    }

    const agentVersion = a?.agent_version || r.agent_version
    const telemtVersion = a?.telemt_version || r.telemt_version
    if (r.updates?.agent) {
        const next = newAgentVersion(r)
        const build = shortBuild(a?.agent_build)
        out.push({
            id: "agent-update", level: "info",
            title: next ? `MTProto agent ${next} is available` : "A new MTProto agent build is available",
            detail: agentVersion ? `This node runs ${agentVersion}${build ? ` (build ${build})` : ""}.` : undefined,
            action: "update-agent",
        })
    }
    if (r.updates?.telemt && r.latest?.telemt) {
        out.push({ id: "proxy-update", level: "info", title: `Proxy ${r.latest!.telemt} is available`, detail: `This node runs ${telemtVersion}.`, action: "update-proxy" })
    }

    return out.sort((x, y) => ORDER[x.level] - ORDER[y.level])
}

function capital(s: string): string {
    return s ? s[0].toUpperCase() + s.slice(1) : s
}

export type ProxyState = "running" | "waiting" | "held" | "off" | "stopped" | "crashing" | "unknown"

/** The proxy's state in one word, for the card and the nodes list. */
/** stopped: the operator stopped it from the panel. */
export function proxyState(t: { running: boolean; crash_loop?: boolean; held?: string } | undefined, users: number | undefined, linkUp: boolean, stopped?: boolean): ProxyState {
    if (!t || !linkUp) return "unknown"
    if (stopped && !t.running) return "off"
    if (t.crash_loop) return "crashing"
    if (t.running) return "running"
    if (t.held) return "held"
    // telemt won't start with no users, so the agent stops it without a reason
    return users === 0 || users === undefined ? "waiting" : "stopped"
}

export const PROXY_LABEL: Record<ProxyState, string> = {
    running: "Running",
    waiting: "Waiting for users",
    held: "Held",
    off: "Stopped",
    // users but no proxy: a fault, unlike a stop from the panel
    stopped: "Not running",
    crashing: "Crashing",
    unknown: "Unknown",
}
