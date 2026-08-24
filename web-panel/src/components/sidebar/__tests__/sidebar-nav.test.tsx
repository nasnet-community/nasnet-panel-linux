import { render } from "@testing-library/react"
import "@testing-library/jest-dom/vitest"
import { MemoryRouter } from "react-router"
import { describe, it, expect } from "vitest"
import { SidebarNav, type NavSection } from "../sidebar-nav"

function Icon({ className }: { className?: string }) {
    return <span className={className} />
}

const sections: NavSection[] = [
    {
        label: "Infrastructure",
        items: [
            {
                label: "Router",
                href: "/router",
                icon: Icon,
                children: [{ label: "Traffic flow", href: "/router/flow", icon: Icon }],
            },
            { label: "Server", href: "/server", icon: Icon },
        ],
    },
]

function renderAt(path: string) {
    return render(
        <MemoryRouter initialEntries={[path]}>
            <SidebarNav sections={sections} collapsed={false} getBadge={() => 0} />
        </MemoryRouter>,
    )
}

/** The current page is one row. Both the link tint and the accent bar mark it,
 *  so count the bars — that is what the eye actually reads. */
function activeLabels(): string[] {
    return Array.from(document.querySelectorAll("a"))
        .filter((a) => a.className.includes("bg-primary/10"))
        .map((a) => a.textContent?.replace("└", "").trim() ?? "")
}

describe("SidebarNav active state", () => {
    // A parent whose child is open used to light up too, so two rows claimed to
    // be the current page at once.
    it("marks only the child when a child route is open", () => {
        renderAt("/router/flow")
        expect(activeLabels()).toEqual(["Traffic flow"])
    })

    it("marks the parent when the parent route itself is open", () => {
        renderAt("/router")
        expect(activeLabels()).toEqual(["Router"])
    })

    // A sub-route with no nav entry of its own still belongs to its parent.
    it("falls back to the parent for a path no child claims", () => {
        renderAt("/router/ports")
        expect(activeLabels()).toEqual(["Router"])
    })

    it("marks a childless item on its own route", () => {
        renderAt("/server")
        expect(activeLabels()).toEqual(["Server"])
    })
})
