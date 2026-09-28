import { ApiError } from "@/lib/api"
import type { CertType, DomainSource, NodeCertRow, StrictBlockerReason } from "@/lib/types"
import { formatRelativeTime } from "@/lib/utils"

export type Tone = "ok" | "warn" | "danger" | "muted"

export function expiryTone(days: number | null | undefined): Tone {
    if (days == null) return "muted"
    if (days <= 7) return "danger"
    if (days <= 30) return "warn"
    return "ok"
}

export const toneText: Record<Tone, string> = {
    ok: "text-emerald-500",
    warn: "text-amber-500",
    danger: "text-red-500",
    muted: "text-muted-foreground",
}

export const toneBar: Record<Tone, string> = {
    ok: "bg-emerald-500",
    warn: "bg-amber-500",
    danger: "bg-red-500",
    muted: "bg-muted-foreground/40",
}

export const toneBadge: Record<Tone, string> = {
    ok: "border-emerald-400/30 text-emerald-500",
    warn: "border-amber-400/30 text-amber-500",
    danger: "border-red-400/30 text-red-500",
    muted: "text-muted-foreground",
}

export function daysLabel(days: number | null | undefined): string {
    if (days == null) return "—"
    if (days < 0) return "Expired"
    if (days === 0) return "Today"
    return `${days}d`
}

/** Share of the validity window still ahead, 0–100. */
export function lifeLeftPct(notBefore: string, notAfter: string): number {
    const start = new Date(notBefore).getTime()
    const end = new Date(notAfter).getTime()
    if (!(end > start)) return 0
    return Math.max(0, Math.min(100, ((end - Date.now()) / (end - start)) * 100))
}

export interface PresentedState {
    label: string
    tone: Tone
    hint: string
}

export function presentedState(row: NodeCertRow): PresentedState {
    const p = row.presented
    if (!p && row.is_stealth) {
        return { label: "Renews on restart", tone: "muted", hint: "Stealth agents fetch a fresh certificate every time they start." }
    }
    if (!p) {
        return {
            label: "Not seen yet",
            tone: "muted",
            hint: row.online ? "The panel has not connected to this node since the update." : "The node is offline.",
        }
    }
    if (p.match === "current") {
        return { label: "Presents current", tone: "ok", hint: `Seen ${formatRelativeTime(p.seen_at)} over ${p.mode}.` }
    }
    if (p.match === "older") {
        return {
            label: p.revoked ? "Presents revoked cert" : "Presents older cert",
            tone: p.revoked ? "danger" : "warn",
            hint: "The node still uses a certificate that has been replaced. Rotate it.",
        }
    }
    return { label: "Unknown cert", tone: "danger", hint: "The node presents a certificate the panel has no record of. Rotate it." }
}

export function blockerLabel(reason: StrictBlockerReason): string {
    switch (reason) {
        case "not_seen": return "not seen yet"
        case "presents_revoked": return "presents a revoked cert"
        case "presents_unknown": return "presents an unknown cert"
        case "id_mismatch": return "cert belongs to another node"
    }
}

export function sourceLabel(source: DomainSource, nodes: string[]): string {
    switch (source) {
        case "acme-http": return "ACME · HTTP"
        case "acme-dns": return "ACME · DNS"
        case "file": return nodes.length ? `File on ${nodes.join(", ")}` : "File on node"
        case "uploaded": return "Uploaded"
    }
}

export function certTypeLabel(type: CertType): string {
    switch (type) {
        case "ca": return "CA"
        case "agent": return "Node"
        case "master": return "Master (legacy)"
        case "public": return "Public (legacy)"
    }
}

export function certErrorMessage(err: unknown): string {
    if (err instanceof ApiError) {
        switch (err.code) {
            case "CERT_AGENT_OUTDATED": return "Update the node agent to rotate its certificate."
            case "NODE_OFFLINE": return "The node is not connected."
            case "NODE_STEALTH": return "Stealth nodes get a new certificate every time they restart."
            case "CERT_INSTALL_REJECTED": return err.message
            case "STRICT_MODE_NOT_READY": return "Some nodes are not ready for strict mode yet."
            case "CA_PROTECTED": return "The CA certificate can't be revoked or deleted."
            case "CERT_CONFIRM_MISMATCH": return "The confirmation text does not match."
        }
        return err.message
    }
    return err instanceof Error ? err.message : "Something went wrong"
}

/** True when the server refused because the cert is a live node's current one. */
export function isCurrentCertRefusal(err: unknown): boolean {
    return err instanceof ApiError && err.code === "CERT_CONFIRM_MISMATCH"
}

export function downloadText(content: string, filename: string) {
    const url = URL.createObjectURL(new Blob([content], { type: "application/x-pem-file" }))
    const a = document.createElement("a")
    a.href = url
    a.download = filename
    a.click()
    URL.revokeObjectURL(url)
}
