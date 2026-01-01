import { useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { HiOutlineDownload, HiOutlineUpload } from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { useExportSettings, useImportSettings } from "@/lib/queries"
import type { Setting } from "@/lib/domain/setting"
import { toast } from "sonner"

interface SettingsImportExportProps {
    disabled?: boolean
    /** When true, render Export/Import as <DropdownMenuItem> for use inside a kebab menu. */
    asMenuItem?: boolean
}

export function SettingsImportExport({ disabled, asMenuItem = false }: SettingsImportExportProps) {
    const exportSettings = useExportSettings()
    const importSettings = useImportSettings()
    const fileInputRef = useRef<HTMLInputElement>(null)
    const [previewOpen, setPreviewOpen] = useState(false)
    const [pendingImport, setPendingImport] = useState<Setting[]>([])

    const handleExport = () => {
        exportSettings.mutate()
    }

    const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0]
        if (!file) return

        const reader = new FileReader()
        reader.onload = (event) => {
            try {
                const data = JSON.parse(event.target?.result as string)
                if (!Array.isArray(data)) {
                    toast.error("Invalid file", { description: "Expected a JSON array of settings." })
                    return
                }
                // Basic validation
                const valid = data.every((s: Setting) => s.key && typeof s.value === "string")
                if (!valid) {
                    toast.error("Invalid format", { description: "Each setting must have a key and value." })
                    return
                }
                setPendingImport(data)
                setPreviewOpen(true)
            } catch {
                toast.error("Parse error", { description: "Could not parse the JSON file." })
            }
        }
        reader.readAsText(file)

        // Reset so the same file can be selected again
        if (fileInputRef.current) fileInputRef.current.value = ""
    }

    const handleConfirmImport = () => {
        importSettings.mutate(pendingImport, {
            onSuccess: () => {
                setPreviewOpen(false)
                setPendingImport([])
            },
        })
    }

    return (
        <>
            {asMenuItem ? (
                <>
                    <DropdownMenuItem
                        onSelect={(e) => {
                            e.preventDefault()
                            handleExport()
                        }}
                        disabled={disabled || exportSettings.isPending}
                    >
                        {exportSettings.isPending ? (
                            <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        ) : (
                            <HiOutlineDownload className="w-4 h-4 mr-2" />
                        )}
                        Export
                    </DropdownMenuItem>

                    <DropdownMenuItem
                        onSelect={(e) => {
                            e.preventDefault()
                            fileInputRef.current?.click()
                        }}
                        disabled={disabled}
                    >
                        <HiOutlineUpload className="w-4 h-4 mr-2" />
                        Import
                    </DropdownMenuItem>
                </>
            ) : (
                <>
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={handleExport}
                        disabled={disabled || exportSettings.isPending}
                    >
                        {exportSettings.isPending ? (
                            <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        ) : (
                            <HiOutlineDownload className="w-4 h-4 mr-2" />
                        )}
                        Export
                    </Button>

                    <Button
                        variant="outline"
                        size="sm"
                        onClick={() => fileInputRef.current?.click()}
                        disabled={disabled}
                    >
                        <HiOutlineUpload className="w-4 h-4 mr-2" />
                        Import
                    </Button>
                </>
            )}

            <input
                ref={fileInputRef}
                type="file"
                accept=".json"
                className="hidden"
                onChange={handleFileSelect}
            />

            {/* Import preview dialog */}
            <Dialog open={previewOpen} onOpenChange={(open) => {
                if (!importSettings.isPending) {
                    setPreviewOpen(open)
                    if (!open) setPendingImport([])
                }
            }}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle>Import Settings</DialogTitle>
                        <DialogDescription>
                            Review the settings to import. This will overwrite existing values.
                        </DialogDescription>
                    </DialogHeader>

                    <div className="rounded-md border p-3 max-h-64 overflow-y-auto">
                        <table className="w-full text-sm">
                            <thead>
                                <tr className="border-b text-left">
                                    <th className="pb-1.5 text-muted-foreground font-medium">Key</th>
                                    <th className="pb-1.5 text-muted-foreground font-medium">Category</th>
                                </tr>
                            </thead>
                            <tbody>
                                {pendingImport.map((s) => (
                                    <tr key={s.key} className="border-b last:border-0">
                                        <td className="py-1.5 font-mono text-xs">{s.key}</td>
                                        <td className="py-1.5 text-muted-foreground">{s.category}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>

                    <p className="text-xs text-muted-foreground">
                        {pendingImport.length} settings will be imported.
                    </p>

                    <DialogFooter>
                        <Button
                            variant="outline"
                            onClick={() => { setPreviewOpen(false); setPendingImport([]) }}
                            disabled={importSettings.isPending}
                        >
                            Cancel
                        </Button>
                        <Button
                            onClick={handleConfirmImport}
                            disabled={importSettings.isPending}
                        >
                            {importSettings.isPending ? (
                                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                            ) : (
                                <HiOutlineUpload className="w-4 h-4 mr-2" />
                            )}
                            Import {pendingImport.length} Settings
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    )
}
