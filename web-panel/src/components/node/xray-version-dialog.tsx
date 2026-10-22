import { useState, useEffect, useRef, useCallback } from "react"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogTrigger } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Badge } from "@/components/ui/badge"
import { QueryError } from "@/components/ui/query-error"
import { getXrayCatalog, getXrayRoutes, streamXrayUpdate, type XrayRoutes, type XrayUpdateProgress } from "@/lib/api/xray-update"
import type { XrayRelease } from "@/lib/api/nodes"
import { Check, ChevronDown, ExternalLink, Loader2, Settings } from "lucide-react"

interface XrayVersionDialogProps {
    nodeId: number
    currentVersion?: string
    isOnline: boolean
    /** the built-in node, which can follow the panel's tested version */
    isLocal?: boolean
    onRefresh?: () => void
}
const errorMessage = (error: unknown) => error instanceof Error ? error.message : "An unexpected error occurred."
export function releaseDate(value: string) {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return "Release date unavailable"
    const days = Math.max(0, Math.floor((Date.now() - date.getTime()) / 86_400_000))
    return `${date.toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" })} · ${days === 0 ? "Today" : `${days} day${days === 1 ? "" : "s"} ago`}`
}
function cleanVersion(value = "") { return value.replace(/^Xray\s+/i, "").split(/\s/)[0].replace(/^v/, "") }
function olderThan(version: string, current: string) {
    return version.localeCompare(current, undefined, { numeric: true }) < 0
}
const routeName = (route?: string) => route === "direct" ? "Direct" : route === "proxy" ? "Via proxy" : route === "environment_proxy" ? "Via server environment proxy" : "Unknown"
/** "Direct · foreign uplinks" in router mode, plain "Direct" otherwise. */
const routeLine = (route?: string, egress?: string) => egress ? `${routeName(route)} · ${egress}` : routeName(route)
const stages = [
    ["preparing", "Check the node"], ["downloading", "Prepare the panel's copy"],
    ["installing", "Download, verify, install & restart"], ["verifying", "Check running version & health"],
] as const

export function XrayVersionDialog({ nodeId, currentVersion, isOnline, isLocal, onRefresh }: XrayVersionDialogProps) {
    const [open, setOpen] = useState(false)
    const [releases, setReleases] = useState<XrayRelease[]>([])
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState<string>()
    const [routes, setRoutes] = useState<XrayRoutes>()
    const [routeError, setRouteError] = useState<string>()
    const [expandedRelease, setExpandedRelease] = useState<string | null>(null)
    const [selectedVersion, setSelectedVersion] = useState<string | null>(null)
    const [confirming, setConfirming] = useState(false)
    const [updating, setUpdating] = useState(false)
    const [progress, setProgress] = useState<XrayUpdateProgress>()
    const [visited, setVisited] = useState<string[]>([])
    const request = useRef(0)
    const updateLock = useRef(false)
    const controller = useRef<AbortController | null>(null)
    const cleanCurrentVersion = cleanVersion(currentVersion)
    const downgrade = selectedVersion && cleanCurrentVersion && olderThan(selectedVersion, cleanCurrentVersion)
    const tested = routes?.tested
    const following = isLocal && !routes?.builtin_pick
    // newer than tested is allowed, and said out loud
    const untested = !!(selectedVersion && tested && selectedVersion !== tested && !olderThan(selectedVersion, tested))
    const load = useCallback(async (refresh = false) => {
        const id = ++request.current
        setLoading(true)
        setError(undefined)
        setRouteError(undefined)
        setConfirming(false)
        // Route information must remain available even if GitHub is unreachable.
        await Promise.all([
            getXrayCatalog(refresh).then(res => {
                if (id !== request.current) return
                if (!res.success || !res.data) throw new Error(res.error || "Could not fetch releases.")
                setReleases([...res.data].sort((a, b) => Date.parse(b.published_at) - Date.parse(a.published_at)))
                setSelectedVersion(previous => res.data?.some(release => release.version === previous) ? previous : null)
            }).catch(e => { if (id === request.current) setError(errorMessage(e)) }),
            getXrayRoutes().then(res => {
                if (id !== request.current) return
                if (!res.success || !res.data) throw new Error("Connection route unavailable.")
                setRoutes(res.data)
            }).catch(e => { if (id === request.current) { setRoutes(undefined); setRouteError(errorMessage(e)) } }),
        ])
        if (id === request.current) setLoading(false)
    }, [])
    useEffect(() => {
        if (!open) return
        setSelectedVersion(null); setConfirming(false); setProgress(undefined); setVisited([]); setExpandedRelease(null)
        void load()
        return () => { request.current++ }
    }, [open, nodeId, load])
    useEffect(() => () => controller.current?.abort(), [])
    useEffect(() => { setConfirming(false) }, [currentVersion, isOnline])

    const handleUpdate = async () => {
        if (!selectedVersion || !isOnline || updateLock.current) return
        if (!confirming) { setConfirming(true); return }
        updateLock.current = true
        setUpdating(true); setConfirming(false); setVisited([])
        setProgress({ stage: "preparing", detail: "Requesting update…" })
        controller.current = new AbortController()
        try {
            await streamXrayUpdate(nodeId, selectedVersion, next => {
                setProgress(next)
                setVisited(previous => previous.includes(next.stage) ? previous : [...previous, next.stage])
                if (next.stage === "succeeded") onRefresh?.()
            }, controller.current.signal)
        } catch (e) {
            setProgress({ stage: "failed", detail: `${errorMessage(e)} Check node status before attempting another update.` })
        } finally { updateLock.current = false; setUpdating(false) }
    }
    const finished = progress?.stage === "succeeded" || progress?.stage === "failed"
    const operationStages = visited.filter(stage => stages.some(([key]) => key === stage))
    const lastStage = operationStages[operationStages.length - 1]
    const lastStageIndex = stages.findIndex(([key]) => key === lastStage)

    return (
        <Dialog open={open} onOpenChange={next => { if (!updateLock.current) setOpen(next) }}>
            <DialogTrigger asChild>
                <button disabled={!isOnline} className="text-muted-foreground/50 group-hover:text-indigo-500 transition-colors disabled:opacity-30 disabled:cursor-not-allowed" title={isOnline ? "Manage Xray version" : "Node is offline"}>
                    <Settings className="w-4 h-4" />
                </button>
            </DialogTrigger>
            <DialogContent className="sm:max-w-xl max-h-[90dvh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>Update Xray Version</DialogTitle>
                    <DialogDescription>
                        {cleanCurrentVersion ? `Currently running v${cleanCurrentVersion}` : "Select a version to install"}
                        {isLocal && routes && (following ? " · follows the panel" : " · picked by hand")}
                    </DialogDescription>
                    <div className="flex flex-wrap justify-center gap-x-4 gap-y-1 pt-1 text-[12px] text-muted-foreground sm:justify-start" title={routeError}>
                        <span>Release checks: <span className="text-foreground/80">{routeLine(routes?.release_checks, routes?.release_checks_egress)}</span></span>
                        <span>Panel binary download: <span className="text-foreground/80">{routeLine(routes?.binary_download, routes?.binary_download_egress)}</span></span>
                    </div>
                </DialogHeader>
                {!progress && <>
                    {error && <QueryError title="Couldn’t fetch releases" description={`${error}${releases.length ? " Previously loaded releases are shown below." : ""}`} onRetry={() => void load(true)} isRetrying={loading} />}
                    {loading && <div role="status" className="flex items-center justify-center gap-2 py-4 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />Loading releases…</div>}
                    {isLocal && tested && !following && (
                        <button
                            type="button" aria-pressed={selectedVersion === tested} disabled={loading}
                            onClick={() => { setSelectedVersion(tested); setConfirming(false) }}
                            className={`w-full rounded-lg border px-3 py-2.5 text-left text-sm transition-colors ${selectedVersion === tested ? "border-indigo-500/50 bg-indigo-500/10" : "border-border/50 hover:bg-muted/40"}`}
                        >
                            <span className="font-medium">Follow the panel</span>
                            <span className="mt-0.5 block text-[12px] text-muted-foreground">Run the tested v{tested}, and move with each panel update.</span>
                        </button>
                    )}
                    <ScrollArea className="h-[min(420px,48dvh)] rounded-lg border border-border/50">
                        <div className="space-y-1 p-1 pr-3">
                            {releases.map((release, index) => {
                                const installed = release.version === cleanCurrentVersion
                                const selected = release.version === selectedVersion
                                return <div key={release.tag} className={`overflow-hidden rounded-md transition-colors ${selected ? "bg-indigo-500/10 ring-1 ring-inset ring-indigo-500/50 hover:bg-indigo-500/15" : !installed && !loading ? "hover:bg-muted/40" : ""}`}>
                                    <div className="flex items-center">
                                        <button aria-pressed={selected} disabled={installed || loading} onClick={() => { setSelectedVersion(release.version); setConfirming(false) }} className="min-w-0 flex-1 px-3 py-2.5 text-left disabled:cursor-default">
                                            <div className="flex flex-wrap items-center gap-2">
                                                <span className="font-mono text-sm font-semibold">v{release.version}</span>
                                                {installed ? <Badge variant="outline" className="text-emerald-500 border-emerald-500/30">Installed</Badge> : index === 0 ? <Badge variant="secondary">Latest</Badge> : null}
                                                {release.version === tested && <Badge variant="outline" title="This panel was tested with it">Tested</Badge>}
                                            </div>
                                            <p className="mt-1 text-[12px] text-muted-foreground">{releaseDate(release.published_at)}{release.prerelease && " · Pre-release"}</p>
                                        </button>
                                        {selected && <Check aria-label="Selected" className="h-4 w-4 shrink-0 text-indigo-500" />}
                                        <button
                                            type="button"
                                            aria-label={`Release notes for v${release.version}`}
                                            aria-expanded={expandedRelease === release.tag}
                                            aria-controls={`xray-notes-${nodeId}-${release.tag}`}
                                            onClick={() => setExpandedRelease(previous => previous === release.tag ? null : release.tag)}
                                            className="m-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
                                        >
                                            <ChevronDown className={`h-4 w-4 transition-transform ${expandedRelease === release.tag ? "rotate-180" : ""}`} />
                                        </button>
                                    </div>
                                    {expandedRelease === release.tag && <div id={`xray-notes-${nodeId}-${release.tag}`} className="border-t border-border/30 px-3 py-2.5 text-xs">
                                        <p className="max-h-40 overflow-y-auto whitespace-pre-wrap break-words">{release.notes || "No release notes provided."}</p>
                                        <a className="mt-2 inline-flex items-center gap-1 text-indigo-500 underline" href={`https://github.com/XTLS/Xray-core/releases/tag/${encodeURIComponent(release.tag)}`} target="_blank" rel="noreferrer">Full changelog <ExternalLink className="h-3 w-3" /></a>
                                    </div>}
                                </div>
                            })}
                            {!loading && !error && releases.length === 0 && <p className="p-8 text-center text-sm text-muted-foreground">No releases published.</p>}
                        </div>
                    </ScrollArea>
                    <Button onClick={handleUpdate} disabled={!selectedVersion || loading || !isOnline} className="w-full h-auto min-h-11 py-3 whitespace-normal" variant={confirming ? "destructive" : "default"}>
                        {confirming ? <span>Confirm {downgrade ? "downgrade" : "update"}: {cleanCurrentVersion ? `v${cleanCurrentVersion}` : "current version"} → v{selectedVersion}<span className="block mt-1 text-xs font-normal">Xray will restart. Active connections may briefly disconnect.<br />Click again to confirm.</span></span> : selectedVersion ? `${downgrade ? "Downgrade" : "Update"} to v${selectedVersion}` : "Select a version"}
                    </Button>
                    {untested && (
                        <p className="rounded-md bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400">
                            v{selectedVersion} isn't tested with this panel, which is tested with v{tested}.
                            {isLocal && " The built-in node stays on it through panel updates, until you pick Follow the panel."}
                        </p>
                    )}
                    {confirming && <Button variant="ghost" size="sm" onClick={() => setConfirming(false)}>Cancel confirmation</Button>}
                </>}
                {progress && <div className="space-y-4">
                    <ol className="space-y-3 text-sm">
                        {stages.map(([stage, title], index) => <li key={stage} className="flex items-center gap-2">
                            {updating && progress.stage === stage ? <Loader2 className="h-4 w-4 animate-spin" /> : visited.includes(stage) && (progress.stage === "succeeded" || index < lastStageIndex) ? <Check className="h-4 w-4 text-emerald-500" /> : <span className="h-4 w-4 rounded-full border" />}
                            {title}{stage === "downloading" && visited.includes("installing") && !visited.includes(stage) ? " (skipped)" : ""}
                        </li>)}
                    </ol>
                    <div role={progress.stage === "failed" ? "alert" : "status"} className={`rounded-lg border p-3 text-sm ${progress.stage === "failed" ? "border-destructive text-destructive" : ""}`}>
                        {progress.stage === "succeeded" && <p className="font-semibold mb-1">Verified: v{selectedVersion} is running</p>}
                        {progress.detail}
                    </div>
                    {finished && <Button className="w-full" onClick={() => setOpen(false)}>Close</Button>}
                </div>}
            </DialogContent>
        </Dialog>
    )
}
