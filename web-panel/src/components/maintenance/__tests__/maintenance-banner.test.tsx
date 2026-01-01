import { render, screen } from "@testing-library/react"
import "@testing-library/jest-dom/vitest"
import { describe, it, expect } from "vitest"
import { MaintenanceBanner } from "../maintenance-banner"

describe("MaintenanceBanner", () => {
  it("renders nothing when status is null", () => {
    const { container } = render(<MaintenanceBanner status={null} />)
    expect(container.querySelector('[role="alert"]')).toBeNull()
  })

  it("renders nothing when status.active is false", () => {
    render(<MaintenanceBanner status={{ active: false, scope: "", message: "" }} />)
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("renders message when active", () => {
    render(
      <MaintenanceBanner
        status={{ active: true, scope: "global", message: "Brief upgrade" }}
      />
    )
    expect(screen.getByRole("alert")).toBeInTheDocument()
    expect(screen.getByText(/Brief upgrade/)).toBeInTheDocument()
    expect(screen.getByText(/Maintenance in progress/)).toBeInTheDocument()
  })

  it("falls back to default text when message is empty", () => {
    render(
      <MaintenanceBanner
        status={{ active: true, scope: "global", message: "" }}
      />
    )
    expect(screen.getByText(/Service maintenance is currently underway/)).toBeInTheDocument()
  })

  it("shows formatted since timestamp when provided", () => {
    render(
      <MaintenanceBanner
        status={{
          active: true,
          scope: "global",
          message: "Hi",
          since: "2026-04-19T10:00:00Z",
        }}
      />
    )
    expect(screen.getByText(/Since /)).toBeInTheDocument()
  })
})
