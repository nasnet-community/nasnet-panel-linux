import { lazy, Suspense, useMemo, useState } from "react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Label } from "@/components/ui/label"
import { Loader2, AlertTriangle, Upload, RefreshCw, Info } from "lucide-react"
import { useTheme } from "@/components/providers/theme-provider"
import { useNodeXrayConfigDiff } from "@/lib/queries"
import { useQueryClient } from "@tanstack/react-query"
import { queryKeys } from "@/lib/queries/keys"
import { pushNodeConfig } from "@/lib/admin-api"
import { toast } from "sonner"

const DiffEditor = lazy(async () => {
    const monaco = await import("monaco-editor")
    const { DiffEditor, loader } = await import("@monaco-editor/react")
    loader.config({ monaco })
    return { default: DiffEditor }
})

interface XrayConfigDiffDialogProps {
    nodeId: number
    open: boolean
    onOpenChange: (open: boolean) => void
}

// stripCertContent replaces PEM-looking blocks inside certificateFile/keyFile
// with a placeholder. Cuts diff noise when one side inlines cert bytes and
// the other side references a file path (stealth vs non-stealth nodes).
function stripCertContent(jsonStr: string): string {
    if (!jsonStr) return jsonStr
    return jsonStr.replace(
        /("(?:certificateFile|keyFile)"\s*:\s*)"(-----BEGIN [^"]*-----[\s\S]*?-----END [^"]*-----\\n?)"/g,
        '$1"«inline PEM omitted»"',
    )
}

export function XrayConfigDiffDialog({ nodeId, open, onOpenChange }: XrayConfigDiffDialogProps) {
    const { resolvedTheme } = useTheme()
    const queryClient = useQueryClient()
    const { data, isLoading, isFetching, error, refetch } = useNodeXrayConfigDiff(nodeId, open)
    const [hidePEM, setHidePEM] = useState(true)
    const [isPushing, setIsPushing] = useState(false)

    const [original, modified] = useMemo(() => {
        const o = data?.running ?? ""
        const m = data?.generated ?? ""
        if (hidePEM) return [stripCertContent(o), stripCertContent(m)]
        return [o, m]
    }, [data, hidePEM])

    const handlePush = async () => {
        setIsPushing(true)
        try {
            const res = await pushNodeConfig(nodeId)
            if (res.success) {
                toast.success("Config pushed")
                await refetch()
                queryClient.invalidateQueries({ queryKey: queryKeys.nodes })
            } else {
                toast.error(res.error || "Push failed")
            }
        } catch (e: any) {
            toast.error(e?.message || "Push failed")
        } finally {
            setIsPushing(false)
        }
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-[min(1400px,95vw)] max-h-[90vh] flex flex-col p-0 gap-0">
                <DialogHeader className="px-6 pt-6 pb-4 border-b">
                    <div className="flex items-start justify-between gap-4">
                        <div className="space-y-1">
                            <DialogTitle className="flex items-center gap-2">
                                Config Diff
                                {data && (
                                    <Badge variant={data.differs ? "warning" : "success"} className="text-[10px]">
                                        {data.differs ? "Changes pending" : "In sync"}
                                    </Badge>
                                )}
                            </DialogTitle>
                            <DialogDescription>
                                Running on the agent <span className="text-foreground font-medium">(left)</span> vs what the panel would push next <span className="text-foreground font-medium">(right)</span>.
                            </DialogDescription>
                        </div>
                        <div className="flex items-center gap-2 shrink-0">
                            <div className="flex items-center gap-2 px-3 py-1.5 border rounded-md">
                                <Switch id="hide-pem" checked={hidePEM} onCheckedChange={setHidePEM} />
                                <Label htmlFor="hide-pem" className="text-xs cursor-pointer">Hide PEM blocks</Label>
                            </div>
                            <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
                                <RefreshCw className={isFetching ? "w-4 h-4 mr-1.5 animate-spin" : "w-4 h-4 mr-1.5"} />
                                Reload
                            </Button>
                            <Button
                                size="sm"
                                onClick={handlePush}
                                disabled={isPushing || isLoading || !data?.differs}
                                title={data?.differs ? "Push the generated config to the agent" : "No changes to push"}
                            >
                                {isPushing ? <Loader2 className="w-4 h-4 mr-1.5 animate-spin" /> : <Upload className="w-4 h-4 mr-1.5" />}
                                Push Config
                            </Button>
                        </div>
                    </div>
                </DialogHeader>

                <div className="flex-1 overflow-hidden flex flex-col">
                    {data?.running_error && (
                        <Alert variant="destructive" className="mx-6 mt-4">
                            <AlertTriangle className="h-4 w-4" />
                            <AlertTitle>Could not fetch running config</AlertTitle>
                            <AlertDescription className="text-xs font-mono">{data.running_error}</AlertDescription>
                        </Alert>
                    )}

                    {error && (
                        <Alert variant="destructive" className="mx-6 mt-4">
                            <AlertTriangle className="h-4 w-4" />
                            <AlertTitle>Failed to load diff</AlertTitle>
                            <AlertDescription>{(error as Error).message}</AlertDescription>
                        </Alert>
                    )}

                    {data && !data.differs && !data.running_error && (
                        <Alert className="mx-6 mt-4 border-emerald-500/20 bg-emerald-500/5">
                            <Info className="h-4 w-4" />
                            <AlertTitle>No changes</AlertTitle>
                            <AlertDescription className="text-xs">
                                The agent's running config matches what would be generated from the current DB state.
                            </AlertDescription>
                        </Alert>
                    )}

                    <div className="flex-1 min-h-[500px] px-2 py-3">
                        {isLoading ? (
                            <div className="h-full flex items-center justify-center">
                                <Loader2 className="w-8 h-8 animate-spin text-muted-foreground" />
                            </div>
                        ) : (
                            <Suspense fallback={<div className="h-full flex items-center justify-center text-muted-foreground">Loading diff editor…</div>}>
                                <DiffEditor
                                    height="100%"
                                    language="json"
                                    originalLanguage="json"
                                    modifiedLanguage="json"
                                    theme={resolvedTheme === "dark" ? "vs-dark" : "light"}
                                    original={original}
                                    modified={modified}
                                    options={{
                                        readOnly: true,
                                        renderSideBySide: true,
                                        minimap: { enabled: false },
                                        fontSize: 13,
                                        wordWrap: "on",
                                        automaticLayout: true,
                                        scrollBeyondLastLine: false,
                                        renderWhitespace: "none",
                                    }}
                                />
                            </Suspense>
                        )}
                    </div>
                </div>
            </DialogContent>
        </Dialog>
    )
}
