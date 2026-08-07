import { describe, expect, it, vi, beforeEach, afterEach } from "vitest"
import { verdictSeverity, isRejected, remainingSeconds, confirmUrls } from "@/lib/api/network"
import type { Verdict } from "@/lib/types/network"

describe("verdict handling", () => {
    it("ranks reject above confirm above warn", () => {
        expect(verdictSeverity("reject")).toBeGreaterThan(verdictSeverity("confirm"))
        expect(verdictSeverity("confirm")).toBeGreaterThan(verdictSeverity("warn"))
    })

    it("treats any reject as blocking", () => {
        const vs: Verdict[] = [
            { rule: "V22", level: "warn", message: "role is tied to the port" },
            { rule: "V8", level: "reject", message: "enp1s0 already holds the lan role" },
        ]
        expect(isRejected(vs)).toBe(true)
        expect(isRejected(vs.slice(0, 1))).toBe(false)
    })
})

describe("confirm window", () => {
    beforeEach(() => vi.useFakeTimers())
    afterEach(() => vi.useRealTimers())

    it("counts down from the server deadline and never goes negative", () => {
        vi.setSystemTime(new Date(1_800_000_000_000))
        expect(remainingSeconds(1_800_000_090)).toBe(90)
        vi.setSystemTime(new Date(1_800_000_200_000))
        expect(remainingSeconds(1_800_000_090)).toBe(0)
    })
})

describe("dual-address confirm", () => {
    // The box may re-address itself mid-apply, so confirm has to be attempted
    // against both the old and the new address.
    it("produces one url per candidate origin", () => {
        const urls = confirmUrls("http://192.168.1.34:9761", "http://10.77.0.1:9761")
        expect(urls).toHaveLength(2)
        expect(urls[0]).toContain("/api/v1/network/confirm")
        expect(urls[1]).toContain("10.77.0.1")
    })

    it("deduplicates when both origins are the same", () => {
        expect(confirmUrls("http://a:9761", "http://a:9761")).toHaveLength(1)
    })
})
