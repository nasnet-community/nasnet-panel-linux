import { useState } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { NumberSetting } from "../protocol-forms/settings-fields"

function Harness({ start, zeroLabel }: { start: number; zeroLabel?: string }) {
    const [v, setV] = useState(start)
    return (
        <>
            <NumberSetting id="n" label="Metrics port" value={v} onChange={setV} zeroLabel={zeroLabel} />
            <output>{v}</output>
        </>
    )
}

describe("NumberSetting", () => {
    it("shows 0 as an empty Auto box and saves an emptied one as 0", () => {
        render(<Harness start={14301} zeroLabel="Auto" />)
        const box = screen.getByRole("spinbutton") as HTMLInputElement
        expect(box.value).toBe("14301")

        fireEvent.change(box, { target: { value: "" } })
        expect(box.value).toBe("")
        expect(box.placeholder).toBe("Auto")
        expect(screen.getByRole("status").textContent).toBe("0")
    })

    it("keeps showing 0 without a zeroLabel", () => {
        render(<Harness start={5} />)
        const box = screen.getByRole("spinbutton") as HTMLInputElement
        fireEvent.change(box, { target: { value: "" } })
        expect(box.value).toBe("0")
    })
})
