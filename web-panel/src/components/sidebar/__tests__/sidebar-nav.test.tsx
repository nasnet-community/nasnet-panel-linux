import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"
import { SidebarNav, type NavSection } from "../sidebar-nav"

const Icon = () => <svg aria-hidden="true" />
const sections: NavSection[] = [{
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
}]

describe("SidebarNav submenus", () => {
    it("removes collapsed destinations from keyboard navigation and restores every child on expansion", async () => {
        const user = userEvent.setup()
        render(
            <MemoryRouter initialEntries={["/dashboard"]}>
                <SidebarNav sections={sections} collapsed={false} getBadge={() => 0} />
            </MemoryRouter>,
        )

        const toggle = screen.getByRole("button", { name: "Collapse Nodes" })
        const lastChild = screen.getByRole("link", { name: "Xray Core" })
        expect(toggle).toHaveAttribute("aria-expanded", "true")
        expect(document.getElementById(toggle.getAttribute("aria-controls")!)).toContainElement(lastChild)

        await user.click(toggle)
        expect(toggle).toHaveAccessibleName("Expand Nodes")
        expect(toggle).toHaveAttribute("aria-expanded", "false")
        expect(screen.queryByRole("link", { name: "Xray Core" })).not.toBeInTheDocument()
        await user.tab()
        expect(lastChild).not.toHaveFocus()
        expect(document.body).toHaveFocus()

        await user.click(toggle)
        expect(screen.getAllByRole("link")).toHaveLength(7)
        for (let i = 0; i < 6; i++) await user.tab()
        expect(lastChild).toHaveFocus()
    })
})
