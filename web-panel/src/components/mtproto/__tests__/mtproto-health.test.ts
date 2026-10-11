import { describe, expect, it } from "vitest"
import { proxyState, mtprotoIssues, newAgentVersion, shortBuild, vpnDown } from "../overview/mtproto-health"
import type { MtprotoNode } from "@/lib/api/mtproto"
import type { Node } from "@/lib/types"

const node = { id: 4, is_active: true, is_online: true } as Node

function mtproto(over: Partial<MtprotoNode> = {}): MtprotoNode {
    return {
        node_id: 4, agent_id: "a", hub_port: 8443, capture_port: 9443, ssh_port: 22, ssh_user: "root", host_key: "",
        disguise: "www.example.ir", max_streams: 5000, arch: "amd64", agent_version: "0.1.0", telemt_version: "3.4.25",
        running: true, healthy: true, users: 2,
        link: { up: 1, conns: [{ name: "domestic-0", up: true, rtt_ms: 30, streams: 0 }], streams: 0, relayed: 0, dial_failures: 0, violations: 0, bytes_to_dc: 0, bytes_from_dc: 0 },
        agent: {
            agent_version: "0.1.0", telemt_version: "3.4.25", state_rev: "r", uptime_sec: 1, rss_bytes: 1, warm: true,
            telemt: { running: true, restarts: 0, uptime_sec: 1 }, capture: { rules: true, active: 0, rejected: 0 },
            disguise: { domain: "www.example.ir", ok: true },
        },
        ...over,
    }
}

const ids = (r: MtprotoNode, n: Node = node) => mtprotoIssues(n, r).map((i) => i.id)

describe("mtprotoIssues", () => {
    it("is empty for a healthy node", () => {
        expect(ids(mtproto())).toEqual([])
    })

    it("names a disconnected VPN instead of the list's lookup error", () => {
        const allow = { prefixes: 14, placeholder: false, last_error: "lookup core.telegram.org: i/o timeout" }
        expect(mtprotoIssues(node, mtproto(), allow).map((i) => i.id)).toEqual(["list-error"])
        const issues = mtprotoIssues(node, mtproto(), allow, true)
        expect(issues.map((i) => i.id)).toEqual(["vpn-down"])
        expect(issues[0]).toMatchObject({ level: "error", title: "The VPN isn't connected", action: "vpn" })
    })

    it("reports a disabled node and nothing about its link", () => {
        expect(ids(mtproto({ running: false, link: undefined }), { ...node, is_active: false })).toEqual(["disabled"])
    })

    it("tells a proxy waiting for users from one that failed", () => {
        const stopped = { running: false, restarts: 0, uptime_sec: 0 }
        expect(ids(mtproto({ users: 0, agent: { ...mtproto().agent!, telemt: stopped } }))).toEqual(["no-users"])
        expect(ids(mtproto({ users: 3, agent: { ...mtproto().agent!, telemt: { ...stopped, last_exit: "exit 1" } } }))).toEqual(["proxy-down"])
    })

    it("sorts errors before warnings before notes", () => {
        const levels = mtprotoIssues(node, mtproto({
            latest: { agent: "0.2.0", telemt: "3.4.25" }, updates: { agent: true, telemt: false },
            desired_rev: "b", applied_rev: "a",
            agent: { ...mtproto().agent!, capture: { rules: false, active: 0, rejected: 0 } },
        })).map((i) => i.level)
        expect(levels).toEqual(["error", "warn", "info"])
    })
})

describe("proxyState", () => {
    it("is unknown without the link", () => {
        expect(proxyState({ running: true }, 1, false)).toBe("unknown")
    })
    it("calls a userless stop waiting, and a held one held", () => {
        expect(proxyState({ running: false }, 0, true)).toBe("waiting")
        expect(proxyState({ running: false, held: "x" }, 0, true)).toBe("held")
        expect(proxyState({ running: false }, 4, true)).toBe("stopped")
        expect(proxyState({ running: false, crash_loop: true }, 4, true)).toBe("crashing")
    })
})

describe("agent updates", () => {
    it("names the new version, or none for a new build of the same one", () => {
        expect(newAgentVersion(mtproto({ agent_version: "0.1.0", agent: undefined, latest: { agent: "0.2.0", telemt: "3.4.25" } }))).toBe("0.2.0")
        expect(newAgentVersion(mtproto({ agent_version: "v0.2.0", agent: undefined, latest: { agent: "0.2.0", telemt: "3.4.25" } }))).toBe("")
        expect(shortBuild("0123456789abcdef")).toBe("01234567")
        expect(shortBuild(undefined)).toBe("")
    })

    it("follows the panel: no note when the node runs what it installs", () => {
        const latest = { agent: "0.2.0", telemt: "3.4.25" }
        expect(ids(mtproto({ latest, updates: { agent: false, telemt: false } }))).not.toContain("agent-update")
        const notes = mtprotoIssues(node, mtproto({
            agent_version: "0.2.0", latest, updates: { agent: true, telemt: false },
            agent: { ...mtproto().agent!, agent_version: "0.2.0", agent_build: "abcdef0123456789" },
        }))
        const note = notes.find((i) => i.id === "agent-update")
        expect(note?.title).toBe("A new MTProto agent build is available")
        expect(note?.detail).toBe("This node runs 0.2.0 (build abcdef01).")
    })
})

describe("a stop from the panel", () => {
    it("reads as stopped, not as a fault", () => {
        expect(proxyState({ running: false, held: "stopped from the panel" }, 3, true, true)).toBe("off")
        expect(proxyState({ running: false }, 3, true)).toBe("stopped")
        const off = mtproto({ proxy_stopped: true, agent: { ...mtproto().agent!, telemt: { running: false, restarts: 0, uptime_sec: 0, held: "stopped from the panel" } } })
        expect(ids(off)).toContain("proxy-off")
        expect(ids(off)).not.toContain("held")
    })

    it("says so when an old agent kept the proxy running", () => {
        const old = mtproto({ proxy_stopped: true, desired_rev: "b", applied_rev: "b" })
        expect(ids(old)).toContain("stop-ignored")
        // still on its way: nothing to say yet
        expect(ids(mtproto({ proxy_stopped: true, desired_rev: "b", applied_rev: "a" }))).not.toContain("stop-ignored")
    })
})

describe("vpnDown", () => {
    const tunnel = (connected: boolean) => ({ connected }) as never
    it("is down only when tunnels are set up and none is connected", () => {
        expect(vpnDown(undefined)).toBe(false)
        expect(vpnDown({ tunnels: [] })).toBe(false)
        expect(vpnDown({ tunnels: [tunnel(false), tunnel(false)] })).toBe(true)
        expect(vpnDown({ tunnels: [tunnel(false), tunnel(true)] })).toBe(false)
    })
})
