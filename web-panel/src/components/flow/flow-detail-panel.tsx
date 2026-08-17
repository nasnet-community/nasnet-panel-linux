import { Check, Copy } from "lucide-react"
import { useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
    Sheet,
    SheetContent,
    SheetDescription,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import type { FlowMismatch, FlowNode, FlowNodeStatus } from "@/lib/types/flow"

function statusVariant(s: FlowNodeStatus) {
    switch (s) {
        case "ok":
            return "success" as const
        case "warn":
            return "warning" as const
        case "down":
            return "danger" as const
        default:
            return "outline" as const
    }
}

interface FlowDetailPanelProps {
    node: FlowNode | null
    mismatches: FlowMismatch[]
    onClose: () => void
}

export function FlowDetailPanel({ node, mismatches, onClose }: FlowDetailPanelProps) {
    return (
        <Sheet open={!!node} onOpenChange={(open) => !open && onClose()}>
            <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-md">
                {node && (
                    <>
                        <SheetHeader>
                            <div className="flex items-center justify-between gap-3 pr-6">
                                <SheetTitle>{node.label}</SheetTitle>
                                <Badge variant={statusVariant(node.status)}>{node.status}</Badge>
                            </div>
                            {node.sublabel && <SheetDescription>{node.sublabel}</SheetDescription>}
                        </SheetHeader>

                        <div className="space-y-4 px-4 pb-6">
                            {node.hint && (
                                <Alert variant="info">
                                    <AlertDescription>{node.hint}</AlertDescription>
                                </Alert>
                            )}

                            {mismatches.map((m) => (
                                <Alert
                                    key={m.rule + m.message}
                                    variant={m.severity === "error" ? "destructive" : "warning"}
                                >
                                    <AlertDescription>
                                        <span className="block">{m.message}</span>
                                        {m.expected && (
                                            <span className="text-text-secondary mt-1 block font-mono text-xs">
                                                expected: {m.expected}
                                            </span>
                                        )}
                                        {m.actual && (
                                            <span className="text-text-secondary block font-mono text-xs">
                                                actual: {m.actual}
                                            </span>
                                        )}
                                    </AlertDescription>
                                </Alert>
                            ))}

                            {(node.detail ?? []).map((section) => (
                                <DetailSection
                                    key={section.title}
                                    title={section.title}
                                    lines={section.lines}
                                />
                            ))}
                        </div>
                    </>
                )}
            </SheetContent>
        </Sheet>
    )
}

// Raw kernel text, selectable and copyable: debugging means pasting this
// somewhere else.
function DetailSection({ title, lines }: { title: string; lines: string[] }) {
    const [copied, setCopied] = useState(false)

    function copy() {
        void navigator.clipboard?.writeText(lines.join("\n"))
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
    }

    return (
        <section className="space-y-1.5">
            <div className="flex items-center justify-between gap-2">
                <h4 className="text-text-secondary text-xs font-medium">{title}</h4>
                <Button variant="ghost" size="sm" className="h-6 px-2" onClick={copy}>
                    {copied ? (
                        <Check className="h-3 w-3" />
                    ) : (
                        <Copy className="h-3 w-3" />
                    )}
                    <span className="sr-only">Copy {title}</span>
                </Button>
            </div>
            <pre className="bg-surface-2 overflow-x-auto rounded-md p-2 font-mono text-xs break-all whitespace-pre-wrap select-all">
                {lines.join("\n")}
            </pre>
        </section>
    )
}
