import { describe, expect, it } from "vitest"
import { slotHolder } from "./interface-table"
import type { NetworkInterfaceView } from "@/lib/types/network"

function iface(over: Partial<NetworkInterfaceView>): NetworkInterfaceView {
    return {
        id: 1,
        if_name: "eth0",
        perm_mac: "",
        id_path: "",
        key: "k1",
        key_kind: "permaddr",
        source: "eth_pci",
        confidence: 100,
        driver: "",
        carrier: true,
        oper_state: "up",
        speed_mbit: 1000,
        mtu: 1500,
        usb_speed_mbit: 0,
        assignable: true,
        addrs: [],
        role: "unassigned",
        slot: "",
        label: "",
        present: true,
        healthy: true,
        ...over,
    }
}

describe("slotHolder", () => {
    const rows = [
        iface({ id: 1, key: "k1", if_name: "eth0", role: "wan", slot: "domestic" }),
        iface({ id: 2, key: "k2", if_name: "eth1" }),
    ]

    it("names the interface already holding the slot", () => {
        expect(slotHolder(rows, "domestic", "k2")).toBe("eth0")
    })

    it("prefers the operator label over the kernel name", () => {
        const labelled = [iface({ key: "k1", role: "wan", slot: "domestic", label: "Fibre" })]
        expect(slotHolder(labelled, "domestic", "k2")).toBe("Fibre")
    })

    it("leaves a free slot open", () => {
        expect(slotHolder(rows, "secondary", "k2")).toBeNull()
    })

    // Otherwise the row holding a slot cannot keep it.
    it("does not count the interface asking", () => {
        expect(slotHolder(rows, "domestic", "k1")).toBeNull()
    })
})
