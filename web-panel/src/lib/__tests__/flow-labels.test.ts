import { describe, expect, it } from "vitest"
import {
    decodeMark,
    eventTone,
    formatBytes,
    formatRate,
    relativeTime,
    traceVerdictLabel,
} from "@/lib/flow-labels"

describe("decodeMark", () => {
    it("splits the group and pin fields", () => {
        expect(decodeMark(0x2020000)).toEqual({ group: "foreign", pin: 2, hex: "0x2020000" })
        expect(decodeMark(0x10000)).toEqual({ group: "domestic", pin: 0, hex: "0x10000" })
    })

    it("says nothing about an unmarked packet", () => {
        expect(decodeMark(0)).toEqual({ group: "", pin: 0, hex: "0x0" })
    })
})

describe("formatBytes", () => {
    it("scales", () => {
        expect(formatBytes(512)).toBe("512 B")
        expect(formatBytes(2048)).toBe("2.0 KB")
        expect(formatBytes(3 * 1024 * 1024)).toBe("3.0 MB")
    })
})

describe("formatRate", () => {
    it("is per-second, and says idle rather than 0", () => {
        expect(formatRate(0)).toBe("idle")
        expect(formatRate(1536)).toBe("1.5 KB/s")
    })
})

describe("relativeTime", () => {
    it("reads like a log", () => {
        const now = new Date("2026-08-14T12:00:00Z").getTime()
        expect(relativeTime("2026-08-14T11:59:30Z", now)).toBe("30s ago")
        expect(relativeTime("2026-08-14T11:30:00Z", now)).toBe("30m ago")
        expect(relativeTime("2026-08-14T09:00:00Z", now)).toBe("3h ago")
    })
})

describe("traceVerdictLabel", () => {
    it("names the outcome in the operator's terms", () => {
        expect(traceVerdictLabel("delivered-vpn")).toMatch(/VPN/)
        expect(traceVerdictLabel("dropped")).toMatch(/never leaves/)
    })
})

describe("eventTone", () => {
    it("colours trouble", () => {
        expect(eventTone("vpn.down")).toBe("bad")
        expect(eventTone("wan.up")).toBe("ok")
        expect(eventTone("wan.applied")).toBe("muted")
    })
})
