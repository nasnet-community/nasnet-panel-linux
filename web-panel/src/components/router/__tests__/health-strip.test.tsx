import { describe, expect, it, vi, beforeEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { HealthStrip } from "@/components/router/health-strip"
import type { RouterHealth, UplinkHealth } from "@/lib/types/health"

const mutate = vi.fn()

vi.mock("@/lib/queries/use-router-health", () => ({
    useRouterHealth: () => mockQuery,
    useSetUplinkForce: () => ({ mutate, isPending: false }),
}))

let mockQuery: { data?: RouterHealth; isLoading: boolean; isError: boolean }

function uplink(over: Partial<UplinkHealth>): UplinkHealth {
    return {
        slot: "domestic",
        if_name: "eth0",
        carrier: "up",
        gateway: "up",
        internet: "up",
        verdict: "up",
        force_state: "",
        degraded: false,
        loss_pct: 0,
        median_rtt_ms: 40,
        targets: [{ address: "1.1.1.1:443", proto: "tcp", ok: true, rtt_ms: 40 }],
        history: [
            { unix: 1, ok_ratio: 1, rtt_ms: 40 },
            { unix: 2, ok_ratio: 1, rtt_ms: 42 },
        ],
        ...over,
    }
}

function health(over: Partial<RouterHealth>): RouterHealth {
    return {
        generated_unix: 100,
        failover_active: false,
        uplinks: [uplink({}), uplink({ slot: "secondary", if_name: "eth1" })],
        vpn: null,
        ...over,
    }
}

beforeEach(() => {
    mutate.mockClear()
    mockQuery = { data: health({}), isLoading: false, isError: false }
})

describe("HealthStrip", () => {
    it("renders one card per uplink with its verdict", () => {
        render(<HealthStrip />)
        expect(screen.getByText("eth0")).toBeInTheDocument()
        expect(screen.getByText("eth1")).toBeInTheDocument()
        expect(document.querySelectorAll('[data-verdict="up"]').length).toBe(2)
    })

    it("shows the loss figure on a degraded uplink", () => {
        mockQuery.data = health({
            uplinks: [uplink({ verdict: "degraded", degraded: true, loss_pct: 25 })],
        })
        render(<HealthStrip />)
        expect(screen.getByText(/25% loss/)).toBeInTheDocument()
        expect(document.querySelector('[data-verdict="degraded"]')).not.toBeNull()
    })

    it("marks the internet segment down while the gateway stays ok", () => {
        mockQuery.data = health({
            uplinks: [uplink({ verdict: "no-internet", internet: "down" })],
        })
        render(<HealthStrip />)
        const segs = document.querySelectorAll("[data-layer]")
        const byLayer: Record<string, string> = {}
        segs.forEach((s) => {
            byLayer[s.getAttribute("data-layer")!] = s.getAttribute("data-layer-status")!
        })
        expect(byLayer["gateway"]).toBe("up")
        expect(byLayer["internet"]).toBe("down")
    })

    it("announces an active failover", () => {
        mockQuery.data = health({ failover_active: true })
        render(<HealthStrip />)
        expect(screen.getByText(/riding the VPN/i)).toBeInTheDocument()
    })

    it("fires the force mutation with the chosen state", () => {
        mockQuery.data = health({ uplinks: [uplink({})] })
        render(<HealthStrip />)
        fireEvent.click(screen.getByRole("button", { name: /force down/i }))
        expect(mutate).toHaveBeenCalledWith({ ifName: "eth0", state: "down" })
    })

    it("stays quiet on loading and error", () => {
        mockQuery = { isLoading: true, isError: false }
        const { unmount } = render(<HealthStrip />)
        expect(screen.queryByText("eth0")).toBeNull()
        unmount()
        mockQuery = { isLoading: false, isError: true }
        render(<HealthStrip />)
        expect(screen.getByText(/health is unavailable/i)).toBeInTheDocument()
    })
})
