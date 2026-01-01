import { useState, useRef, useCallback } from "react"
import { PageHeader } from "@/components/shared/page-header"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Button } from "@/components/ui/button"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Input } from "@/components/ui/input"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { useBackups, useCreateBackup, useDeleteBackup, useRestoreBackup, useRestoreFromExisting } from "@/lib/queries"
import { getBackupDownloadUrl } from "@/lib/admin-api"
import { formatDateTime } from "@/lib/utils"
import { toast } from "sonner"
import { Loader2 } from "lucide-react"
import {
    HiOutlineDatabase,
    HiOutlineDownload,
    HiOutlineTrash,
    HiOutlineUpload,
    HiOutlinePlus,
    HiOutlineArchive,
    HiOutlineExclamation,
    HiOutlineShieldCheck,
    HiOutlineRefresh,
} from "react-icons/hi"

export default function BackupPage() {
    const { data: backups = [], isLoading } = useBackups()
    const createBackup = useCreateBackup()
    const deleteBackup = useDeleteBackup()
    const restoreBackup = useRestoreBackup()
    const restoreFromExisting = useRestoreFromExisting()
    const confirm = useConfirm()

    // Restore dialog state
    const [restoreDialogOpen, setRestoreDialogOpen] = useState(false)
    const [restoreFile, setRestoreFile] = useState<File | null>(null)
    const [confirmText, setConfirmText] = useState("")
    const fileInputRef = useRef<HTMLInputElement>(null)

    // Restore-from-existing dialog state
    const [restoreExistingDialogOpen, setRestoreExistingDialogOpen] = useState(false)
    const [restoreExistingFilename, setRestoreExistingFilename] = useState("")
    const [confirmExistingText, setConfirmExistingText] = useState("")

    // Restart required banner
    const [restartRequired, setRestartRequired] = useState(false)

    // Drag-and-drop state
    const [isDragOver, setIsDragOver] = useState(false)

    const handleFileSelect = useCallback((file: File) => {
        if (!file.name.endsWith(".sql")) {
            toast.error("Only .sql files are accepted")
            return
        }
        setRestoreFile(file)
        setConfirmText("")
        setRestoreDialogOpen(true)
    }, [])

    const handleFileInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0]
        if (file) {
            handleFileSelect(file)
        }
        // Reset so same file can be re-selected
        e.target.value = ""
    }, [handleFileSelect])

    const handleDrop = useCallback((e: React.DragEvent) => {
        e.preventDefault()
        setIsDragOver(false)
        const file = e.dataTransfer.files?.[0]
        if (file) {
            handleFileSelect(file)
        }
    }, [handleFileSelect])

    const handleDragOver = useCallback((e: React.DragEvent) => {
        e.preventDefault()
        setIsDragOver(true)
    }, [])

    const handleDragLeave = useCallback((e: React.DragEvent) => {
        e.preventDefault()
        setIsDragOver(false)
    }, [])

    const handleRestore = useCallback(() => {
        if (!restoreFile) return
        setRestoreDialogOpen(false)
        restoreBackup.mutate(restoreFile, {
            onSuccess: (data) => {
                if (data.requires_restart) {
                    setRestartRequired(true)
                }
            },
            onSettled: () => {
                setRestoreFile(null)
                setConfirmText("")
            },
        })
    }, [restoreFile, restoreBackup])

    const handleRestoreFromExisting = useCallback((filename: string) => {
        setRestoreExistingFilename(filename)
        setConfirmExistingText("")
        setRestoreExistingDialogOpen(true)
    }, [])

    const executeRestoreFromExisting = useCallback(() => {
        if (!restoreExistingFilename) return
        setRestoreExistingDialogOpen(false)
        restoreFromExisting.mutate(restoreExistingFilename, {
            onSuccess: (data) => {
                if (data.requires_restart) {
                    setRestartRequired(true)
                }
            },
            onSettled: () => {
                setRestoreExistingFilename("")
                setConfirmExistingText("")
            },
        })
    }, [restoreExistingFilename, restoreFromExisting])

    const handleDownload = useCallback(async (filename: string) => {
        try {
            const url = getBackupDownloadUrl(filename)
            const response = await fetch(url, { credentials: "include" })
            if (!response.ok) {
                throw new Error(`Download failed: ${response.statusText}`)
            }
            const blob = await response.blob()
            const a = document.createElement("a")
            a.href = URL.createObjectURL(blob)
            a.download = filename
            document.body.appendChild(a)
            a.click()
            document.body.removeChild(a)
            URL.revokeObjectURL(a.href)
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Download failed")
        }
    }, [])

    const handleDelete = useCallback(async (filename: string) => {
        const ok = await confirm({
            title: "Delete Backup",
            description: `Are you sure you want to delete "${filename}"? This cannot be undone.`,
            confirmLabel: "Delete",
            variant: "destructive",
        })
        if (ok) {
            deleteBackup.mutate(filename)
        }
    }, [confirm, deleteBackup])

    const formatFileSize = (bytes: number) => {
        if (restoreFile && bytes === restoreFile.size) {
            const units = ["B", "KB", "MB", "GB"]
            let size = bytes
            let unitIndex = 0
            while (size >= 1024 && unitIndex < units.length - 1) {
                size /= 1024
                unitIndex++
            }
            return `${size.toFixed(1)} ${units[unitIndex]}`
        }
        return ""
    }

    const isBusy = createBackup.isPending || deleteBackup.isPending || restoreBackup.isPending || restoreFromExisting.isPending

    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            {/* Page Header */}
            <PageHeader
                title="Database Backup"
                description="Create backups, download, or restore your database"
            />

            {/* Restart Required Banner */}
            {restartRequired && (
                <Alert variant="destructive">
                    <HiOutlineExclamation className="w-4 h-4" />
                    <AlertDescription>
                        Database restored successfully. <strong>Server restart is required</strong> for
                        changes to take full effect. The server may restart automatically.
                    </AlertDescription>
                </Alert>
            )}

            {/* Top Section: Create + Restore side by side */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                {/* Create Backup Card */}
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <HiOutlineDatabase className="w-5 h-5" />
                            Create Backup
                        </CardTitle>
                        <CardDescription>
                            Create a new database backup (pg_dump)
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <Button
                            onClick={() => createBackup.mutate()}
                            disabled={isBusy}
                            className="w-full"
                        >
                            {createBackup.isPending ? (
                                <>
                                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                                    Creating Backup...
                                </>
                            ) : (
                                <>
                                    <HiOutlinePlus className="w-4 h-4 mr-2" />
                                    Create Backup Now
                                </>
                            )}
                        </Button>
                    </CardContent>
                </Card>

                {/* Restore Card */}
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <HiOutlineUpload className="w-5 h-5" />
                            Restore from Backup
                        </CardTitle>
                        <CardDescription>
                            Upload a .sql backup file to restore
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <input
                            ref={fileInputRef}
                            type="file"
                            accept=".sql"
                            onChange={handleFileInputChange}
                            className="hidden"
                        />
                        <div
                            onClick={() => !isBusy && fileInputRef.current?.click()}
                            onDrop={handleDrop}
                            onDragOver={handleDragOver}
                            onDragLeave={handleDragLeave}
                            className={`
                                border-2 border-dashed rounded-lg p-6 text-center cursor-pointer
                                transition-colors duration-200
                                ${isDragOver
                                    ? "border-primary bg-primary/5"
                                    : "border-muted-foreground/25 hover:border-muted-foreground/50"
                                }
                                ${isBusy ? "opacity-50 pointer-events-none" : ""}
                            `}
                        >
                            <HiOutlineUpload className="w-8 h-8 mx-auto mb-2 text-muted-foreground" />
                            <p className="text-sm text-muted-foreground">
                                Drop a .sql file here or click to browse
                            </p>
                        </div>
                        {(restoreBackup.isPending || restoreFromExisting.isPending) && (
                            <div className="flex items-center gap-2 mt-3 text-sm text-muted-foreground">
                                <Loader2 className="w-4 h-4 animate-spin" />
                                Restoring database...
                            </div>
                        )}
                    </CardContent>
                </Card>
            </div>

            {/* Backup History */}
            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                        <HiOutlineArchive className="w-5 h-5" />
                        Backup History
                    </CardTitle>
                    <CardDescription>
                        {backups.length} backup{backups.length !== 1 ? "s" : ""} available
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    {isLoading ? (
                        <div className="flex items-center justify-center py-12">
                            <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
                        </div>
                    ) : backups.length === 0 ? (
                        <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                            <HiOutlineArchive className="w-12 h-12 mb-3 opacity-50" />
                            <p className="text-sm font-medium">No backups yet</p>
                            <p className="text-xs mt-1">Create your first backup using the button above</p>
                        </div>
                    ) : (
                        <div className="rounded-md border">
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>Filename</TableHead>
                                        <TableHead>Size</TableHead>
                                        <TableHead>Created</TableHead>
                                        <TableHead className="text-right">Actions</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {backups.map((backup) => (
                                        <TableRow key={backup.filename}>
                                            <TableCell className="font-mono text-sm">
                                                {backup.filename}
                                            </TableCell>
                                            <TableCell>{backup.size_human}</TableCell>
                                            <TableCell>{formatDateTime(backup.created_at)}</TableCell>
                                            <TableCell className="text-right">
                                                <div className="flex items-center justify-end gap-1">
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        onClick={() => handleRestoreFromExisting(backup.filename)}
                                                        disabled={isBusy}
                                                        title="Restore this backup"
                                                    >
                                                        <HiOutlineRefresh className="w-4 h-4" />
                                                    </Button>
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        onClick={() => handleDownload(backup.filename)}
                                                        disabled={isBusy}
                                                        title="Download"
                                                    >
                                                        <HiOutlineDownload className="w-4 h-4" />
                                                    </Button>
                                                    <Button
                                                        variant="ghost"
                                                        size="icon"
                                                        onClick={() => handleDelete(backup.filename)}
                                                        disabled={isBusy}
                                                        title="Delete"
                                                        className="text-destructive hover:text-destructive"
                                                    >
                                                        <HiOutlineTrash className="w-4 h-4" />
                                                    </Button>
                                                </div>
                                            </TableCell>
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        </div>
                    )}
                </CardContent>
            </Card>

            {/* Restore Upload Confirmation Dialog */}
            <Dialog open={restoreDialogOpen} onOpenChange={(open) => {
                if (!open) {
                    setRestoreDialogOpen(false)
                    setRestoreFile(null)
                    setConfirmText("")
                }
            }}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <HiOutlineExclamation className="w-5 h-5 text-destructive" />
                            Restore Database
                        </DialogTitle>
                        <DialogDescription>
                            This action is destructive and will replace your current database.
                        </DialogDescription>
                    </DialogHeader>

                    <div className="space-y-4">
                        <Alert variant="destructive">
                            <AlertDescription>
                                This will <strong>REPLACE ALL DATA</strong> in your database with the contents of the backup file.
                            </AlertDescription>
                        </Alert>

                        <Alert>
                            <HiOutlineShieldCheck className="w-4 h-4" />
                            <AlertDescription>
                                An automatic safety backup will be created before restoring.
                            </AlertDescription>
                        </Alert>

                        {restoreFile && (
                            <div className="text-sm space-y-1 p-3 rounded-md bg-muted">
                                <div><span className="text-muted-foreground">File:</span> <span className="font-mono">{restoreFile.name}</span></div>
                                <div><span className="text-muted-foreground">Size:</span> {formatFileSize(restoreFile.size)}</div>
                            </div>
                        )}

                        <div className="space-y-2">
                            <label className="text-sm font-medium">
                                Type <span className="font-mono font-bold">RESTORE</span> to confirm
                            </label>
                            <Input
                                value={confirmText}
                                onChange={(e) => setConfirmText(e.target.value)}
                                placeholder="RESTORE"
                                autoComplete="off"
                            />
                        </div>
                    </div>

                    <DialogFooter className="gap-2 sm:gap-0">
                        <Button
                            variant="outline"
                            onClick={() => {
                                setRestoreDialogOpen(false)
                                setRestoreFile(null)
                                setConfirmText("")
                            }}
                        >
                            Cancel
                        </Button>
                        <Button
                            variant="destructive"
                            onClick={handleRestore}
                            disabled={confirmText !== "RESTORE"}
                        >
                            Restore Database
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Restore from Existing Confirmation Dialog */}
            <Dialog open={restoreExistingDialogOpen} onOpenChange={(open) => {
                if (!open) {
                    setRestoreExistingDialogOpen(false)
                    setRestoreExistingFilename("")
                    setConfirmExistingText("")
                }
            }}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <HiOutlineExclamation className="w-5 h-5 text-destructive" />
                            Restore Database
                        </DialogTitle>
                        <DialogDescription>
                            This action is destructive and will replace your current database.
                        </DialogDescription>
                    </DialogHeader>

                    <div className="space-y-4">
                        <Alert variant="destructive">
                            <AlertDescription>
                                This will <strong>REPLACE ALL DATA</strong> in your database with the contents of the backup file.
                            </AlertDescription>
                        </Alert>

                        <Alert>
                            <HiOutlineShieldCheck className="w-4 h-4" />
                            <AlertDescription>
                                An automatic safety backup will be created before restoring.
                            </AlertDescription>
                        </Alert>

                        {restoreExistingFilename && (
                            <div className="text-sm space-y-1 p-3 rounded-md bg-muted">
                                <div><span className="text-muted-foreground">File:</span> <span className="font-mono">{restoreExistingFilename}</span></div>
                            </div>
                        )}

                        <div className="space-y-2">
                            <label className="text-sm font-medium">
                                Type <span className="font-mono font-bold">RESTORE</span> to confirm
                            </label>
                            <Input
                                value={confirmExistingText}
                                onChange={(e) => setConfirmExistingText(e.target.value)}
                                placeholder="RESTORE"
                                autoComplete="off"
                            />
                        </div>
                    </div>

                    <DialogFooter className="gap-2 sm:gap-0">
                        <Button
                            variant="outline"
                            onClick={() => {
                                setRestoreExistingDialogOpen(false)
                                setRestoreExistingFilename("")
                                setConfirmExistingText("")
                            }}
                        >
                            Cancel
                        </Button>
                        <Button
                            variant="destructive"
                            onClick={executeRestoreFromExisting}
                            disabled={confirmExistingText !== "RESTORE"}
                        >
                            Restore Database
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    )
}
