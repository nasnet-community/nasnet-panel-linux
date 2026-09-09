import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, render, screen, within } from "@testing-library/react"
import type { BalancingRule, Outbound, RoutingRule } from "@/lib/types"
import { ManagedOutboundList } from "../managed-outbound-list"
import { OutboundDetailPanel } from "../outbound-detail-panel"
import { buildOutboundUsage, canTestOutbound, defaultOutboundTag, partitionOutbounds } from "../outbound-details"

const routerRows = [
    { id: 0, node_id: 1, managed: true, tag: "direct-foreign-wan1", protocol: "freedom", sockopt_settings: { mark: 100 } },
    { id: 0, node_id: 1, managed: true, tag: "direct-domestic-wan2", protocol: "freedom", sockopt_settings: { mark: 200 } },
] as Outbound[]
const stored = { id: 7, node_id: 1, tag: "proxy-out", protocol: "socks", address: "127.0.0.1", port: 1080 } as Outbound
const rules = [
    { id: 1, rule_tag: "foreign-rule", outbound_tag: routerRows[0].tag },
    { id: 2, rule_tag: "domestic-rule", outbound_tag: routerRows[1].tag },
] as RoutingRule[]
const balancers = [{ id: 1, outbound_selectors: ["direct-foreign"], fallback_tag: "proxy-out" }] as BalancingRule[]
afterEach(cleanup)

describe("router outbounds in the redesigned list", () => {
    it("keeps every generated zero-ID row out of selection and test queues", () => {
        const all = [...routerRows, stored]
        expect(partitionOutbounds(all)).toEqual({ managedOutbounds: routerRows, outbounds: [stored] })
        expect(all.filter(canTestOutbound)).toEqual([stored])
        expect(canTestOutbound({ ...stored, id: 0 })).toBe(false)
        expect(canTestOutbound({ ...stored, protocol: "http" })).toBe(false)
    })

    it("uses tag identity for distinct rule usage and the real router fallback", () => {
        const all = [...routerRows, stored]
        const usage = buildOutboundUsage(all, rules, balancers)
        expect(usage.get(routerRows[0].tag)).toEqual({ rules: [rules[0]], balancers })
        expect(usage.get(routerRows[1].tag)).toEqual({ rules: [rules[1]], balancers: [] })
        expect(usage.get(stored.tag)).toEqual({ rules: [], balancers })
        expect(defaultOutboundTag(all)).toBe(routerRows[0].tag)
        expect(defaultOutboundTag([{ ...stored, is_disabled: true }, { ...stored, id: 8, tag: "next" }])).toBe("next")
    })

    it("renders both router routes, one default, and no mutation or selection controls", () => {
        const usage = buildOutboundUsage(routerRows, rules, balancers)
        render(<ManagedOutboundList outbounds={routerRows} usage={usage} defaultTag={routerRows[0].tag} onOpenRules={vi.fn()} />)
        const region = screen.getByRole("region", { name: "Managed router outbounds" })
        expect(within(region).getByText(routerRows[0].tag)).toBeInTheDocument()
        expect(within(region).getByText(routerRows[1].tag)).toBeInTheDocument()
        expect(within(region).getAllByText("Default", { exact: true })).toHaveLength(1)
        expect(within(region).getByText("fwmark 100", { selector: "summary span" })).toBeInTheDocument()
        expect(within(region).getByText("fwmark 200", { selector: "summary span" })).toBeInTheDocument()
        expect(within(region).queryByRole("checkbox")).not.toBeInTheDocument()
        expect(within(region).queryByRole("button", { name: /edit|delete|test|enable|disable/i })).not.toBeInTheDocument()
    })
})


describe("outbound detail timestamps", () => {
    it.each([undefined, "0001-01-01T00:00:00Z"])("omits timestamps for generated rows, including %s", timestamp => {
        const outbound = { ...routerRows[0], created_at: timestamp, updated_at: timestamp } as Outbound
        render(<OutboundDetailPanel outbound={outbound} route="fwmark 100" details={[]} usage={{ rules: [], balancers: [] }} isDefault onOpenRules={vi.fn()} />)
        expect(screen.queryByText("Created", { exact: true })).not.toBeInTheDocument()
        expect(screen.queryByText("Updated", { exact: true })).not.toBeInTheDocument()
        expect(screen.queryByText(/Invalid Date/)).not.toBeInTheDocument()
    })

    it("retains the saved timestamps for persisted outbounds", () => {
        const outbound = { ...stored, created_at: "2026-09-05T12:00:00Z", updated_at: "2026-09-09T12:00:00Z" }
        render(<OutboundDetailPanel outbound={outbound} route="127.0.0.1:1080" details={[]} usage={{ rules: [], balancers: [] }} isDefault={false} onOpenRules={vi.fn()} />)
        expect(screen.getByText("Created", { exact: true }).nextElementSibling).toHaveTextContent("Sep 5, 2026")
        expect(screen.getByText("Updated", { exact: true }).nextElementSibling).toHaveTextContent("Sep 9, 2026")
    })
})
