import { useState, useRef, useEffect } from "react"
import { Upload, Loader2, AlertTriangle, Package, CheckCircle2, XCircle, Circle } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { toast } from "@/lib/toast"
import { cn } from "@/lib/utils"
import { getApiBaseUrl } from '@/lib/config'

interface UpdateAgentDialogProps {
    nodeId: number
    nodeName: string
    currentVersion?: string
    open: boolean
    onOpenChange: (open: boolean) => void
    onSuccess?: () => void
}

type UpdateStep = "idle" | "init" | "connect" | "check_arch" | "prep_binary" | "upload" | "install" | "verify" | "complete"

interface UpdateProgress {
    step: UpdateStep
    message: string
    status: "pending" | "running" | "success" | "error"
    error?: string
}

export function UpdateAgentDialog({
    nodeId,
    nodeName,
    currentVersion,
    open,
    onOpenChange,
    onSuccess,
}: UpdateAgentDialogProps) {
    const [status, setStatus] = useState<UpdateStep>("idle")
    const [progress, setProgress] = useState<UpdateProgress[]>([])
    const [error, setError] = useState<string | null>(null)
    const eventSourceRef = useRef<EventSource | null>(null)

    // Cleanup on unmount or close
    useEffect(() => {
        if (!open) {
            closeEventSource()
            // Reset state after a delay or immediately? 
            // Better to keep state unless closed for a while, but for now reset on close
            if (status === "complete" || status === "idle") {
                setTimeout(() => {
                    setStatus("idle")
                    setProgress([])
                    setError(null)
                }, 500)
            }
        }
        return () => closeEventSource()
    }, [open])

    const closeEventSource = () => {
        if (eventSourceRef.current) {
            eventSourceRef.current.close()
            eventSourceRef.current = null
        }
    }

    const startUpdate = () => {
        setStatus("init")
        setError(null)
        setProgress([])

        // fetch + ReadableStream so we can pass Authorization header (EventSource can't).
        fetchStream()
    }

    const fetchStream = async () => {
        try {
            const token = localStorage.getItem("token")
            const headers: HeadersInit = {}
            if (token) {
                headers["Authorization"] = `Bearer ${token}`
            }

            const response = await fetch(`${getApiBaseUrl()}/api/v1/nodes/${nodeId}/agent/update/stream`, {
                headers,
                credentials: "include",
            })

            if (!response.ok) {
                throw new Error(await response.text() || "Failed to start update stream")
            }

            const reader = response.body?.getReader()
            if (!reader) throw new Error("No response body")

            const decoder = new TextDecoder()
            let buffer = ""

            // Initial generic steps
            const steps: UpdateProgress[] = [
                { step: "init", message: "Initializing...", status: "pending" },
                { step: "connect", message: "Connecting to agent...", status: "pending" },
                { step: "check_arch", message: "Checking system...", status: "pending" },
                { step: "prep_binary", message: "Preparing binary...", status: "pending" },
                { step: "upload", message: "Uploading...", status: "pending" },
                { step: "install", message: "Installing...", status: "pending" },
            ]
            setProgress(steps)

            while (true) {
                const { done, value } = await reader.read()
                if (done) break

                buffer += decoder.decode(value, { stream: true })
                const lines = buffer.split("\n\n")
                buffer = lines.pop() || ""

                for (const line of lines) {
                    if (line.startsWith("data: ")) {
                        const data = JSON.parse(line.slice(6)) as UpdateProgress
                        handleProgressUpdate(data)
                    }
                }
            }
        } catch (err) {
            setError(err instanceof Error ? err.message : "Stream connection failed")
            // Mark current step as error
            setProgress(prev => prev.map(p =>
                p.status === "running" ? { ...p, status: "error", error: "Connection failed" } : p
            ))
        }
    }

    const handleProgressUpdate = (data: UpdateProgress) => {
        setStatus(data.step as UpdateStep)

        if (data.status === "error") {
            setError(data.error || "Unknown error")
        }

        if (data.step === "complete") {
            toast.success("Agent update completed successfully")
            onSuccess?.()
        }

        setProgress(prev => {
            const newProgress = [...prev]
            const existingIndex = newProgress.findIndex(p => p.step === data.step)

            // Mark previous steps as success if we moved to a new step
            // Simple heuristic based on known order
            const stepOrder: UpdateStep[] = ["init", "connect", "check_arch", "prep_binary", "upload", "install", "verify", "complete"]
            const currentIdx = stepOrder.indexOf(data.step as UpdateStep)

            if (currentIdx > 0) {
                for (let i = 0; i < currentIdx; i++) {
                    const prevStepKey = stepOrder[i]
                    const idx = newProgress.findIndex(p => p.step === prevStepKey)
                    if (idx !== -1 && newProgress[idx].status !== "success") {
                        newProgress[idx] = { ...newProgress[idx], status: "success" }
                    }
                }
            }

            if (existingIndex !== -1) {
                newProgress[existingIndex] = { ...newProgress[existingIndex], ...data }
            } else if (data.step !== "complete") {
                // Add if not exists (dynamic steps)
                newProgress.push(data)
            }

            return newProgress
        })
    }

    const renderStepIcon = (p: UpdateProgress) => {
        if (p.status === "error") return <XCircle className="h-5 w-5 text-red-500" />
        if (p.status === "success") return <CheckCircle2 className="h-5 w-5 text-green-500" />
        if (p.status === "running") return <Loader2 className="h-5 w-5 text-blue-500 animate-spin" />
        return <Circle className="h-5 w-5 text-muted-foreground/30" />
    }

    return (
        <Dialog open={open} onOpenChange={(val) => {
            if (!val && status !== "idle" && status !== "complete" && !error) {
                // Prevent closing while updating
                return
            }
            onOpenChange(val)
        }}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <Package className="h-5 w-5" />
                        Update Agent
                    </DialogTitle>
                    <DialogDescription>
                        {status === "idle" ? (
                            <span>Update the agent binary on <strong>{nodeName}</strong> to the latest version.</span>
                        ) : (
                            <span>Updating agent on <strong>{nodeName}</strong>...</span>
                        )}
                    </DialogDescription>
                </DialogHeader>

                <div className="py-4">
                    {status === "idle" ? (
                        <div className="space-y-4">
                            {currentVersion && (
                                <div className="flex items-center justify-between text-sm">
                                    <span className="text-muted-foreground">Current Version:</span>
                                    <span className="font-mono text-foreground">{currentVersion}</span>
                                </div>
                            )}

                            <div className="flex items-start gap-2 p-3 rounded-md bg-blue-500/10 border border-blue-500/20">
                                <Package className="h-5 w-5 text-blue-500 shrink-0 mt-0.5" />
                                <div className="text-sm text-blue-500">
                                    <p className="font-medium">Automated Update</p>
                                    <p className="text-blue-500/80">
                                        The system will automatically detect the node's architecture and push the latest compatible binary from the server.
                                    </p>
                                </div>
                            </div>

                            <div className="flex items-start gap-2 p-3 rounded-md bg-amber-500/10 border border-amber-500/20">
                                <AlertTriangle className="h-5 w-5 text-amber-500 shrink-0 mt-0.5" />
                                <div className="text-sm text-amber-500">
                                    <p className="font-medium">Agent will restart</p>
                                    <p className="text-amber-500/80">
                                        The agent service will automatically restart after the update.
                                    </p>
                                </div>
                            </div>
                        </div>
                    ) : (
                        <div className="space-y-4">
                            {error && (
                                <div className="p-3 rounded-md bg-red-500/10 border border-red-500/20 text-red-500 text-sm flex items-start gap-2">
                                    <AlertTriangle className="h-5 w-5 shrink-0" />
                                    <div>
                                        <p className="font-medium">Update Failed</p>
                                        <p>{error}</p>
                                    </div>
                                </div>
                            )}

                            <div className="space-y-1">
                                {progress.map((p) => (
                                    <div key={p.step} className="flex items-center gap-3 p-2 rounded-lg transition-colors">
                                        {renderStepIcon(p)}
                                        <div className="flex-1">
                                            <p className={cn("text-sm font-medium",
                                                p.status === "pending" && "text-muted-foreground",
                                                p.status === "running" && "text-blue-600",
                                                p.status === "success" && "text-green-600",
                                                p.status === "error" && "text-red-600"
                                            )}>
                                                {p.message}
                                            </p>
                                        </div>
                                    </div>
                                ))}
                            </div>

                            {status === "complete" && (
                                <div className="flex flex-col items-center justify-center p-4 text-center space-y-2 animate-in fade-in zoom-in duration-300">
                                    <div className="h-12 w-12 rounded-full bg-green-100 flex items-center justify-center">
                                        <CheckCircle2 className="h-6 w-6 text-green-600" />
                                    </div>
                                    <h3 className="font-medium text-lg">Update Successful</h3>
                                    <p className="text-sm text-muted-foreground">
                                        The agent has been updated and restarted.
                                    </p>
                                </div>
                            )}
                        </div>
                    )}
                </div>

                <DialogFooter>
                    {status === "idle" ? (
                        <>
                            <Button variant="outline" onClick={() => onOpenChange(false)}>
                                Cancel
                            </Button>
                            <Button onClick={startUpdate}>
                                <Upload className="mr-2 h-4 w-4" />
                                Update Now
                            </Button>
                        </>
                    ) : (
                        <Button
                            onClick={() => onOpenChange(false)}
                            variant={status === "complete" ? "default" : "outline"}
                            disabled={status !== "complete" && !error}
                        >
                            {status === "complete" ? "Done" : "Close"}
                        </Button>
                    )}
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
