import { useState, useEffect, useCallback, lazy, Suspense } from "react"

const Editor = lazy(async () => {
    const monaco = await import("monaco-editor")
    const { default: EditorComponent, loader } = await import("@monaco-editor/react")
    loader.config({ monaco })
    return { default: EditorComponent }
})
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { copyToClipboard } from "@/lib/utils"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
    HiOutlineSave,
    HiOutlineClipboard,
    HiOutlineCheckCircle,
    HiOutlineExclamationCircle,
    HiOutlineRefresh,
} from "react-icons/hi"
import { Loader2, GitCompare } from "lucide-react"
import { XrayConfigDiffDialog } from "@/components/node/xray-config-diff-dialog"
import { toast } from "sonner"
import { getXrayConfig, updateXrayConfig, validateXrayConfig } from "@/lib/admin-api"
import { useTheme } from "@/components/providers/theme-provider"

interface NodeXrayConfigEditorProps {
    nodeId: number
    isOnline: boolean
}

export function NodeXrayConfigEditor({ nodeId, isOnline }: NodeXrayConfigEditorProps) {
    const { resolvedTheme } = useTheme()
    const [content, setContent] = useState("")
    const [originalContent, setOriginalContent] = useState("") // For modification check
    const [isLoading, setIsLoading] = useState(false)
    const [isSaving, setIsSaving] = useState(false)
    const [isValidating, setIsValidating] = useState(false)
    const [validationResult, setValidationResult] = useState<{
        valid: boolean
        errors: string[]
        warnings: string[]
    } | null>(null)
    const [diffOpen, setDiffOpen] = useState(false)

    const fetchConfig = useCallback(async () => {
        if (!isOnline) return

        try {
            setIsLoading(true)
            const res = await getXrayConfig(nodeId)
            if (res.success && res.data) {
                // Prettify JSON if possible
                try {
                    const parsed = JSON.parse(res.data)
                    const formatted = JSON.stringify(parsed, null, 2)
                    setContent(formatted)
                    setOriginalContent(formatted)
                } catch {
                    setContent(res.data)
                    setOriginalContent(res.data)
                }
                setValidationResult(null)
            } else {
                toast.error(res.error || "Failed to load config")
            }
        } catch {
            toast.error("Failed to load config")
        } finally {
            setIsLoading(false)
        }
    }, [nodeId, isOnline])

    useEffect(() => {
        fetchConfig()
    }, [fetchConfig])

    const handleCopy = async () => {
        await copyToClipboard(content)
        toast.success("Config copied to clipboard")
    }

    const handleValidate = async () => {
        try {
            setIsValidating(true)
            const res = await validateXrayConfig(nodeId, content)
            if (res.success && res.data) {
                setValidationResult(res.data)
                if (res.data.valid) {
                    toast.success("Configuration is valid")
                } else {
                    toast.error("Configuration has errors")
                }
            } else {
                toast.error(res.error || "Validation failed")
            }
        } catch {
            toast.error("Validation failed")
        } finally {
            setIsValidating(false)
        }
    }

    const handleSave = async () => {
        // Validation first (implicit or explicit) - let's validate first
        try {
            setIsValidating(true)
            const valRes = await validateXrayConfig(nodeId, content)

            if (!valRes.success) {
                toast.error("Validation check failed")
                setIsValidating(false)
                return
            }

            if (!valRes.data?.valid) {
                setValidationResult(valRes.data || null)
                toast.error("Cannot save: Configuration is invalid")
                setIsValidating(false)
                return
            }
            setIsValidating(false)

            // Proceed to save
            setIsSaving(true)
            const saveRes = await updateXrayConfig(nodeId, content)
            if (saveRes.success) {
                toast.success("Configuration saved and pushed to node")
                setOriginalContent(content)
                setValidationResult(null)
            } else {
                toast.error(saveRes.error || "Failed to save config")
            }
        } catch {
            toast.error("Failed to save config")
        } finally {
            setIsSaving(false)
            setIsValidating(false)
        }
    }

    if (!isOnline) {
        return (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground border rounded-xl bg-muted/10 border-dashed">
                <HiOutlineExclamationCircle className="w-12 h-12 mb-4 opacity-50" />
                <h3 className="text-lg font-medium text-foreground">Node is Offline</h3>
                <p>Start the node agent to edit configuration.</p>
            </div>
        )
    }

    const isModified = content !== originalContent

    return (
        <div className="space-y-4 h-[calc(100vh-300px)] min-h-[500px] flex flex-col">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-4">
                    <div>
                        <h3 className="text-lg font-semibold flex items-center gap-2">
                            Xray Configuration
                            {isModified && <Badge variant="warning" className="text-[10px] px-1 py-0 h-5">Modified</Badge>}
                        </h3>
                        <p className="text-sm text-muted-foreground">Directly edit the running Xray configuration JSON</p>
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    <Button variant="outline" size="sm" onClick={fetchConfig} disabled={isLoading || isSaving}>
                        <HiOutlineRefresh className={`w-4 h-4 mr-2 ${isLoading ? 'animate-spin' : ''}`} />
                        Reload
                    </Button>
                    <Button variant="outline" size="sm" onClick={handleCopy}>
                        <HiOutlineClipboard className="w-4 h-4 mr-2" />
                        Copy
                    </Button>
                    <Button variant="outline" size="sm" onClick={() => setDiffOpen(true)} disabled={isLoading}>
                        <GitCompare className="w-4 h-4 mr-2" />
                        Diff
                    </Button>
                    <Button
                        variant="secondary"
                        size="sm"
                        onClick={handleValidate}
                        disabled={isLoading || isSaving || isValidating}
                    >
                        {isValidating ? (
                            <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        ) : (
                            <HiOutlineCheckCircle className="w-4 h-4 mr-2" />
                        )}
                        Validate
                    </Button>
                    <Button
                        size="sm"
                        onClick={handleSave}
                        disabled={isLoading || isSaving || isValidating || !isModified}
                    >
                        {isSaving ? (
                            <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        ) : (
                            <HiOutlineSave className="w-4 h-4 mr-2" />
                        )}
                        Save & Apply
                    </Button>
                </div>
            </div>

            {/* Validation Feedback */}
            {validationResult && !validationResult.valid && (
                <Alert variant="destructive" className="animate-in fade-in slide-in-from-top-2">
                    <HiOutlineExclamationCircle className="h-4 w-4" />
                    <AlertTitle>Configuration Error</AlertTitle>
                    <AlertDescription>
                        <ul className="list-disc pl-5 mt-2 space-y-1">
                            {validationResult.errors.map((err, i) => (
                                <li key={i} className="text-xs font-mono">{err}</li>
                            ))}
                        </ul>
                    </AlertDescription>
                </Alert>
            )}

            {validationResult && validationResult.valid && (
                <Alert className="border-green-500/20 bg-green-500/10 text-green-600 animate-in fade-in slide-in-from-top-2">
                    <HiOutlineCheckCircle className="h-4 w-4" />
                    <AlertTitle>Valid Configuration</AlertTitle>
                    <AlertDescription className="text-xs">
                        The configuration syntax is correct.
                    </AlertDescription>
                </Alert>
            )}

            <XrayConfigDiffDialog
                nodeId={nodeId}
                open={diffOpen}
                onOpenChange={setDiffOpen}
            />

            <Card className="flex-1 overflow-hidden border-0 shadow-none bg-card/50 backdrop-blur-sm border-white/5">
                {isLoading ? (
                    <div className="h-full flex items-center justify-center">
                        <Loader2 className="w-8 h-8 animate-spin text-muted-foreground" />
                    </div>
                ) : (
                    <Suspense fallback={<div className="h-full flex items-center justify-center text-muted-foreground">Loading editor...</div>}>
                        <Editor
                            height="100%"
                            defaultLanguage="json"
                            language="json"
                            theme={resolvedTheme === "dark" ? "vs-dark" : "light"}
                            value={content}
                            onChange={(val) => setContent(val || "")}
                            options={{
                                minimap: { enabled: false },
                                fontSize: 14,
                                scrollBeyondLastLine: false,
                                wordWrap: "on",
                                automaticLayout: true,
                                tabSize: 2,
                                formatOnPaste: true,
                                formatOnType: true,
                            }}
                        />
                    </Suspense>
                )}
            </Card>
        </div>
    )
}
