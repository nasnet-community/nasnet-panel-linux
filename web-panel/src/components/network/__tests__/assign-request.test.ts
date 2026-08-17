import { describe, it, expect } from "vitest"
import { buildAssignRequest, ROLE_CHOICES } from "../interface-table"
import type { NetworkInterfaceView } from "@/lib/types/network"

function iface(over: Partial<NetworkInterfaceView>): NetworkInterfaceView {
    return {
        id: 1,
        key: "permaddr:aa",
        key_kind: "permaddr",
        if_name: "enp1s0",
        label: "",
        role: "unassigned",
        slot: "",
        present: true,
        link: "up",
        confidence: 100,
        source: "eth_pci",
        addresses: [],
        ...over,
    } as NetworkInterfaceView
}

const choice = (value: string) => ROLE_CHOICES.find((c) => c.value === value)!

describe("buildAssignRequest", () => {
    it("names the evictee when a singleton role is already held", () => {
        const holder = iface({ id: 7, if_name: "enp2s0", role: "lan" })
        const target = iface({ id: 9, if_name: "enp3s0" })

        const req = buildAssignRequest([holder, target], target, choice("lan"))

        expect(req).toMatchObject({ interface_id: 9, role: "lan", evict_id: 7 })
    })

    it("points a LAN member at the row holding the lan role", () => {
        const bridge = iface({ id: 3, if_name: "enp2s0", role: "lan" })
        const target = iface({ id: 4, if_name: "enp4s0" })

        const req = buildAssignRequest([bridge, target], target, choice("lan_member"))

        expect(req.master_id).toBe(3)
        // The bridge holds "lan", not "lan_member", so nobody is evicted.
        expect(req.evict_id).toBeUndefined()
    })

    it("leaves both ids off when nothing holds the role", () => {
        const target = iface({ id: 5 })

        const req = buildAssignRequest([target], target, choice("mgmt"))

        expect(req).toEqual({ interface_id: 5, role: "mgmt", slot: "" })
    })

    it("evicts per slot, so the two WAN slots are independent", () => {
        const domestic = iface({ id: 1, role: "wan", slot: "domestic" })
        const secondary = iface({ id: 2, if_name: "enp2s0", role: "wan", slot: "secondary" })
        const target = iface({ id: 3, if_name: "enp3s0" })

        const req = buildAssignRequest(
            [domestic, secondary, target],
            target,
            choice("wan:secondary"),
        )

        expect(req.evict_id).toBe(2)
    })
})
