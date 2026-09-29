import { CheckCircle2, ChevronRight, MoreHorizontal, Package, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"
import type { CoreDepReport, DepStatus } from "@/lib/api/core-deps"
import type { CoreProtocol } from "@/lib/types/cores"
import { CoreProtocolIcon } from "@/components/cores/core-protocol-badge"
import { PACKAGES_CELL, SETUP_GRID, STATUS_CELL } from "./setup-grid"

const kindLabels: Record<string, string> = { binary: "Binary" }

interface CoreDepRowProps {
    report: CoreDepReport
    label: string
    family?: string
    expanded: boolean
    disabled: boolean
    onToggleExpand: () => void
    onInstall: () => void
    onUninstall: () => void
}

export function CoreDepRow({ report, label, family, expanded, disabled, onToggleExpand, onInstall, onUninstall }: CoreDepRowProps) {
    const missing = report.deps.filter(dep => !dep.present && !dep.optional)
    const installed = report.deps.some(dep => dep.present)
    const panelId = `core-dep-panel-${report.core_type}`
    const statusText = report.ready ? "Ready" : `${missing.length} missing`
    const statusTone = report.ready ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400"

    return (
        <div
            role="group"
            aria-label={`${label} dependencies`}
            className="border-t border-border first:border-t-0 transition-colors hover:bg-muted/40"
        >
            {/* Header. The div carries the click target; the chevron carries the
                name and state for anyone not using a mouse. */}
            <div onClick={onToggleExpand} className={cn(SETUP_GRID, "min-h-14 cursor-pointer pl-2 pr-3")}>
                <button
                    type="button"
                    aria-label={expanded ? `Collapse ${label}` : `Expand ${label}`}
                    aria-expanded={expanded}
                    aria-controls={panelId}
                    onClick={event => { event.stopPropagation(); onToggleExpand() }}
                    className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                    <ChevronRight className={cn("size-4 transition-transform duration-200", expanded && "rotate-90")} />
                </button>

                {/* Ready or not. Missing packages is a to-do, so amber is earned here. */}
                <span
                    title={report.ready ? "Ready" : "Needs setup"}
                    className={cn("size-2.5 shrink-0 rounded-full", report.ready ? "bg-emerald-500 ring-4 ring-emerald-500/15" : "bg-amber-500 ring-4 ring-amber-500/15")}
                />

                <div className="flex min-w-0 items-center gap-3">
                    <CoreProtocolIcon protocol={report.core_type as CoreProtocol} size="sm" />
                    <div className="min-w-0">
                        <p className="truncate text-sm font-semibold">{label}</p>
                        <p className="truncate text-[12px] text-muted-foreground">
                            {/* Narrow: the status column is gone, so it leads the subline. */}
                            <span className="@md:hidden"><span className={statusTone}>{statusText}</span>{family && " · "}</span>
                            {family}
                        </p>
                    </div>
                </div>

                {/* Binary chips in mono like the tags in Network; the ones blocking setup turn amber. */}
                <div className={cn(PACKAGES_CELL, "min-w-0")}>
                    <div className="flex flex-wrap gap-1">
                        {report.deps.map(dep => {
                            const blocking = !dep.present && !dep.optional
                            return (
                                <span
                                    key={`${dep.kind}:${dep.name}`}
                                    title={`${kindLabels[dep.kind] ?? dep.kind}${blocking ? " · missing" : dep.present ? " · installed" : " · optional"}`}
                                    className={cn(
                                        "inline-block max-w-full truncate rounded-md border px-1.5 py-0.5 font-mono text-[12px] leading-4",
                                        blocking
                                            ? "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"
                                            : "border-border/60 bg-muted/40 text-muted-foreground",
                                        !dep.present && dep.optional && "border-dashed",
                                    )}
                                >
                                    {dep.name}
                                </span>
                            )
                        })}
                    </div>
                </div>

                <div className={STATUS_CELL}>
                    <span className={cn("text-xs font-medium tabular-nums", statusTone)}>{statusText}</span>
                </div>

                {/* The kebab keeps its slot even when absent, so Install lines up down the list. */}
                <div className="flex items-center justify-end gap-1" onClick={event => event.stopPropagation()}>
                    {!report.ready && (
                        <Button variant="outline" size="sm" className="h-7 px-2.5 text-xs" disabled={disabled} onClick={onInstall}>Install</Button>
                    )}
                    <div className="size-7 shrink-0">
                        {installed && (
                            <DropdownMenu>
                                <DropdownMenuTrigger asChild>
                                    <Button size="icon" variant="ghost" className="size-7" aria-label={`${label} options`}><MoreHorizontal className="size-3.5" /></Button>
                                </DropdownMenuTrigger>
                                <DropdownMenuContent align="end">
                                    <DropdownMenuItem disabled={disabled} className="text-destructive" onSelect={onUninstall}>
                                        <Trash2 className="size-3.5" />Uninstall
                                    </DropdownMenuItem>
                                </DropdownMenuContent>
                            </DropdownMenu>
                        )}
                    </div>
                </div>
            </div>

            {expanded && (
                <div id={panelId} className="border-t bg-muted/10 px-4 py-3 sm:px-6">
                    <ul className="grid gap-x-8 gap-y-2 sm:grid-cols-2">
                        {report.deps.map(dep => <DepLine key={`${dep.kind}:${dep.name}`} dep={dep} />)}
                    </ul>
                </div>
            )}
        </div>
    )
}

function DepLine({ dep }: { dep: DepStatus }) {
    const required = !dep.present && !dep.optional
    return (
        <li className="flex items-start gap-2 text-xs">
            {dep.present
                ? <CheckCircle2 className="mt-0.5 size-3.5 shrink-0 text-emerald-500" />
                : <Package className={cn("mt-0.5 size-3.5 shrink-0", required ? "text-amber-500" : "text-muted-foreground")} />}
            <div className="min-w-0 flex-1">
                <p className="font-mono">
                    {dep.name}
                    {dep.optional && <span className="ml-1.5 font-sans text-[11px] text-muted-foreground">optional</span>}
                </p>
                <p className="mt-0.5 text-[11px] text-muted-foreground">
                    {kindLabels[dep.kind] ?? dep.kind}
                    {dep.version && <span className="ml-2 break-all font-mono">{dep.version}</span>}
                    {required && <span className="ml-2 text-amber-600 dark:text-amber-400">missing</span>}
                </p>
            </div>
        </li>
    )
}
