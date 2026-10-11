import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { AlertRule } from "@/lib/api/alerts"
import AlertsPage from "../alerts"

const state = vi.hoisted(() => ({ rules: [] as unknown[], threshold: vi.fn() }))

vi.mock("@/lib/queries", () => ({
    useAlertRules: () => ({ data: state.rules, isLoading: false }),
    useAlertEvents: () => ({ data: [], isLoading: false }),
    useSetAlertRuleEnabled: () => ({ mutate: vi.fn() }),
    useSetAlertRuleThreshold: () => ({ mutate: state.threshold, isPending: false }),
    useTestAlertRule: () => ({ mutate: vi.fn(), isPending: false }),
}))

function rule(over: Partial<AlertRule>): AlertRule {
    return {
        id: 1, name: "rule", rule_type: "node_offline", scope: "global", threshold: {}, cooldown_sec: 900,
        enabled: false, description: "", created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z",
        ...over,
    }
}

function row(name: string) {
    return screen.getByRole("heading", { name }).closest("div.border") as HTMLElement
}

beforeEach(() => {
    vi.clearAllMocks()
    state.rules = [
        rule({ id: 1, name: "MTProto refused destinations", rule_type: "relay_allowlist_violation", threshold: { count: 20, window_sec: 300 } }),
        rule({ id: 2, name: "MTProto cannot reach Telegram", rule_type: "relay_egress_down", threshold: { count: 5, window_sec: 120 } }),
        rule({ id: 3, name: "MTProto cover site down", rule_type: "relay_disguise_down", cooldown_sec: 3600 }),
    ]
})

describe("AlertsPage MTProto rules", () => {
    it("labels each MTProto rule and summarises its threshold", () => {
        render(<AlertsPage />)
        expect(within(row("MTProto refused destinations")).getByText("Refused Destinations")).toBeInTheDocument()
        expect(row("MTProto refused destinations")).toHaveTextContent("20 refused in 300s · cooldown 900s")
        expect(within(row("MTProto cannot reach Telegram")).getByText("Telegram Unreachable")).toBeInTheDocument()
        expect(row("MTProto cannot reach Telegram")).toHaveTextContent("5 failed connections in 120s")
        expect(within(row("MTProto cover site down")).getByText("Cover Site Down")).toBeInTheDocument()
        expect(row("MTProto cover site down")).toHaveTextContent("cover site stops working")
    })

    it("offers count and window for the counting rules, nothing for the cover site", async () => {
        const user = userEvent.setup()
        render(<AlertsPage />)
        expect(within(row("MTProto cover site down")).queryByRole("button", { name: /threshold/i })).toBeNull()
        await user.click(within(row("MTProto cannot reach Telegram")).getByRole("button", { name: /threshold/i }))
        const dialog = await screen.findByRole("dialog")
        expect(within(dialog).getByText("Count")).toBeInTheDocument()
        expect(within(dialog).getByText("Window (seconds)")).toBeInTheDocument()
        expect(within(dialog).queryByText("Threshold (%)")).toBeNull()
        await user.click(within(dialog).getByRole("button", { name: "Save" }))
        expect(state.threshold).toHaveBeenCalledWith(
            { id: 2, threshold: { count: 5, window_sec: 120 }, cooldownSec: 900 },
            expect.anything(),
        )
    })
})
