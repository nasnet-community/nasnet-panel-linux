import { describe, it, expect } from "vitest"
import { buildChartPoints, UNIT_FACTOR } from "../usage-trend-transforms"
import type { UsageTrendPoint } from "@/lib/types/sub-panel"

describe("buildChartPoints", () => {
  it("fills gaps with zero bars for a 7d window", () => {
    const today = new Date("2026-04-19T00:00:00Z")
    const points: UsageTrendPoint[] = [
      { date: "2026-04-18", upload: 1_000_000, download: 4_000_000, total: 5_000_000 },
    ]
    const chart = buildChartPoints(points, 7, today)
    expect(chart).toHaveLength(7)
    expect(chart[0].total).toBe(0)
    expect(chart[5].total).toBe(5_000_000)
  })

  it("marks legacy rows (null splits) with legacyTotal", () => {
    const today = new Date("2026-04-19T00:00:00Z")
    const points: UsageTrendPoint[] = [
      { date: "2026-04-19", upload: null, download: null, total: 7_000_000 },
    ]
    const chart = buildChartPoints(points, 7, today)
    const last = chart[chart.length - 1]
    expect(last.isLegacy).toBe(true)
    expect(last.legacyTotal).toBe(7_000_000)
    expect(last.upload).toBe(0)
    expect(last.download).toBe(0)
  })

  it("non-legacy rows do not emit legacyTotal", () => {
    const today = new Date("2026-04-19T00:00:00Z")
    const points: UsageTrendPoint[] = [
      { date: "2026-04-19", upload: 1_000_000, download: 2_000_000, total: 3_000_000 },
    ]
    const chart = buildChartPoints(points, 7, today)
    const last = chart[chart.length - 1]
    expect(last.isLegacy).toBe(false)
    expect(last.legacyTotal).toBe(0)
    expect(last.upload).toBe(1_000_000)
    expect(last.download).toBe(2_000_000)
  })

  it("UNIT_FACTOR is binary (1024-based) to match backend FormatBytes", () => {
    expect(UNIT_FACTOR.KB).toBe(1024)
    expect(UNIT_FACTOR.MB).toBe(1024 * 1024)
    expect(UNIT_FACTOR.GB).toBe(1024 * 1024 * 1024)
  })
})
