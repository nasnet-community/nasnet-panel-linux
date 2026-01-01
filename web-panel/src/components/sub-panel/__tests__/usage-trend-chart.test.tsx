import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { UsageTrendChart } from "../usage-trend-chart"
import type { UsageTrendResponse } from "@/lib/types/sub-panel"

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal("fetch", fetchMock)
})

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

function jsonResponse(body: UsageTrendResponse) {
  return {
    ok: true,
    status: 200,
    json: async () => ({ success: true, data: body }),
  }
}

describe("UsageTrendChart", () => {
  it("shows empty-state copy when response has no points", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ range: "7d", points: [], unit_hint: "KB" }))

    wrap(<UsageTrendChart uuid="aaaaaaaa" />)

    expect(await screen.findByText(/No traffic recorded yet/i)).toBeInTheDocument()
  })

  it("shows trend-unavailable copy on fetch error", async () => {
    fetchMock.mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({}) })

    wrap(<UsageTrendChart uuid="aaaaaaaa" />)

    expect(await screen.findByText(/Trend unavailable/i)).toBeInTheDocument()
  })

  it("shows 'split unavailable' legend when any point is legacy", async () => {
    const body: UsageTrendResponse = {
      range: "7d",
      unit_hint: "MB",
      points: [
        { date: isoDaysAgo(0), upload: null, download: null, total: 5_000_000 },
      ],
    }
    fetchMock.mockResolvedValueOnce(jsonResponse(body))

    wrap(<UsageTrendChart uuid="aaaaaaaa" />)

    expect(await screen.findByText(/split unavailable/i)).toBeInTheDocument()
  })

  it("does not show 'split unavailable' legend when all points carry split", async () => {
    const body: UsageTrendResponse = {
      range: "7d",
      unit_hint: "MB",
      points: [
        { date: isoDaysAgo(0), upload: 1_000_000, download: 4_000_000, total: 5_000_000 },
      ],
    }
    fetchMock.mockResolvedValueOnce(jsonResponse(body))

    wrap(<UsageTrendChart uuid="aaaaaaaa" />)

    expect(await screen.findByText(/Upload/i)).toBeInTheDocument()
    expect(screen.queryByText(/split unavailable/i)).not.toBeInTheDocument()
  })

  it("clicking 30d triggers a second fetch with range=30d", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ range: "7d", points: [], unit_hint: "KB" }))
    fetchMock.mockResolvedValueOnce(jsonResponse({ range: "30d", points: [], unit_hint: "KB" }))

    wrap(<UsageTrendChart uuid="aaaaaaaa" />)

    await screen.findByText(/No traffic recorded yet/i)

    fireEvent.click(screen.getByRole("tab", { name: "30d" }))

    // Second fetch url must include range=30d
    await vi.waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2)
    })
    const secondCallArgs = fetchMock.mock.calls[1]
    expect(secondCallArgs?.[0]).toMatch(/range=30d/)
  })
})

function isoDaysAgo(n: number): string {
  const d = new Date()
  d.setUTCDate(d.getUTCDate() - n)
  return d.toISOString().slice(0, 10)
}
