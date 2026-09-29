// Substrate shared across the protocol forms: section headings and the
// device-limit strategy select.
import { SelectSetting } from "./settings-fields"
import type { DeviceLimitStrategy } from "@/lib/types/cores"

export function FormSection({
    title,
    description,
    children,
}: {
    title: string
    description?: string
    children: React.ReactNode
}) {
    return (
        <section className="space-y-3">
            <div className="border-b pb-1.5">
                <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                    {title}
                </h4>
                {description && <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>}
            </div>
            {children}
        </section>
    )
}

export function DeviceLimitSetting({
    value,
    onChange,
}: {
    value: DeviceLimitStrategy | undefined
    onChange: (v: DeviceLimitStrategy) => void
}) {
    return (
        <SelectSetting
            label="Device-limit strategy"
            value={value || "accept"}
            onChange={(v) => onChange(v as DeviceLimitStrategy)}
            options={[
                { value: "accept", label: "Accept — evict the oldest device" },
                { value: "reject", label: "Reject — deny the new device" },
            ]}
            description="What happens when a user exceeds their device limit."
        />
    )
}
