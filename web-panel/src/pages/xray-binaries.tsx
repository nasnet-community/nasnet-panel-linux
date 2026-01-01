import { useState, useRef } from "react"
import { Upload, Trash2, Rocket, Loader2, FileCheck2, Cpu } from "lucide-react"
import { PageHeader } from "@/components/shared/page-header"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table"
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog"
import { cn, formatBytes } from "@/lib/utils"
import { useXrayVersions, useUploadXrayBinary, useDeleteXrayVersion } from "@/lib/queries/use-xray"
import { useNodes, useBulkUpdateXrayVersion } from "@/lib/queries/use-nodes"
import type { XrayVersionInfo } from "@/lib/api/xray"

const VERSION_RE = /^[0-9]+(\.[0-9]+)*(-[a-z0-9]+)*$/

function platformSize(v: XrayVersionInfo): number {
    return Object.values(v.platforms).reduce((sum, p) => sum + (p.size || 0), 0)
}

export default function XrayBinaries() {
    const { data, isLoading } = useXrayVersions()
    const upload = useUploadXrayBinary()
    const del = useDeleteXrayVersion()
    const deploy = useBulkUpdateXrayVersion()
    const { data: nodes } = useNodes()

    const [file, setFile] = useState<File | null>(null)
    const [version, setVersion] = useState("")
    const [dragging, setDragging] = useState(false)
    const fileInput = useRef<HTMLInputElement>(null)

    // Deploy + delete dialog targets (the version being acted on).
    const [deployTarget, setDeployTarget] = useState<string | null>(null)
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
    const [picked, setPicked] = useState<Set<number>>(new Set())

    const versions = data?.versions ?? []
    const versionValid = version.length > 0 && version.length <= 20 && VERSION_RE.test(version)
    const canUpload = !!file && versionValid && !upload.isPending

    const onUpload = () => {
        if (!file || !versionValid) return
        upload.mutate({ version, file }, {
            onSuccess: () => { setFile(null); setVersion(""); if (fileInput.current) fileInput.current.value = "" },
        })
    }

    const togglePick = (id: number) => setPicked(prev => {
        const next = new Set(prev)
        next.has(id) ? next.delete(id) : next.add(id)
        return next
    })

    const onDeploy = () => {
        if (!deployTarget || picked.size === 0) return
        deploy.mutate({ ids: [...picked], version: deployTarget }, {
            onSuccess: () => { setDeployTarget(null); setPicked(new Set()) },
        })
    }

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            <PageHeader
                title="Xray Core Binaries"
                description="Upload a custom xray-core build and deploy it to nodes. Architecture is detected from the binary."
            />

            {/* Upload */}
            <Card>
                <CardHeader><CardTitle className="text-base">Upload binary</CardTitle></CardHeader>
                <CardContent className="space-y-4">
                    <div
                        onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
                        onDragLeave={() => setDragging(false)}
                        onDrop={(e) => { e.preventDefault(); setDragging(false); const f = e.dataTransfer.files?.[0]; if (f) setFile(f) }}
                        onClick={() => fileInput.current?.click()}
                        className={cn(
                            "flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed px-6 py-10 text-center transition-colors",
                            dragging ? "border-primary bg-primary/5" : "border-border hover:border-primary/50",
                        )}
                    >
                        {file ? (
                            <>
                                <FileCheck2 className="size-7 text-status-success" />
                                <div className="text-sm font-medium">{file.name}</div>
                                <div className="text-xs text-muted-foreground">{formatBytes(file.size)}</div>
                            </>
                        ) : (
                            <>
                                <Upload className="size-7 text-muted-foreground" />
                                <div className="text-sm">Drop the <code>xray</code> binary here, or click to choose</div>
                            </>
                        )}
                        <input
                            ref={fileInput} type="file" className="hidden"
                            onChange={(e) => setFile(e.target.files?.[0] ?? null)}
                        />
                    </div>

                    <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
                        <div className="flex-1 space-y-1">
                            <Label htmlFor="xray-version">Version label</Label>
                            <Input
                                id="xray-version" placeholder="e.g. 1.260123.0-wg1" value={version}
                                onChange={(e) => setVersion(e.target.value.trim())}
                            />
                            <p className={cn("text-xs", version && !versionValid ? "text-status-danger" : "text-muted-foreground")}>
                                Digits, dots, and optional <code>-suffix</code>; max 20 chars.
                            </p>
                        </div>
                        <Button onClick={onUpload} disabled={!canUpload}>
                            {upload.isPending ? <Loader2 className="size-4 animate-spin" /> : <Upload className="size-4" />}
                            {upload.isPending ? "Uploading…" : "Upload"}
                        </Button>
                    </div>
                </CardContent>
            </Card>

            {/* Versions */}
            <Card>
                <CardHeader><CardTitle className="text-base">Cached versions</CardTitle></CardHeader>
                <CardContent>
                    {isLoading ? (
                        <Skeleton className="h-40 w-full" />
                    ) : versions.length === 0 ? (
                        <div className="py-10 text-center text-sm text-muted-foreground">
                            No binaries cached yet. Upload one above.
                        </div>
                    ) : (
                        <Table>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>Version</TableHead>
                                    <TableHead>Arch</TableHead>
                                    <TableHead>Size</TableHead>
                                    <TableHead>Checksum</TableHead>
                                    <TableHead className="text-right">Actions</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {versions.map((v) => {
                                    const archs = Object.keys(v.platforms)
                                    const checksum = Object.values(v.platforms)[0]?.checksum ?? ""
                                    return (
                                        <TableRow key={v.version}>
                                            <TableCell className="font-medium">
                                                {v.version}
                                                {v.is_default && <Badge variant="secondary" className="ml-2">default</Badge>}
                                            </TableCell>
                                            <TableCell>
                                                <div className="flex gap-1">
                                                    {archs.map((a) => (
                                                        <Badge key={a} variant="outline" className="gap-1">
                                                            <Cpu className="size-3" />{a}
                                                        </Badge>
                                                    ))}
                                                </div>
                                            </TableCell>
                                            <TableCell className="text-muted-foreground">{formatBytes(platformSize(v))}</TableCell>
                                            <TableCell className="font-mono text-xs text-muted-foreground">{checksum.slice(0, 12) || "—"}</TableCell>
                                            <TableCell className="text-right">
                                                <div className="flex justify-end gap-2">
                                                    <Button size="sm" variant="outline" onClick={() => { setDeployTarget(v.version); setPicked(new Set()) }}>
                                                        <Rocket className="size-4" /> Deploy
                                                    </Button>
                                                    <Button
                                                        size="sm" variant="ghost"
                                                        className="text-status-danger"
                                                        disabled={v.is_default}
                                                        title={v.is_default ? "Change the default version before deleting" : "Delete"}
                                                        onClick={() => setDeleteTarget(v.version)}
                                                    >
                                                        <Trash2 className="size-4" />
                                                    </Button>
                                                </div>
                                            </TableCell>
                                        </TableRow>
                                    )
                                })}
                            </TableBody>
                        </Table>
                    )}
                </CardContent>
            </Card>

            {/* Deploy dialog */}
            <Dialog open={!!deployTarget} onOpenChange={(o) => !o && setDeployTarget(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Deploy {deployTarget}</DialogTitle>
                        <DialogDescription>Pick nodes to update. Each restarts xray and rolls back on failure.</DialogDescription>
                    </DialogHeader>
                    <div className="max-h-72 space-y-1 overflow-y-auto">
                        {(nodes ?? []).map((n) => (
                            <label key={n.id} className="flex cursor-pointer items-center gap-3 rounded-md px-2 py-2 hover:bg-muted">
                                <input type="checkbox" checked={picked.has(n.id)} onChange={() => togglePick(n.id)} />
                                <span className={cn("size-2 rounded-full", n.is_online ? "bg-status-success" : "bg-muted-foreground/40")} />
                                <span className="text-sm">{n.name}</span>
                                <span className="ml-auto text-xs uppercase text-muted-foreground">{n.country_code}</span>
                            </label>
                        ))}
                        {(nodes ?? []).length === 0 && <p className="py-4 text-center text-sm text-muted-foreground">No nodes.</p>}
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setDeployTarget(null)}>Cancel</Button>
                        <Button onClick={onDeploy} disabled={picked.size === 0 || deploy.isPending}>
                            {deploy.isPending ? <Loader2 className="size-4 animate-spin" /> : <Rocket className="size-4" />}
                            Deploy to {picked.size || ""} node{picked.size === 1 ? "" : "s"}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Delete confirm */}
            <Dialog open={!!deleteTarget} onOpenChange={(o) => !o && setDeleteTarget(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Delete {deleteTarget}?</DialogTitle>
                        <DialogDescription>Removes the cached binary from the hub. Nodes already running it are unaffected.</DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setDeleteTarget(null)}>Cancel</Button>
                        <Button
                            variant="destructive"
                            onClick={() => deleteTarget && del.mutate(deleteTarget, { onSuccess: () => setDeleteTarget(null) })}
                            disabled={del.isPending}
                        >
                            {del.isPending ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />} Delete
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    )
}
