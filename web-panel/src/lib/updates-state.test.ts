import { describe, expect, it } from "vitest"
import type { UpdateStatus } from "@/lib/api/updates"
import { panelLine, routeLabel } from "./updates-state"

const offer = { version: "v1.1.0", prerelease: false, notes: "", size: 118 << 20, sha256: "a", xray: "26.10.10", built_at: "2026-11-02T00:00:00Z" }
const base: UpdateStatus = {
    current: "v1.0.0", arch: "amd64", kind: { name: "systemd" }, channel: "stable", check_enabled: true, mirror_url: "", tested_xray: "26.10.10",
    history: [], parts: { geofiles: {} },
    check: { checked_at: "2026-11-02T03:48:00Z", channel: "stable", route: "direct", latest: offer, available: true },
    available: offer,
}
const st = (o: Partial<UpdateStatus>): UpdateStatus => ({ ...base, ...o })
const now = Date.parse("2026-11-02T04:10:00Z")

describe("the Panel version card", () => {
    it("offers a newer release", () => {
        expect(panelLine(base, now)).toMatchObject({ state: "available", tone: "warn", text: "v1.1.0 is available", hint: "You run v1.0.0, on the stable channel." })
    })

    it("says why a Docker install has no button", () => {
        const l = panelLine(st({ kind: { name: "docker", why: "Docker installs update through Docker." } }), now)
        expect(l).toMatchObject({ state: "available", hint: "Docker installs update through Docker." })
    })

    it("puts a running job first", () => {
        const job = { id: "j", kind: "update" as const, from: "v1.0.0", to: "v1.1.0", source: "download" as const, phase: "panel" as const, steps: [], started_at: "2026-11-02T04:00:00Z" }
        expect(panelLine(st({ job, schedule: { version: "v1.1.0", size: 1, at: "2026-11-04T04:00:00Z" } }), now).state).toBe("running")
        expect(panelLine(st({ job: { ...job, kind: "rollback", to: "v0.9.0" } }), now).text).toBe("Rolling back to v0.9.0")
        expect(panelLine(st({ job: { ...job, phase: "done" } }), now).state).toBe("available")
    })

    it("shows a scheduled update", () => {
        expect(panelLine(st({ schedule: { version: "v1.1.0", size: 1, at: "2026-11-04T04:00:00Z" } }), now).state).toBe("scheduled")
    })

    it("keeps a rolled back update in view while it's still on offer", () => {
        const row = { id: 1, job_id: "j", kind: "update" as const, from: "v1.0.0", to: "v1.1.0", result: "rolled_back" as const,
            reason: "Xray didn't start within 2 minutes", started_at: "2026-11-02T04:00:00Z", finished_at: "2026-11-02T04:03:00Z" }
        expect(panelLine(st({ history: [row] }), now)).toMatchObject({ state: "failed", tone: "danger", hint: "Xray didn't start within 2 minutes" })
        // once something newer is out, that's what counts
        expect(panelLine(st({ history: [row], available: { ...offer, version: "v1.1.1" } }), now).state).toBe("available")
    })

    it("tells a fresh update apart for a week", () => {
        const row = { id: 1, job_id: "j", kind: "update" as const, from: "v1.0.0", to: "v1.1.0", result: "ok" as const,
            started_at: "2026-11-02T04:00:00Z", finished_at: "2026-11-02T04:00:52Z" }
        const after = st({ current: "v1.1.0", available: undefined, history: [row], check: { ...base.check!, available: false, latest: { ...offer } } })
        expect(panelLine(after, now)).toMatchObject({ state: "updated", tone: "ok", text: "Updated to v1.1.0" })
        expect(panelLine(after, now).hint).toContain("took 52 s")
        expect(panelLine(after, now + 8 * 24 * 3600_000).state).toBe("up_to_date")
    })

    it("is quiet about a failed check", () => {
        const l = panelLine(st({ available: undefined, check: { checked_at: "x", channel: "stable", available: false, error: "connection timed out" } }), now)
        expect(l).toMatchObject({ state: "check_failed", tone: "off" })
    })

    it("names the step a release needs first", () => {
        const l = panelLine(st({ available: undefined, check: { ...base.check!, available: false, latest: { ...offer, version: "v1.3.0" }, blocked: "v1.3.0 needs v1.2.0 or later first." } }), now)
        expect(l).toMatchObject({ state: "blocked", text: "v1.3.0 is available", hint: "v1.3.0 needs v1.2.0 or later first." })
    })

    it("waits for a stable release after beta is turned off", () => {
        const l = panelLine(st({ current: "v1.2.0-rc.2", available: undefined, check: { ...base.check!, available: false, latest: { ...offer } } }), now)
        expect(l.state).toBe("beta_waiting")
    })

    it("says when automatic checks are off or never ran", () => {
        expect(panelLine(st({ check_enabled: false, available: undefined, check: undefined }), now).state).toBe("off")
        expect(panelLine(st({ available: undefined, check: undefined }), now).state).toBe("unchecked")
    })
})

describe("routes", () => {
    it("reads as the host a mirror is on", () => {
        expect(routeLabel("mirror", "https://ghproxy.example/")).toBe("ghproxy.example")
        expect(routeLabel("proxy", "")).toBe("the outbound proxy")
        expect(routeLabel("direct", "")).toBe("github.com")
    })
})
