import { describe, expect, it } from "vitest"
import { classifyObstructionCell, obstructionCellColor, OBSTRUCTION_COLORS, readTrend } from "./starlink-helpers"
import { mapOrientation } from "./starlink-obstruction-map"

describe("Starlink readings", () => {
    it("interprets improving throughput and falling latency as healthy trends", () => {
        expect(readTrend([10, 20, 40], "higher-better").tone).toBe("good")
        expect(readTrend([40, 20, 10], "higher-better").tone).toBe("bad")
        expect(readTrend([40, 20, 10], "lower-better").tone).toBe("good")
        expect(readTrend([0, 0.0001, 0], "lower-better", 0.001)).toEqual({ tone: "flat", deltaPct: 0, steady: true })
    })

    it("keeps unmeasured sky distinct and preserves intermediate obstruction readings", () => {
        for (const value of [null, undefined, NaN, -1]) {
            expect(classifyObstructionCell(value)).toBe("nodata")
            expect(obstructionCellColor(value)).toBe(OBSTRUCTION_COLORS.nodata)
        }
        expect(classifyObstructionCell(0.49)).toBe("obstructed")
        expect(classifyObstructionCell(0.5)).toBe("clear")
        expect(obstructionCellColor(0.5)).toBe("rgb(242,148,155)")
        expect(obstructionCellColor(0)).toBe(OBSTRUCTION_COLORS.obstructed)
        expect(obstructionCellColor(1)).toBe(OBSTRUCTION_COLORS.clear)
    })

    it("labels a dish-relative map only with a trusted heading", () => {
        const data = { snr: [], num_rows: 0, num_cols: 0, reference_frame: "FRAME_UT", boresight_azimuth_deg: 90 }
        const trusted = mapOrientation({ ...data, attitude_estimation_state: "FILTER_CONVERGED" })
        expect(trusted.mirrorX).toBe(true)
        expect(trusted.labels.find(label => label.text === "E")?.angleDeg).toBe(180)
        const uncertain = mapOrientation({ ...data, attitude_estimation_state: "FILTER_UNCONVERGED" })
        expect(uncertain.compass).toBe(false)
        expect(uncertain.labels.map(label => label.text)).toEqual(["Fwd", "Aft", "Stbd", "Port"])
    })
})
