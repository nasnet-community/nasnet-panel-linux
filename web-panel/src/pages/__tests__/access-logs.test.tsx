import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import AccessLogsPage from "../access-logs"
import type { AggregatedAccessLogEntry } from "@/lib/types"

const entries: AggregatedAccessLogEntry[] = Array.from({ length: 105 }, (_, i) => ({
  timestamp: 1_780_000_000 + i,
  node_id: 1,
  node_name: "London",
  node_country: "GB",
  source_ip: "192.0.2.1",
  status: "accepted",
  network: "tcp",
  domain: `host-${i}.example.com`,
  port: 443,
  inbound_tag: "in",
  outbound_tag: "out",
  email: "subscriber@example.com",
}))

vi.mock("@/lib/queries", () => ({
  useNodes: () => ({ data: [] }),
  useAggregatedAccessLogs: () => ({ data: entries, isLoading: false, isFetching: false }),
}))

afterEach(cleanup)

it("sorts and paginates access logs with the registered v9 features", () => {
  render(<AccessLogsPage />)
  const rows = within(screen.getByRole("table")).getAllByRole("row", { hidden: true })
  expect(rows).toHaveLength(101)
  expect(rows[1]).toHaveTextContent("host-104.example.com")
  expect(screen.queryByText("host-0.example.com")).not.toBeInTheDocument()

  fireEvent.click(screen.getByRole("button", { name: "Next page" }))
  expect(within(screen.getByRole("table")).getAllByRole("row", { hidden: true })).toHaveLength(6)
  expect(screen.getByText("host-0.example.com")).toBeInTheDocument()

  fireEvent.click(screen.getByRole("button", { name: "Previous page" }))
  fireEvent.click(screen.getByRole("button", { name: "Time" }))
  expect(within(screen.getByRole("table")).getAllByRole("row", { hidden: true })[1]).toHaveTextContent("host-0.example.com")
// This exercises several renders of a full 100-row page in jsdom.
}, 15_000)

it("filters loaded access logs by domain", () => {
  render(<AccessLogsPage />)
  fireEvent.change(screen.getByPlaceholderText("Search domain, IP, port..."), {
    target: { value: "host-42.example.com" },
  })
  expect(within(screen.getByRole("table")).getAllByRole("row", { hidden: true })).toHaveLength(2)
  expect(screen.getByText("host-42.example.com")).toBeInTheDocument()
  expect(screen.queryByText("host-104.example.com")).not.toBeInTheDocument()
})
