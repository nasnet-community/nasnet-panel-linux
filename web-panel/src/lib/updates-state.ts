import type { PanelUpdate, UpdateStatus } from "@/lib/api/updates"

export type UpdateTone = "ok" | "warn" | "danger" | "off"

export type UpdateState =
    | "running" | "scheduled" | "failed" | "available" | "blocked"
    | "updated" | "check_failed" | "beta_waiting" | "off" | "unchecked" | "up_to_date"

export interface PanelLine {
    state: UpdateState
    tone: UpdateTone
    text: string
    hint?: string
}

const DAY = 24 * 60 * 60 * 1000

export const isPrerelease = (v: string) => /^v?\d+\.\d+\.\d+-/.test(v)

/** "12 Oct, 04:00" in the browser's time. */
export function dateTime(iso: string) {
    return new Date(iso).toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hour12: false })
}

/** "Tue 4 Nov, 04:00": a scheduled time, where the weekday matters. */
export function weekdayTime(iso: string) {
    return new Date(iso).toLocaleString(undefined, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hour12: false })
}

export function took(row: Pick<PanelUpdate, "started_at" | "finished_at">) {
    const s = Math.max(0, Math.round((Date.parse(row.finished_at) - Date.parse(row.started_at)) / 1000))
    return s < 90 ? `${s} s` : `${Math.round(s / 60)} min`
}

export const mb = (bytes: number) => `${Math.max(1, Math.round(bytes / (1 << 20)))} MB`

/** How a check or download got through. */
export function routeLabel(route: string | undefined, mirror: string) {
    if (route === "proxy") return "the outbound proxy"
    if (route === "mirror") {
        try { return new URL(mirror).host } catch { return "the mirror" }
    }
    return "github.com"
}

/** What the Panel version card leads with: the one thing that matters now. */
export function panelLine(st: UpdateStatus, now = Date.now()): PanelLine {
    const job = st.job && st.job.phase !== "done" ? st.job : undefined
    const last = st.history[0]
    const offer = st.available
    const check = st.check

    if (job) {
        return job.kind === "rollback"
            ? { state: "running", tone: "warn", text: `Rolling back to ${job.to}`, hint: `Started ${dateTime(job.started_at)}.` }
            : { state: "running", tone: "warn", text: `Updating to ${job.to}`, hint: `Started ${dateTime(job.started_at)}.` }
    }
    if (st.schedule) {
        return { state: "scheduled", tone: "warn", text: `${st.schedule.version} installs ${weekdayTime(st.schedule.at)}`, hint: "This server's users drop for about a minute then." }
    }
    if (last && last.kind === "update" && last.result !== "ok" && offer?.version === last.to) {
        return {
            state: "failed", tone: "danger",
            text: last.result === "rolled_back" ? `The update to ${last.to} was rolled back` : `The update to ${last.to} failed`,
            hint: last.reason,
        }
    }
    if (offer) {
        return {
            state: "available", tone: "warn", text: `${offer.version} is available`,
            // not-root installs say why in the card's notice
            hint: st.kind.name === "docker" || st.kind.name === "source" ? st.kind.why : `You run ${st.current}, on the ${st.channel} channel.`,
        }
    }
    if (check?.blocked && check.latest) {
        return { state: "blocked", tone: "warn", text: `${check.latest.version} is available`, hint: check.blocked }
    }
    if (last && last.result === "ok" && last.to === st.current && now - Date.parse(last.finished_at) < 7 * DAY) {
        return last.kind === "rollback"
            ? { state: "updated", tone: "ok", text: `Rolled back to ${last.to}`, hint: `${dateTime(last.finished_at)}. The database stays as it is.` }
            : { state: "updated", tone: "ok", text: `Updated to ${last.to}`, hint: `${dateTime(last.finished_at)} · took ${took(last)}. The database was backed up first.` }
    }
    if (check?.error) {
        return { state: "check_failed", tone: "off", text: "Couldn't check for updates", hint: check.error }
    }
    if (isPrerelease(st.current) && st.channel === "stable") {
        return { state: "beta_waiting", tone: "off", text: "No stable release is newer yet", hint: "You run a beta. The next stable is offered when it ships." }
    }
    if (!st.check_enabled) {
        return { state: "off", tone: "off", text: "Automatic checks are off", hint: check ? "The last check found nothing new." : "Check now looks once." }
    }
    if (!check) {
        return { state: "unchecked", tone: "off", text: "Not checked yet", hint: "The first check runs a few minutes after the panel starts." }
    }
    return {
        state: "up_to_date", tone: "ok", text: "Up to date",
        hint: check.latest ? `${check.latest.version} is the newest ${check.channel} release.` : undefined,
    }
}

/** Release notes as plain lines: headings, bullets and the min_from comment dropped. */
export function plainNotes(md: string): string[] {
    return md.split("\n")
        .map(l => l.trim())
        .filter(l => l && !l.startsWith("<!--") && !l.startsWith("#"))
        .map(l => l.replace(/^[-*]\s+/, "").replace(/\*\*|`/g, ""))
}
