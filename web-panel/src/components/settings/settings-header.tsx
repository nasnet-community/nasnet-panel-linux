import { useRef, useState } from "react"
import { Link } from "react-router"
import { Archive, Download, Ellipsis, RotateCw, Upload } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
    DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { restartServer } from "@/lib/api/backup"
import { getApiBaseUrl } from "@/lib/config"
import { useExportSettings } from "@/lib/queries/use-settings"
import { toast } from "@/lib/toast"
import { cn } from "@/lib/utils"
import { pendingCards, since, when, type Ctx } from "./catalog"
import { ImportSettingsDialog } from "./import-settings-dialog"

/** Restarts the panel, then reloads the page once it answers again. */
function useRestartPanel(waiting: string[]) {
    const confirm = useConfirm()
    const [restarting, setRestarting] = useState(false)
    const timer = useRef<ReturnType<typeof setInterval>>(undefined)

    const restart = async () => {
        const ok = await confirm({
            title: "Restart the panel?",
            description: `${waiting.length ? `Applies the change waiting in ${waiting.join(", ")}. ` : ""}The panel, the bot, subscription pages and this router's Xray are down for a few seconds. Remote nodes keep serving users, and reverse nodes reconnect on their own.`,
            confirmLabel: "Restart",
        })
        if (!ok) return
        try {
            const res = await restartServer()
            if (!res.success) {
                toast.error("The panel didn't restart", { description: res.error })
                return
            }
        } catch {
            // the connection can drop as the panel goes down; wait for it anyway
        }
        setRestarting(true)
        toast.info("Restarting the panel…", { description: "The page reloads when it's back." })
        const started = Date.now()
        let warned = false
        clearInterval(timer.current)
        timer.current = setInterval(async () => {
            try {
                const resp = await fetch(`${getApiBaseUrl()}/health`, { cache: "no-store" })
                if (resp.ok && Date.now() - started > 3000) {
                    clearInterval(timer.current)
                    window.location.reload()
                }
            } catch {
                // still down
            }
            if (!warned && Date.now() - started > 90_000) {
                warned = true
                toast.error("The panel hasn't come back after 90 seconds", { description: "Check the server. This page keeps trying." })
            }
        }, 2000)
    }
    return { restart, restarting }
}

export function SettingsHeader({ ctx, phone }: { ctx: Ctx; phone: boolean }) {
    const s = ctx.status
    const waiting = pendingCards(s).map(c => c.title)
    const { restart, restarting } = useRestartPanel(waiting)
    const exportSettings = useExportSettings()
    const [importOpen, setImportOpen] = useState(false)

    const chip = waiting.length > 0 && (
        <span className="inline-flex h-8 items-center gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 text-xs font-medium text-amber-600 dark:text-amber-500">
            <span className="size-1.5 rounded-full bg-amber-500" aria-hidden />
            Restart pending · <span className="font-semibold">{waiting.join(", ")}</span>
        </span>
    )
    const menu = (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="outline" size={phone ? "icon" : "sm"} className={phone ? "size-10" : "h-8 px-2.5"} aria-label="More">
                    <Ellipsis className="size-4" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-60">
                {phone && (
                    <>
                        <DropdownMenuItem onSelect={() => void restart()} disabled={restarting}>
                            <RotateCw className="size-4" />Restart server
                            {waiting.length > 0 && <span className="ml-auto flex items-center gap-1.5 text-xs text-amber-500"><span className="size-1.5 rounded-full bg-amber-500" />pending</span>}
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                    </>
                )}
                <DropdownMenuItem onSelect={() => exportSettings.mutate()} disabled={exportSettings.isPending}>
                    <Download className="size-4" />Export settings
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setImportOpen(true)}>
                    <Upload className="size-4" />Import settings…
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem asChild><Link to="/backup"><Archive className="size-4" />Backups</Link></DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    )

    return (
        <div className="space-y-3">
            <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                    <h1 className="text-2xl font-bold tracking-tight sm:text-3xl">Settings</h1>
                    {s && (
                        <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm text-muted-foreground">
                            <span className="inline-flex items-center gap-1.5"><span className="size-1.5 rounded-full bg-emerald-500" aria-hidden />Up {since(s.hub.started_at)}</span>
                            {!phone && <><span aria-hidden className="opacity-50">·</span><span>since {when(s.hub.started_at)}</span></>}
                            <span aria-hidden className="opacity-50">·</span>
                            <span>build <span className="font-mono text-[13px]">{s.hub.version}</span></span>
                        </p>
                    )}
                </div>
                <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
                    {!phone && chip}
                    {!phone && (
                        <Button
                            variant="outline" size="sm" onClick={() => void restart()} disabled={restarting}
                            className={cn("h-8 gap-1.5", waiting.length > 0 && "border-amber-500/50 text-amber-600 hover:text-amber-600 dark:text-amber-500 dark:hover:text-amber-500")}
                        >
                            <RotateCw className={cn("size-3.5", restarting && "animate-spin")} />Restart server
                        </Button>
                    )}
                    {menu}
                </div>
            </div>
            {phone && chip}
            <ImportSettingsDialog open={importOpen} onOpenChange={setImportOpen} values={ctx.values} />
        </div>
    )
}
