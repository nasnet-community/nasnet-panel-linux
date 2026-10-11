import type { MtprotoNode } from "@/lib/api/mtproto"

/** What each step of an install, repair, update or removal is called to a human. */
export const STEP_LABELS: Record<string, string> = {
    download: "Fetching the pinned releases",
    check: "Checking the node",
    verify: "Verifying what will be uploaded",
    nftables: "Installing nftables",
    account: "Creating the proxy's account",
    upload: "Uploading",
    directories: "Preparing folders",
    disguise: "Testing the cover site from the node",
    credentials: "Writing the link credentials",
    service: "Starting the agent",
    register: "Adding the node",
    link: "Waiting for the agent to answer",
    remove: "Removing the agent",
    forget: "Removing the node",
    rotate: "Switching keys",
    restart: "Restarting the agent",
    commit: "Confirming the new version",
}

/** Steps that mean something else in one kind of job. */
const KIND_LABELS: Record<string, Record<string, string>> = {
    rotate: { verify: "Checking the new keys work" },
    "link-port": { open: "Opening the new port on the node", switch: "Reconnecting on the new port", close: "Closing the old port" },
    address: { reach: "Reaching the agent at the new address" },
}

export function stepLabel(name: string, kind?: string): string {
    return (kind ? KIND_LABELS[kind]?.[name] : undefined) ?? STEP_LABELS[name] ?? name.replace(/_/g, " ")
}

/** True for a plausible domain name; the panel validates again. */
export function looksLikeDomain(s: string): boolean {
    return /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/i.test(s.trim())
}

/** Starts a job from a user action, never from an effect: a StrictMode remount would start it twice. */
export function startMtprotoJob(begin: () => Promise<string>): Promise<string> {
    const p = begin()
    p.catch(() => {}) // the dialog shows the failure
    return p
}

/** Why a link port can't be used, or "" when it can; the agent checks the proxy's port itself. */
export function linkPortProblem(raw: string, r: Pick<MtprotoNode, "hub_port" | "capture_port" | "ssh_port">): string {
    const s = raw.trim()
    if (!/^\d+$/.test(s)) return "Enter a port number."
    const port = Number(s)
    if (port < 1 || port > 65535) return "Use a port from 1 to 65535."
    if (port === r.hub_port) return "The link already uses this port."
    if (port === r.capture_port) return "The agent's capture port can't be shared."
    if (port === r.ssh_port) return "That's the node's SSH port."
    return ""
}
