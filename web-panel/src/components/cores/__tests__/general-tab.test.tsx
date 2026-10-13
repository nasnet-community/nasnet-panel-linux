import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { describe, expect, it } from "vitest"
import { Form } from "@/components/ui/form"
import { coreFormSchema, type CoreFormData } from "@/lib/validations/core-schema"
import { GeneralTab } from "../tabs/general-tab"

function Harness() {
    const form = useForm<CoreFormData>({
        resolver: zodResolver(coreFormSchema),
        mode: "onBlur",
        defaultValues: { node_id: 4, protocol: "mtproto", listen: "0.0.0.0", port: 8443, egress_mode: "hub", remark: "" },
    })
    return (
        <Form {...form}>
            <form onSubmit={form.handleSubmit(() => {})}>
                <GeneralTab form={form} nodeName="VPS 1" builtIn={false} lite />
                <button type="submit">Save</button>
            </form>
        </Form>
    )
}

describe("GeneralTab port", () => {
    it("stays empty when cleared instead of snapping to 0", async () => {
        render(<Harness />)
        const port = screen.getByRole("spinbutton", { name: "Port" }) as HTMLInputElement
        expect(port.value).toBe("8443")

        fireEvent.change(port, { target: { value: "" } })
        expect(port.value).toBe("")

        fireEvent.click(screen.getByRole("button", { name: "Save" }))
        await waitFor(() => expect(screen.getByText("Port must be 1–65535")).toBeInTheDocument())

        fireEvent.change(port, { target: { value: "443" } })
        expect(port.value).toBe("443")
    })
})
