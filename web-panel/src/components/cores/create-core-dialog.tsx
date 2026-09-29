import { useEffect, useMemo, useState } from "react"
import { ArrowLeft } from "lucide-react"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { Sheet, SheetContent, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Form } from "@/components/ui/form"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { cn } from "@/lib/utils"
import type { Node } from "@/lib/types"
import { coreProtocolMeta, type CoreInbound, type CoreProtocol } from "@/lib/types/cores"
import { CoreProtocolIcon } from "./core-protocol-badge"
import { ProtocolPicker } from "./protocol-picker"
import { GeneralTab } from "./tabs/general-tab"
import { ProtocolConfigForm } from "./protocol-forms/protocol-config-form"
import { useCoreForm } from "./use-core-form"
import { useNodeProtocols } from "@/lib/queries"

export interface CreateCoreDialogProps {
    nodeId: number
    node: Node
    open: boolean
    onOpenChange: (open: boolean) => void
    editInbound?: CoreInbound | null
}

export function CreateCoreDialog({ nodeId, node, open, onOpenChange, editInbound }: CreateCoreDialogProps) {
    const isMobile = useIsMobile()
    const isEdit = !!editInbound
    const [picked, setPicked] = useState<CoreProtocol | null>(null)

    useEffect(() => {
        if (!open) setPicked(null)
    }, [open])

    // Fetch the node's protocol enable state only while the create picker is
    // shown, so protocols disabled on this node render non-selectable.
    const { data: nodeProtocols = [] } = useNodeProtocols(open && !isEdit ? nodeId : undefined)
    const disabledProtocols = useMemo(
        () => new Set(nodeProtocols.filter((p) => !p.enabled).map((p) => p.protocol)),
        [nodeProtocols],
    )

    const protocol: CoreProtocol | null = isEdit ? editInbound!.protocol : picked
    const meta = protocol ? coreProtocolMeta(protocol) : null

    const title = isEdit
        ? `Edit ${meta?.label} tunnel`
        : protocol
          ? `Configure ${meta?.label}`
          : "New tunnel"
    const description = protocol
        ? `On ${node.name}`
        : "Choose a protocol to stand up a listener on this node."

    const content = protocol ? (
        <CoreFormBody
            key={protocol}
            nodeId={nodeId}
            node={node}
            protocol={protocol}
            editInbound={editInbound}
            onClose={() => onOpenChange(false)}
            onBack={isEdit ? undefined : () => setPicked(null)}
        />
    ) : (
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
            <ProtocolPicker onPick={setPicked} disabledProtocols={disabledProtocols} />
        </div>
    )

    if (isMobile) {
        return (
            <Sheet open={open} onOpenChange={onOpenChange}>
                <SheetContent side="bottom" className="flex h-[95dvh] flex-col rounded-t-xl p-0">
                    <div className="flex items-center gap-2 border-b p-4">
                        {protocol && !isEdit && (
                            <button
                                type="button"
                                onClick={() => setPicked(null)}
                                className="-ml-1 rounded-lg p-1 hover:bg-accent"
                                aria-label="Back"
                            >
                                <ArrowLeft className="size-5" />
                            </button>
                        )}
                        <div>
                            <SheetTitle className="text-base">{title}</SheetTitle>
                            <SheetDescription className="text-xs">{description}</SheetDescription>
                        </div>
                    </div>
                    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">{content}</div>
                </SheetContent>
            </Sheet>
        )
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            {/* Once a protocol is chosen the body is tabbed, so the dialog gets a
                fixed height: without it the modal resizes as you switch tabs.
                The picker step keeps its natural height. */}
            <DialogContent
                className={cn(
                    "flex max-h-[92vh] max-w-2xl flex-col gap-0 p-0",
                    protocol && "h-[min(92vh,44rem)]",
                )}
            >
                <DialogHeader className="border-b p-5">
                    <DialogTitle className="flex items-center gap-2">
                        {protocol && <CoreProtocolIcon protocol={protocol} size="sm" />}
                        {title}
                    </DialogTitle>
                    <DialogDescription>{description}</DialogDescription>
                </DialogHeader>
                <div className="flex min-h-0 flex-1 flex-col overflow-hidden">{content}</div>
            </DialogContent>
        </Dialog>
    )
}

function TabDot() {
    return <span className="ml-1.5 size-1.5 rounded-full bg-destructive" aria-hidden />
}

function CoreFormBody({
    nodeId,
    node,
    protocol,
    editInbound,
    onClose,
    onBack,
}: {
    nodeId: number
    node: Node
    protocol: CoreProtocol
    editInbound?: CoreInbound | null
    onClose: () => void
    onBack?: () => void
}) {
    const { form, slot, isEdit, submit, getSettings, setSettings, isSubmitting } = useCoreForm({
        nodeId,
        protocol,
        editInbound,
    })
    const onSubmit = submit(onClose)

    const errors = form.formState.errors as Record<string, unknown>
    const generalHasError = !!(errors.port || errors.listen || errors.remark)
    const builtIn = node.connect_mode === "local"
    const slotErrorsRaw = (errors[slot] ?? {}) as Record<string, { message?: string }>
    const configHasError = !!errors[slot]
    const slotFieldErrors: Record<string, string> = {}
    for (const [k, v] of Object.entries(slotErrorsRaw)) {
        if (v && typeof v === "object" && "message" in v && v.message) {
            slotFieldErrors[k] = String(v.message)
        }
    }

    return (
        <Form {...form}>
            <form onSubmit={onSubmit} className="flex min-h-0 flex-1 flex-col">
                <Tabs defaultValue="general" className="flex min-h-0 flex-1 flex-col">
                    <div className="px-5 pt-4">
                        <TabsList>
                            <TabsTrigger value="general">
                                General{generalHasError && <TabDot />}
                            </TabsTrigger>
                            <TabsTrigger value="config">
                                Configuration{configHasError && <TabDot />}
                            </TabsTrigger>
                        </TabsList>
                    </div>
                    <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
                        <TabsContent value="general" className="mt-0">
                            <GeneralTab form={form} nodeName={node.name} builtIn={builtIn} />
                        </TabsContent>
                        <TabsContent value="config" className="mt-0 space-y-4">
                            <ProtocolConfigForm
                                protocol={protocol}
                                value={getSettings()}
                                onChange={setSettings}
                                builtIn={builtIn}
                                errors={slotFieldErrors}
                            />
                        </TabsContent>
                    </div>
                </Tabs>

                <div
                    className={cn(
                        "flex items-center gap-2 border-t p-4",
                        onBack ? "justify-between" : "justify-end",
                    )}
                >
                    {onBack && (
                        <Button type="button" variant="ghost" onClick={onBack}>
                            <ArrowLeft className="size-4" />
                            Back
                        </Button>
                    )}
                    <div className="flex items-center gap-2">
                        {isEdit && (
                            <p className="mr-1 hidden text-xs text-muted-foreground sm:block">
                                Applies on save; active sessions may briefly reconnect.
                            </p>
                        )}
                        <Button type="button" variant="outline" onClick={onClose}>
                            Cancel
                        </Button>
                        <Button type="submit" disabled={isSubmitting}>
                            {isSubmitting ? "Saving…" : isEdit ? "Save changes" : "Create tunnel"}
                        </Button>
                    </div>
                </div>
            </form>
        </Form>
    )
}
