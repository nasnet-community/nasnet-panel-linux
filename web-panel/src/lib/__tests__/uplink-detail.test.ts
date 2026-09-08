import { describe, expect, it } from "vitest"
import {
    bytesLabel,
    duration,
    hasSpeed,
    lineSubtitle,
    memberRole,
    rungMeaning,
    sinceLabel,
    slotPosition,
    speedLabel,
} from "@/lib/uplink-detail"
import type { HealthSample, UplinkHealth } from "@/lib/types/health"

function samples(n: number): HealthSample[] {
    return Array.from({ length: n }, (_, i) => ({ unix: i, ok_ratio: 1, rtt_ms: 20 }))
}

function up(over: Partial<UplinkHealth> = {}): UplinkHealth {
    return {
        slot: "domestic",
        if_name: "eth0",
        carrier: "up",
        gateway: "up",
        internet: "up",
        verdict: "up",
        via: "",
        force_state: "",
        degraded: false,
        loss_pct: 0,
        median_rtt_ms: 18,
        targets: [],
        history: samples(20),
        rx_bytes: 0,
        tx_bytes: 0,
        ...over,
    }
}

describe("rung meaning", () => {
    it("says what the carrier rung means, with the speed when known", () => {
        const ctx = { okTargets: 2, totalTargets: 2, speedMbit: 1000 }
        expect(rungMeaning("carrier", "up", ctx)).toBe("Cable connected · 1 Gbit/s")
        expect(rungMeaning("carrier", "up", { ...ctx, speedMbit: undefined })).toBe("Cable connected")
        expect(rungMeaning("carrier", "down", ctx)).toBe("No cable detected")
        expect(rungMeaning("carrier", "unknown", ctx)).toBe("Not checked yet")
    })

    // The gateway rung is a rung, not an address, so the address has to be
    // spelled out or the operator cannot tell what is being dialled.
    it("names the device the gateway rung dials", () => {
        const ctx = { gatewayIP: "10.20.0.1", okTargets: 2, totalTargets: 2 }
        expect(rungMeaning("gateway", "up", ctx)).toBe("10.20.0.1 answers")
        expect(rungMeaning("gateway", "down", ctx)).toBe("10.20.0.1 is not answering")
        expect(rungMeaning("gateway", "up", { okTargets: 0, totalTargets: 0 })).toBe(
            "The upstream router answers",
        )
    })

    it("counts the checks on the internet rung", () => {
        expect(rungMeaning("internet", "up", { okTargets: 2, totalTargets: 2 })).toBe(
            "Internet reachable · 2 of 2 checks answered",
        )
        expect(rungMeaning("internet", "down", { okTargets: 1, totalTargets: 2 })).toBe(
            "Not reachable · only 1 of 2 checks answered",
        )
        expect(rungMeaning("internet", "down", { okTargets: 0, totalTargets: 2 })).toBe(
            "Not reachable · nothing answered",
        )
    })
})

describe("since", () => {
    it("counts up from the last change, and says which way", () => {
        const now = 1_000_000_000_000
        const secs = Math.round(now / 1000)
        expect(sinceLabel(up({ since_unix: secs - 240 }), now)).toBe("up for 4 minutes")
        expect(
            sinceLabel(up({ verdict: "no-internet", since_unix: secs - 240 }), now),
        ).toBe("down for 4 minutes")
    })

    it("says nothing when the line has never changed", () => {
        expect(sinceLabel(up())).toBeNull()
    })

    it("scales the unit rather than printing 4320 minutes", () => {
        expect(duration(30)).toBe("30 seconds")
        expect(duration(60)).toBe("1 minute")
        expect(duration(3600)).toBe("1 hour")
        expect(duration(3600 * 72)).toBe("3 days")
    })
})

describe("bytes", () => {
    it("scales a cumulative counter", () => {
        expect(bytesLabel(0)).toBe("0 B")
        expect(bytesLabel(512)).toBe("512 B")
        expect(bytesLabel(1024 * 1024 * 412)).toBe("412 MB")
        expect(bytesLabel(1024 ** 3 * 1.21)).toBe("1.21 GB")
        // 61.0 MB reads like a measurement nobody took.
        expect(bytesLabel(1024 * 1024 * 61)).toBe("61 MB")
        expect(bytesLabel(1024 * 1024 * 10.5)).toBe("10.5 MB")
    })
})

describe("group roles", () => {
    const self = up({ if_name: "eth1", slot: "domestic2", via: "eth0", verdict: "no-internet" })

    it("says what this line is doing, in its own row", () => {
        expect(memberRole(self, self, "Fiber")).toBe("Sending its traffic through Fiber")
        expect(memberRole(self, { ...self, via: "pool" }, null)).toBe(
            "Sending its traffic through the VPN",
        )
    })

    it("says the carrier is carrying this line too", () => {
        const carrier = up({ if_name: "eth0", slot: "domestic" })
        expect(memberRole(self, carrier, null)).toBe("Carrying traffic now, including eth1's")
    })

    it("explains why a sibling cannot help", () => {
        const dead = up({ if_name: "wwan0", slot: "domestic3", verdict: "no-carrier" })
        expect(memberRole(self, dead, null)).toBe("No cable or SIM detected")
        const held = up({ if_name: "wwan0", slot: "domestic3", verdict: "forced-down" })
        expect(memberRole(self, held, null)).toBe("Held down by you")
    })

    it("calls an idle healthy sibling a standby", () => {
        const healthy = up({ if_name: "eth2", slot: "domestic3" })
        const alone = up({ if_name: "eth1", slot: "domestic2", verdict: "up" })
        expect(memberRole(alone, healthy, null)).toBe("Ready to take over")
    })
})

describe("identity", () => {
    it("reads priority off the slot number", () => {
        expect(slotPosition("domestic")).toBe(1)
        expect(slotPosition("domestic3")).toBe(3)
        expect(slotPosition("secondary")).toBe(1)
    })

    it("subtitles a line by what it is for", () => {
        expect(lineSubtitle("domestic", 2)).toBe("Main internet line · used first")
        expect(lineSubtitle("domestic2", 2)).toBe("Backup internet line · used second")
        expect(lineSubtitle("domestic", 0)).toBe("Internet line")
        expect(lineSubtitle("secondary", 1)).toBe("Foreign internet line · carries the VPN tunnels")
    })

    it("scales the link speed label", () => {
        expect(speedLabel(100)).toBe("100 Mbit/s")
        expect(speedLabel(1000)).toBe("1 Gbit/s")
    })

    // A virtio NIC reports -1, which rendered as "-1 Mbit/s".
    it("treats an unreported speed as unknown, not as a number", () => {
        expect(hasSpeed(1000)).toBe(true)
        expect(hasSpeed(-1)).toBe(false)
        expect(hasSpeed(0)).toBe(false)
        expect(hasSpeed(undefined)).toBe(false)
        expect(rungMeaning("carrier", "up", { okTargets: 2, totalTargets: 2, speedMbit: -1 })).toBe(
            "Cable connected",
        )
    })
})
