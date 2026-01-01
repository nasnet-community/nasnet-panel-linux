import * as React from "react"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import type { FinalMask, FinalMaskSection } from "@/lib/types"

interface FinalMaskEditorProps {
    value: FinalMask
    onChange: (next: FinalMask | undefined) => void
}

type SectionKey = "tcp" | "udp" | "quicParams"

const SECTIONS: { key: SectionKey; label: string; help: string; placeholder: string }[] = [
    {
        key: "tcp",
        label: "TCP",
        help: 'Array of masks, e.g. [{"type":"salamander","settings":{"password":"…"}}]',
        placeholder: '[{"type":"salamander","settings":{"password":"…"}}]',
    },
    {
        key: "udp",
        label: "UDP",
        help: 'Array of masks (e.g. salamander for Hysteria2 UDP obfuscation)',
        placeholder: '[{"type":"salamander","settings":{"password":"…"}}]',
    },
    {
        key: "quicParams",
        label: "QUIC Params",
        help: "QUIC tuning object (congestion, udpHop, up/down, …)",
        placeholder: '{"congestion":"bbr"}',
    },
]

// FinalMaskEditor: one textarea per section. Each section must be a JSON
// object (not a JSON-encoded string — xray-core rejects). Parses on every
// keystroke so the form payload only holds valid objects.
export function FinalMaskEditor({ value, onChange }: FinalMaskEditorProps) {
    const [drafts, setDrafts] = React.useState<Record<SectionKey, string>>(() => ({
        tcp: stringify(value.tcp),
        udp: stringify(value.udp),
        quicParams: stringify(value.quicParams),
    }))
    const [errors, setErrors] = React.useState<Partial<Record<SectionKey, string>>>({})

    // Re-hydrate textarea contents when the form value changes externally
    // (e.g. dialog reopened with a different inbound).
    React.useEffect(() => {
        setDrafts({
            tcp: stringify(value.tcp),
            udp: stringify(value.udp),
            quicParams: stringify(value.quicParams),
        })
        setErrors({})
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [stringify(value.tcp), stringify(value.udp), stringify(value.quicParams)])

    const update = (key: SectionKey, raw: string) => {
        setDrafts((prev) => ({ ...prev, [key]: raw }))
        const trimmed = raw.trim()
        const next: FinalMask = {
            tcp: value.tcp,
            udp: value.udp,
            quicParams: value.quicParams,
        }
        if (trimmed === "") {
            delete next[key]
            setErrors((e) => ({ ...e, [key]: undefined }))
        } else {
            try {
                const parsed = JSON.parse(trimmed)
                if (typeof parsed !== "object" || parsed === null) {
                    setErrors((e) => ({ ...e, [key]: "must be a JSON array of masks (or object for quicParams)" }))
                    return
                }
                // xray-core: tcp/udp are arrays of Mask objects; quicParams is an object.
                if (key === "quicParams" && Array.isArray(parsed)) {
                    setErrors((e) => ({ ...e, [key]: "quicParams must be a JSON object" }))
                    return
                }
                next[key] = parsed as FinalMaskSection
                setErrors((e) => ({ ...e, [key]: undefined }))
            } catch (err) {
                setErrors((e) => ({ ...e, [key]: err instanceof Error ? err.message : "invalid JSON" }))
                return
            }
        }
        const empty = !next.tcp && !next.udp && !next.quicParams
        onChange(empty ? undefined : next)
    }

    return (
        <div className="space-y-4 rounded-md border p-4 bg-muted/20">
            {SECTIONS.map(({ key, label, help, placeholder }) => (
                <div key={key} className="space-y-2">
                    <Label>{label} (JSON)</Label>
                    <Textarea
                        placeholder={placeholder}
                        value={drafts[key]}
                        onChange={(e) => update(key, e.target.value)}
                        rows={3}
                        className="font-mono text-xs"
                    />
                    {errors[key] ? (
                        <p className="text-xs text-red-500">{errors[key]}</p>
                    ) : (
                        <p className="text-xs text-muted-foreground">{help}</p>
                    )}
                </div>
            ))}
        </div>
    )
}

function stringify(section: FinalMaskSection | undefined): string {
    if (!section || Object.keys(section).length === 0) return ""
    try {
        return JSON.stringify(section, null, 2)
    } catch {
        return ""
    }
}
