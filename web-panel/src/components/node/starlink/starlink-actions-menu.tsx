import { useState } from "react"
import { toast } from "@/lib/toast"
import {
    ArrowDownToLine, ArrowUpFromLine, ChevronDown, Eraser, LocateOff, Locate,
    RotateCcw, Stethoscope, TriangleAlert, CheckCircle2, XCircle,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import {
    DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel,
    DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { useStarlinkAction } from "@/lib/queries/use-starlink"
import type { StarlinkAction, StarlinkStatus } from "@/lib/types"
import { cn } from "@/lib/utils"
import { starlinkErrorMessage } from "./starlink-helpers"

interface StarlinkActionsMenuProps {
    nodeId: number
    nodeName: string
    status: StarlinkStatus
    /** When set the menu is disabled and this explains why. */
    disabledReason?: string
    onStarted?: (action: StarlinkAction) => void
    className?: string
}

interface ActionSpec {
    title: string
    description: string
    confirmLabel: string
    variant: "default" | "warning" | "destructive"
    typeToConfirm?: string
}

export function StarlinkActionsMenu({ nodeId, nodeName, status, disabledReason, onStarted, className }: StarlinkActionsMenuProps) {
    const confirm = useConfirm()
    const mutation = useStarlinkAction(nodeId)
    const [selfTest, setSelfTest] = useState<{ passed: boolean; report: string } | null>(null)
    const noMotors = status.has_actuators !== "HAS_ACTUATORS_YES"

    const run = (action: StarlinkAction, spec: ActionSpec) =>
        confirm({
            ...spec,
            onConfirm: async () => {
                try {
                    const res = await mutation.mutateAsync({ action, confirm: spec.typeToConfirm })
                    toast.success(res.message)
                    if (res.self_test) setSelfTest(res.self_test)
                    onStarted?.(action)
                } catch (err) {
                    toast.error(starlinkErrorMessage(err))
                    throw err
                }
            },
        })

    const trigger = (
        <Button variant="outline" size="sm" disabled={!!disabledReason} className={cn("h-11 gap-1.5 sm:h-7", className)}>
            Dish actions
            <ChevronDown className="h-3.5 w-3.5 opacity-60" aria-hidden />
        </Button>
    )

    // No app-wide TooltipProvider exists; components wrap their own.
    if (disabledReason) {
        return (
            <TooltipProvider>
                <Tooltip>
                    <TooltipTrigger asChild><span tabIndex={0}>{trigger}</span></TooltipTrigger>
                    <TooltipContent>{disabledReason}</TooltipContent>
                </Tooltip>
            </TooltipProvider>
        )
    }

    return (
        <>
            <DropdownMenu>
                <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-72">
                    <Item icon={RotateCcw} label="Reboot dish" hint="Link drops for 1–3 minutes"
                        onSelect={() => run("reboot", {
                            title: "Reboot the dish?",
                            description: `${nodeName} loses its Starlink link for 1–3 minutes while the dish restarts.`,
                            confirmLabel: "Reboot", variant: "warning",
                        })} />
                    {status.stow_requested ? (
                        <Item icon={ArrowUpFromLine} label="Unstow" hint="Resume the link"
                            onSelect={() => run("unstow", {
                                title: "Unstow the dish?",
                                description: "The dish resumes tracking and reconnects within a couple of minutes.",
                                confirmLabel: "Unstow", variant: "default",
                            })} />
                    ) : (
                        <Item icon={ArrowDownToLine} label="Stow" hint={noMotors ? "Pauses the link until unstowed" : "Folds the dish flat; link stops"}
                            onSelect={() => run("stow", {
                                title: "Stow the dish?",
                                description: noMotors
                                    ? "This dish has no motors, so stowing only pauses the link. It stays down until you unstow."
                                    : "The dish tilts to its stowed position and the link stays down until you unstow.",
                                confirmLabel: "Stow", variant: "warning",
                            })} />
                    )}
                    <Item icon={Eraser} label="Clear obstruction map" hint="Map rebuilds over hours"
                        onSelect={() => run("clear-map", {
                            title: "Clear the obstruction map?",
                            description: "The dish forgets what it has mapped. The map rebuilds over the next hours; the link is not affected.",
                            confirmLabel: "Clear map", variant: "warning",
                        })} />
                    <Item icon={Stethoscope} label="Run self-test" hint="Hardware check, shows a report"
                        onSelect={() => run("self-test", {
                            title: "Run a dish self-test?",
                            description: "The dish runs its hardware self-test and returns a report.",
                            confirmLabel: "Run test", variant: "default",
                        })} />

                    <DropdownMenuSeparator />
                    <DropdownMenuLabel className="text-[12px] font-medium uppercase tracking-[0.08em] text-red-400/80">Danger zone</DropdownMenuLabel>
                    {status.gps_inhibited ? (
                        <Item icon={Locate} label="Restore GPS" hint="Dish uses GPS again"
                            onSelect={() => run("gps-restore", {
                                title: "Restore GPS?",
                                description: "The dish goes back to using GPS for positioning.",
                                confirmLabel: "Restore GPS", variant: "default",
                            })} />
                    ) : (
                        <Item icon={LocateOff} label="Inhibit GPS" hint="Helps against spoofing; can drop service" danger
                            onSelect={() => run("gps-inhibit", {
                                title: "Inhibit GPS?",
                                description: "The dish stops trusting GPS and positions itself from satellites. It helps where GPS is jammed or spoofed, but if positioning fails the dish can lose service.",
                                confirmLabel: "Inhibit GPS", variant: "destructive",
                            })} />
                    )}
                    <Item icon={TriangleAlert} label="Factory reset" hint="Erases dish settings" danger
                        onSelect={() => run("factory-reset", {
                            title: "Factory reset the dish?",
                            description: "The dish erases its settings and restarts. The link drops, and settings made in the Starlink app are lost.",
                            confirmLabel: "Factory reset", variant: "destructive", typeToConfirm: nodeName,
                        })} />
                </DropdownMenuContent>
            </DropdownMenu>

            <Dialog open={!!selfTest} onOpenChange={(open) => !open && setSelfTest(null)}>
                <DialogContent className="sm:max-w-lg">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            {selfTest?.passed
                                ? <CheckCircle2 className="h-5 w-5 text-emerald-400" aria-hidden />
                                : <XCircle className="h-5 w-5 text-red-400" aria-hidden />}
                            Self-test {selfTest?.passed ? "passed" : "failed"}
                        </DialogTitle>
                        <DialogDescription>Report from the dish.</DialogDescription>
                    </DialogHeader>
                    <pre className="max-h-[50vh] overflow-auto whitespace-pre-wrap rounded-lg bg-muted/40 p-3 font-mono text-xs">
                        {selfTest?.report || "The dish returned no report."}
                    </pre>
                </DialogContent>
            </Dialog>
        </>
    )
}

function Item({ icon: Icon, label, hint, danger, onSelect }: {
    icon: React.ComponentType<{ className?: string }>
    label: string
    hint: string
    danger?: boolean
    onSelect: () => void
}) {
    return (
        <DropdownMenuItem onSelect={onSelect} className="items-start gap-2.5 py-2">
            <Icon className={cn("mt-0.5 h-4 w-4 shrink-0", danger ? "text-red-400" : "text-muted-foreground")} />
            <span className="flex flex-col">
                <span className={cn("text-sm font-medium", danger && "text-red-400")}>{label}</span>
                <span className="text-[12px] leading-4 text-muted-foreground">{hint}</span>
            </span>
        </DropdownMenuItem>
    )
}
