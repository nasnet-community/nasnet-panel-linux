import { describe, expect, it } from "vitest"
import { collidesLocally, portForwardSummary } from "@/lib/api/network"
import type { PortForward } from "@/lib/types/network"

const base: PortForward = {
    id: 1,
    uplink_key: "",
    proto: "tcp",
    dport: 443,
    to_addr: "10.77.0.5",
    to_port: 443,
    comment: "",
    enabled: true,
}

describe("port forward display", () => {
    it("renders any-uplink forwards distinctly from per-uplink ones", () => {
        expect(portForwardSummary(base, {})).toContain("any uplink")

        const pinned: PortForward = { ...base, uplink_key: "aa:bb:cc:dd:ee:01" }
        expect(portForwardSummary(pinned, { "aa:bb:cc:dd:ee:01": "Domestic ISP" })).toContain(
            "Domestic ISP",
        )
    })

    it("falls back to the raw key when no label is known", () => {
        const pinned: PortForward = { ...base, uplink_key: "aa:bb:cc:dd:ee:01" }
        expect(portForwardSummary(pinned, {})).toContain("aa:bb:cc:dd:ee:01")
    })

    it("catches an obvious collision client-side before the round trip", () => {
        const existing = [base]
        expect(collidesLocally(existing, { proto: "tcp", dport: 443, uplink_key: "aa" })).toBe(true)
        expect(collidesLocally(existing, { proto: "udp", dport: 443, uplink_key: "aa" })).toBe(false)
        expect(collidesLocally(existing, { proto: "tcp", dport: 8080, uplink_key: "aa" })).toBe(
            false,
        )
    })

    it("ignores disabled rows when checking collisions", () => {
        const existing = [{ ...base, enabled: false }]
        expect(collidesLocally(existing, { proto: "tcp", dport: 443, uplink_key: "aa" })).toBe(false)
    })

    it("does not let a row collide with itself while being edited", () => {
        const existing = [{ ...base, uplink_key: "aa" }]
        expect(
            collidesLocally(existing, { proto: "tcp", dport: 443, uplink_key: "aa", id: 1 }),
        ).toBe(false)
    })
})
