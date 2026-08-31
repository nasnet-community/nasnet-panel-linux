import { describe, expect, it } from "vitest"

import { beaconableChannels, describeRadioTradeoff, signalBars } from "@/lib/api/network"
import type { RadioView } from "@/lib/types/network"

const radio: RadioView = {
    phy: "phy0",
    if_name: "wlp3s0",
    key: "k2",
    interface_id: 2,
    role: "unassigned",
    mode: "",
    supports_ap: true,
    supports_sta: true,
    bands: {
        "2g": [{ number: 6, freq_mhz: 2437, no_ir: false, radar: false, disabled_by_regdomain: false }],
        "5g": [
            { number: 36, freq_mhz: 5180, no_ir: false, radar: false, disabled_by_regdomain: false },
            { number: 52, freq_mhz: 5260, no_ir: false, radar: true, disabled_by_regdomain: false },
            { number: 100, freq_mhz: 5500, no_ir: true, radar: false, disabled_by_regdomain: false },
            { number: 149, freq_mhz: 5745, no_ir: false, radar: false, disabled_by_regdomain: true },
        ],
    },
    country_code: "IR",
    country_code_set: true,
    ax_supported: true,
    sae_supported: true,
    sibling_role: "",
}

describe("signal display", () => {
    it("maps dBm to four bars monotonically", () => {
        expect(signalBars(-40)).toBe(4)
        expect(signalBars(-60)).toBeLessThanOrEqual(3)
        expect(signalBars(-90)).toBeLessThanOrEqual(1)
        expect(signalBars(-40)).toBeGreaterThan(signalBars(-90))
    })

    it("never goes below zero or above four", () => {
        expect(signalBars(-200)).toBe(0)
        expect(signalBars(0)).toBe(4)
    })
})

describe("beaconable channels", () => {
    it("hides radar, no-IR and disabled channels from the picker", () => {
        expect(beaconableChannels(radio, "5g").map((c) => c.number)).toEqual([36])
    })

    it("returns nothing when the country code is unset", () => {
        expect(beaconableChannels({ ...radio, country_code_set: false }, "5g")).toHaveLength(0)
    })

    it("returns nothing for a radio that cannot be an AP", () => {
        expect(beaconableChannels({ ...radio, supports_ap: false }, "2g")).toHaveLength(0)
    })

    it("returns nothing for a band the radio does not have", () => {
        expect(beaconableChannels(radio, "6g")).toHaveLength(0)
    })
})

describe("one radio, one role", () => {
    // The consequence has to be stated plainly, not implied by a greyed control
    it("explains the trade-off on a single-radio box", () => {
        const text = describeRadioTradeoff({ ...radio, sibling_role: "" }, 1, 1)
        expect(text).toMatch(/either/i)
        expect(text).toMatch(/adapter/i)
    })

    it("names the sibling role when one is held", () => {
        const text = describeRadioTradeoff({ ...radio, sibling_role: "wan" }, 2, 1)
        expect(text).toContain("phy0")
        expect(text).toMatch(/never both/i)
    })

    it("says nothing when there is no conflict", () => {
        expect(describeRadioTradeoff(radio, 3, 2)).toBe("")
    })
})
