import { describe, expect, it } from "vitest"
import {
    attachmentLabel,
    groupAddresses,
    linkLabel,
    linkTone,
    missingRoleHint,
    roleLabel,
    uncoveredWarnings,
} from "@/lib/network-labels"
import type { NetworkInterfaceView } from "@/lib/types/network"

function iface(over: Partial<NetworkInterfaceView>): NetworkInterfaceView {
    return {
        id: 1,
        if_name: "eth0",
        phy: "",
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

describe("attachmentLabel", () => {
    it("translates the classifier token", () => {
        expect(attachmentLabel("eth_pci")).toBe("PCIe Ethernet")
        expect(attachmentLabel("tether_iphone")).toBe("iPhone tether")
    })

    // A new backend Source should not render as blank.
    it("falls back to the raw token", () => {
        expect(attachmentLabel("eth_thunderbolt")).toBe("eth_thunderbolt")
    })
})

describe("roleLabel", () => {
    it("names the uplink by its slot", () => {
        expect(roleLabel("wan", "domestic")).toBe("Domestic ISP")
        expect(roleLabel("wan", "secondary")).toBe("Secondary 1")
        expect(roleLabel("wan", "secondary4")).toBe("Secondary 4")
    })

    it("uses the role for everything slotless", () => {
        expect(roleLabel("lan_member", "")).toBe("LAN member")
        expect(roleLabel("unassigned", "")).toBe("Unassigned")
    })
})

describe("groupAddresses", () => {
    it("promotes routable IPv4 over IPv6", () => {
        const g = groupAddresses(["fec0::1/64", "fe80::1/64", "10.0.2.15/24"])
        expect(g.primary).toBe("10.0.2.15/24")
        expect(g.extra).toEqual(["fec0::1/64", "fe80::1/64"])
    })

    it("ranks link-local last", () => {
        const g = groupAddresses(["fe80::1/64", "169.254.3.4/16", "2001:db8::5/64"])
        expect(g.primary).toBe("2001:db8::5/64")
        expect(g.extra).toEqual(["169.254.3.4/16", "fe80::1/64"])
    })

    it("reports no address rather than throwing", () => {
        expect(groupAddresses(null)).toEqual({ primary: null, extra: [] })
        expect(groupAddresses([])).toEqual({ primary: null, extra: [] })
    })
})

describe("link state", () => {
    it("separates absent from down", () => {
        expect(linkTone(iface({ present: false, carrier: true }))).toBe("absent")
        expect(linkTone(iface({ carrier: false }))).toBe("down")
        expect(linkTone(iface({ carrier: true }))).toBe("up")
    })

    it("prefers the kernel oper_state when the carrier is off", () => {
        expect(linkLabel(iface({ carrier: false, oper_state: "dormant" }))).toBe("dormant")
        expect(linkLabel(iface({ carrier: false, oper_state: "" }))).toBe("down")
    })
})

// The empty bays' one real job was telling a novice which role still matters.
describe("missingRoleHint", () => {
    it("asks for a LAN port while none is assigned", () => {
        const hint = missingRoleHint([iface({ role: "wan", slot: "domestic" })])
        expect(hint).toMatch(/LAN/)
        expect(hint).toMatch(/switch or access point/i)
    })

    it("goes quiet once a port holds the LAN", () => {
        expect(missingRoleHint([iface({ id: 2, key: "k2", role: "lan" })])).toBeNull()
    })

    // Management is optional, so nagging about it would be noise on a working router.
    it("says nothing about the optional management port", () => {
        expect(missingRoleHint([iface({ role: "lan" })])).toBeNull()
    })
})

describe("uncoveredWarnings", () => {
    it("drops the warnings the page states from state fields", () => {
        expect(
            uncoveredWarnings([
                "network not managed by nasnet yet — assign roles to finish setup",
                "1 uplink(s) assigned — no failover and no split routing",
            ]),
        ).toEqual([])
    })

    it("passes anything else through", () => {
        expect(uncoveredWarnings(["dhcp lease expires in 40s"])).toEqual([
            "dhcp lease expires in 40s",
        ])
    })

    it("tolerates a missing list", () => {
        expect(uncoveredWarnings(undefined)).toEqual([])
    })
})
