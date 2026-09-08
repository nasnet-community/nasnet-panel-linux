import { describe, expect, it } from "vitest"
import { uplinkLabel } from "@/pages/router/lan-tab"
import type { NetworkState, UplinkView } from "@/lib/types/network"

function up(over: Partial<UplinkView>): UplinkView {
    return {
        if_name: "eth0",
        slot: "domestic",
        label: "",
        table: 201,
        addrs: [],
        gateway: "",
        healthy: true,
        verdict: "up",
        force_state: "",
        ...over,
    }
}

function state(uplinks: UplinkView[]): NetworkState {
    return {
        router_mode: true,
        takeover_done: true,
        // [] on its own infers never[], and tsc then refuses the cast below.
        warnings: [] as string[],
        uplinks,
        pending_plan_id: 0,
        confirm_deadline_unix: 0,
    } as NetworkState
}

describe("uplinkLabel", () => {
    it("names the one domestic line", () => {
        expect(uplinkLabel(state([up({ label: "ADSL" })]), "domestic")).toBe("ADSL")
    })

    it("names the backup when there are two", () => {
        const s = state([
            up({ label: "ADSL" }),
            up({ if_name: "eth2", slot: "domestic2", label: "Fibre" }),
        ])
        expect(uplinkLabel(s, "domestic")).toBe("ADSL, or Fibre on failover")
    })

    // The API lists uplinks by interface name, so the primary has to come from
    // the slot order instead.
    it("names the primary by slot, not by interface name", () => {
        const s = state([
            up({ if_name: "eth1", slot: "domestic2" }),
            up({ if_name: "eth3", slot: "domestic" }),
        ])
        expect(uplinkLabel(s, "domestic")).toBe("eth3, or eth1 on failover")
    })

    it("orders the secondaries by slot too", () => {
        const s = state([
            up({ if_name: "eth1", slot: "secondary2" }),
            up({ if_name: "eth3", slot: "secondary", label: "Dish" }),
        ])
        expect(uplinkLabel(s, "secondary")).toBe("Dish and 1 more")
    })

    it("counts the backups past two", () => {
        const s = state([
            up({ label: "ADSL" }),
            up({ if_name: "eth2", slot: "domestic2", label: "Fibre" }),
            up({ if_name: "eth3", slot: "domestic3" }),
        ])
        expect(uplinkLabel(s, "domestic")).toBe("ADSL, or 2 backups on failover")
    })

    it("still counts the secondaries the old way", () => {
        const s = state([
            up({ if_name: "eth1", slot: "secondary", label: "Dish" }),
            up({ if_name: "eth4", slot: "secondary2" }),
        ])
        expect(uplinkLabel(s, "secondary")).toBe("Dish and 1 more")
    })

    it("falls back to a generic name with nothing assigned", () => {
        expect(uplinkLabel(state([]), "domestic")).toBe("the domestic uplink")
    })
})
