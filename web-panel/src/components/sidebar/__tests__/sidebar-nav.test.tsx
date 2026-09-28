import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it } from "vitest"
import { SidebarNav, type NavSection } from "../sidebar-nav"

const Icon = () => <svg aria-hidden="true" />
const sections = [{
    label: "Infrastructure",
    items: [{
        label: "Nodes",
        href: "/nodes",
        icon: Icon,
        children: ["Hosts", "Groups", "Tunnels", "Access Logs", "Access History", "Xray Core"].map(label => ({
            label,
            href: `/${label.toLowerCase().replaceAll(" ", "-")}`,
            icon: Icon,
        })),
    }],
}] as unknown as NavSection[]

beforeEach(() => localStorage.clear())

describe("SidebarNav submenus", () => {
    it("keeps other groups closed and out of keyboard navigation until expanded", async () => {
        const user = userEvent.setup()
        render(
            <MemoryRouter initialEntries={["/dashboard"]}>
                <SidebarNav sections={sections} collapsed={false} badges={{}} />
            </MemoryRouter>,
        )

        const toggle = screen.getByRole("button", { name: "Expand Nodes" })
        expect(toggle).toHaveAttribute("aria-expanded", "false")
        expect(screen.queryByRole("link", { name: "Xray Core" })).not.toBeInTheDocument()

        await user.click(toggle)
        expect(toggle).toHaveAccessibleName("Collapse Nodes")
        const lastChild = screen.getByRole("link", { name: "Xray Core" })
        expect(document.getElementById(toggle.getAttribute("aria-controls")!)).toContainElement(lastChild)
        expect(screen.getAllByRole("link")).toHaveLength(7)

        await user.click(toggle)
        expect(screen.queryByRole("link", { name: "Xray Core" })).not.toBeInTheDocument()
        await user.tab()
        expect(document.body).toHaveFocus()
    })

    it("always shows the group that holds the current page", () => {
        localStorage.setItem("sidebar-expanded-groups", JSON.stringify({ "/nodes": false }))
        render(
            <MemoryRouter initialEntries={["/tunnels"]}>
                <SidebarNav sections={sections} collapsed={false} badges={{}} />
            </MemoryRouter>,
        )
        expect(screen.getByRole("link", { name: "Tunnels" })).toHaveAttribute("aria-current", "page")
        expect(screen.queryByRole("button", { name: /Nodes/ })).not.toBeInTheDocument()
    })
})
