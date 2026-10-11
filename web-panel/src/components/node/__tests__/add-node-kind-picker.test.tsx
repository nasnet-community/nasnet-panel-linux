import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { AddNodeKindPicker } from "../add-node-kind-picker"

describe("AddNodeKindPicker", () => {
    it("offers both kinds and reports the one picked", async () => {
        const user = userEvent.setup()
        const onPick = vi.fn()
        render(<AddNodeKindPicker onPick={onPick} />)
        await user.click(screen.getByRole("button", { name: /Full node/ }))
        await user.click(screen.getByRole("button", { name: /MTProto node/ }))
        expect(onPick.mock.calls).toEqual([["full"], ["mtproto"]])
    })

    it("says node, never relay or server", () => {
        const { container } = render(<AddNodeKindPicker onPick={() => {}} />)
        expect(container.textContent).not.toMatch(/relay|server/i)
    })
})
