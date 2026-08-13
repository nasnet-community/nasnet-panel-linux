import { describe, expect, it } from "vitest"
import { deviceName, lastSeenLabel } from "@/lib/network-labels"
import type { LANDevice } from "@/lib/types/network"

function device(over: Partial<LANDevice> = {}): LANDevice {
    return {
        mac: "b8:27:eb:aa:bb:01",
        ips: ["10.77.0.141"],
        hostname: "",
        vendor: "",
        label: "",
        randomized: false,
        online: true,
        ...over,
    }
}

describe("deviceName", () => {
    // The operator's own name outranks anything the device says about itself.
    it("prefers the operator's label", () => {
        const d = device({ label: "the NAS", hostname: "raspberrypi", vendor: "Raspberry Pi" })
        expect(deviceName(d)).toEqual({ name: "the NAS", from: "named" })
    })

    it("falls back to the hostname the client claimed", () => {
        const d = device({ hostname: "my-nas", vendor: "Raspberry Pi Foundation" })
        expect(deviceName(d)).toEqual({ name: "my-nas", from: "claimed" })
    })

    it("falls back to the registered vendor", () => {
        const d = device({ vendor: "Raspberry Pi Foundation" })
        expect(deviceName(d)).toEqual({ name: "Raspberry Pi Foundation", from: "vendor" })
    })

    // A randomized MAC has no vendor, so this is where those land.
    it("falls back to the MAC when nothing is known", () => {
        const d = device({ mac: "b2:27:eb:aa:bb:02", randomized: true })
        expect(deviceName(d)).toEqual({ name: "b2:27:eb:aa:bb:02", from: "unknown" })
    })

    // A client sending a blank hostname must not produce a blank row.
    it("treats an empty hostname as absent", () => {
        expect(deviceName(device({ hostname: "", vendor: "Cisco" })).from).toBe("vendor")
    })
})

describe("lastSeenLabel", () => {
    it("distinguishes never-seen from just-seen", () => {
        expect(lastSeenLabel(undefined)).toBe("Not seen on the bridge")
        expect(lastSeenLabel(0)).toBe("Seconds ago")
        expect(lastSeenLabel(59)).toBe("Seconds ago")
    })

    it("singularizes correctly", () => {
        expect(lastSeenLabel(60)).toBe("1 minute ago")
        expect(lastSeenLabel(120)).toBe("2 minutes ago")
        expect(lastSeenLabel(3600)).toBe("1 hour ago")
        expect(lastSeenLabel(7200)).toBe("2 hours ago")
    })
})
