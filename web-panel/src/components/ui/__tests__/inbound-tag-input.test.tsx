import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import "@testing-library/jest-dom/vitest"
import { describe, it, expect, vi } from "vitest"
import { InboundTagInput } from "../tag-input"

const suggestions = [
    { tag: "vless-t", label: "vless-t (vless:8443)" },
    { tag: "socks-in", label: "socks-in (socks:54981)" },
]

describe("InboundTagInput", () => {
    it("renders a suggestion chip per available suggestion", () => {
        render(
            <InboundTagInput suggestions={suggestions} selected={[]} onAdd={vi.fn()} onRemove={vi.fn()} />
        )
        expect(screen.getByRole("button", { name: /vless-t/ })).toBeInTheDocument()
        expect(screen.getByRole("button", { name: /socks-in/ })).toBeInTheDocument()
    })

    it("adds a known tag when its chip is clicked", async () => {
        const onAdd = vi.fn()
        render(
            <InboundTagInput suggestions={suggestions} selected={[]} onAdd={onAdd} onRemove={vi.fn()} />
        )
        await userEvent.click(screen.getByRole("button", { name: /socks-in/ }))
        expect(onAdd).toHaveBeenCalledWith("socks-in")
    })

    it("adds a custom, synthetic tag typed by the user (e.g. dns-in)", async () => {
        const onAdd = vi.fn()
        render(
            <InboundTagInput suggestions={suggestions} selected={[]} onAdd={onAdd} onRemove={vi.fn()} />
        )
        const input = screen.getByRole("textbox")
        await userEvent.type(input, "dns-in{Enter}")
        expect(onAdd).toHaveBeenCalledWith("dns-in")
    })

    it("hides suggestions already selected", () => {
        render(
            <InboundTagInput suggestions={suggestions} selected={["socks-in"]} onAdd={vi.fn()} onRemove={vi.fn()} />
        )
        expect(screen.queryByRole("button", { name: /socks-in \(/ })).not.toBeInTheDocument()
        expect(screen.getByRole("button", { name: /vless-t/ })).toBeInTheDocument()
    })

    it("renders each selected tag", () => {
        render(
            <InboundTagInput suggestions={suggestions} selected={["dns-in"]} onAdd={vi.fn()} onRemove={vi.fn()} />
        )
        // Selected value shows in the TagList; the matching suggestion chip is hidden.
        expect(screen.getByText("dns-in")).toBeInTheDocument()
    })
})
