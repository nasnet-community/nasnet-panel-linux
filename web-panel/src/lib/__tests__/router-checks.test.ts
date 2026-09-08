import { describe, expect, it } from "vitest"
import {
    CHECK_PRESETS,
    checkRows,
    groupOf,
    groupLossKey,
    groupRttKey,
    groupTargetsKey,
    isPreset,
    parseTargets,
    protoLabel,
    serializeTargets,
    slotLossKey,
    slotRttKey,
    slotTargetsKey,
    splitAddress,
    targetError,
} from "@/lib/router-checks"
import { isHiddenSettingKey } from "@/components/settings/settings-constants"

describe("groups and keys", () => {
    it("puts every domestic slot in the domestic group and the rest abroad", () => {
        expect(groupOf("domestic")).toBe("domestic")
        expect(groupOf("domestic4")).toBe("domestic")
        expect(groupOf("secondary")).toBe("foreign")
        expect(groupOf("secondary3")).toBe("foreign")
    })

    // These strings are a contract with health_config.go.
    it("names a line's own keys apart from its group's", () => {
        expect(slotTargetsKey("domestic2")).toBe("router_probe_targets_slot_domestic2")
        expect(slotRttKey("domestic2")).toBe("router_degraded_rtt_ms_slot_domestic2")
        expect(slotLossKey("domestic2")).toBe("router_degraded_loss_pct_slot_domestic2")
        expect(groupTargetsKey("domestic2")).toBe("router_probe_targets_domestic")
        expect(groupTargetsKey("secondary2")).toBe("router_probe_targets_foreign")
        expect(groupRttKey("secondary2")).toBe("router_degraded_rtt_ms_foreign")
        expect(groupLossKey("secondary2")).toBe("router_degraded_loss_pct_foreign")
        expect(groupLossKey("domestic2")).toBe("router_degraded_loss_pct_domestic")
    })

    it("never lets a line key collide with its group key", () => {
        expect(slotTargetsKey("domestic")).not.toBe(groupTargetsKey("domestic"))
    })
})

describe("parsing", () => {
    it("keeps the label the backend ignores", () => {
        const got = parseTargets('[{"address":"9.9.9.9:443","proto":"tcp","label":"Quad9"}]')
        expect(got).toEqual([{ address: "9.9.9.9:443", proto: "tcp", label: "Quad9" }])
    })

    it("round-trips through the wire form", () => {
        const ts = [
            { address: "1.1.1.1:443", proto: "tcp" as const },
            { address: "10.0.0.1:53", proto: "dns" as const, label: "Modem" },
        ]
        expect(parseTargets(serializeTargets(ts))).toEqual(ts)
    })

    it("treats junk and emptiness as no override", () => {
        expect(parseTargets("not json")).toEqual([])
        expect(parseTargets("{}")).toEqual([])
        expect(parseTargets("")).toEqual([])
        expect(parseTargets(undefined)).toEqual([])
    })

    it("drops entries with no address and defaults an odd protocol to tcp", () => {
        const got = parseTargets('[{"proto":"dns"},{"address":"8.8.8.8:443","proto":"quic"}]')
        expect(got).toEqual([{ address: "8.8.8.8:443", proto: "tcp" }])
    })
})

describe("validation", () => {
    it("accepts a literal v4 address and a real port", () => {
        expect(targetError("10.0.0.1", "53")).toBeNull()
        expect(targetError(" 1.1.1.1 ", "443")).toBeNull()
    })

    // The prober drops these without a word, so the form has to catch them.
    it("refuses what the prober would silently discard", () => {
        expect(targetError("dns.google", "53")).not.toBeNull()
        expect(targetError("2606:4700::1111", "53")).not.toBeNull()
        expect(targetError("10.0.0.300", "53")).not.toBeNull()
        expect(targetError("10.0.0", "53")).not.toBeNull()
        expect(targetError("", "53")).not.toBeNull()
        expect(targetError("10.0.0.1", "0")).not.toBeNull()
        expect(targetError("10.0.0.1", "70000")).not.toBeNull()
        expect(targetError("10.0.0.1", "https")).not.toBeNull()
    })
})

describe("display", () => {
    it("names the two protocols the way the dialog does", () => {
        expect(protoLabel("tcp")).toBe("TCP Conn")
        expect(protoLabel("dns")).toBe("DNS lookup")
    })

    it("splits a host and port so the port can be shown on its own", () => {
        expect(splitAddress("217.218.155.155:53")).toEqual({ host: "217.218.155.155", port: "53" })
    })

    it("lists presets first and the operator's own after, so rows never jump", () => {
        const own = { address: "10.0.0.1:53", proto: "dns" as const, label: "Modem" }
        const rows = checkRows("domestic", [CHECK_PRESETS.domestic[1], own])
        expect(rows.map((r) => r.address)).toEqual([
            "217.218.155.155:53",
            "178.22.122.100:53",
            "10.0.0.1:53",
        ])
        expect(isPreset("domestic", own)).toBe(false)
        expect(isPreset("domestic", CHECK_PRESETS.domestic[0])).toBe(true)
    })
})

describe("settings page", () => {
    it("hides the per-line keys, which are edited on the Router page", () => {
        expect(isHiddenSettingKey(slotTargetsKey("domestic2"))).toBe(true)
        expect(isHiddenSettingKey(slotRttKey("secondary3"))).toBe(true)
        expect(isHiddenSettingKey(slotLossKey("domestic"))).toBe(true)
    })

    it("leaves the group keys on the settings page", () => {
        expect(isHiddenSettingKey(groupTargetsKey("domestic"))).toBe(false)
        expect(isHiddenSettingKey(groupRttKey("secondary"))).toBe(false)
        expect(isHiddenSettingKey("router_degraded_loss_pct")).toBe(false)
        expect(isHiddenSettingKey(groupLossKey("domestic2"))).toBe(false)
        expect(isHiddenSettingKey("router_failover_domestic_to_vpn")).toBe(false)
    })
})
