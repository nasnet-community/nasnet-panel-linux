import { useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { Button } from "@/components/ui/button"
import { Form } from "@/components/ui/form"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { toast } from "sonner"
import { deleteNode } from "@/lib/api/nodes"
import { useNodeSettingsForm } from "@/hooks/use-node-settings-form"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { useUnsavedChanges } from "@/hooks/use-unsaved-changes"
import { SSHSettingsCard } from "./ssh-settings-card"
import { NodeSettingsStatusBar } from "./settings/node-settings-status-bar"
import { NodeSettingsGeneral } from "./settings/node-settings-general"
import { NodeSettingsXray } from "./settings/node-settings-xray"
import { NodeSettingsStarlink } from "./settings/node-settings-starlink"
import { NodeSettingsCrashRecovery } from "./settings/node-settings-crash-recovery"
import { NodeSettingsActions } from "./settings/node-settings-actions"
import { NodeSettingsSaveBar } from "./settings/node-settings-save-bar"
import { settingsFields, settingsSections, type SettingsSection } from "./settings/settings-sections"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { Check, Loader2, SlidersHorizontal, Plug, ScrollText, Workflow, TriangleAlert, ArrowRight } from "lucide-react"
import { DangerZone } from "@/components/node/danger-zone"
import type { Node } from "@/lib/types"

interface NodeSettingsProps {
    node: Node
    onRefresh: () => void
    onDirtyChange?: (dirty: boolean) => void
}

export function NodeSettings({ node, onRefresh, onDirtyChange }: NodeSettingsProps) {
    const navigate = useNavigate()
    const confirmDelete = useConfirm()
    const settingsForm = useNodeSettingsForm(node, { onRefresh })
    const unsaved = useUnsavedChanges(settingsForm.isDirty)
    const [forceDelete, setForceDelete] = useState(false)
    const [section, setSection] = useState<SettingsSection>("general")
    const [reviewOpen, setReviewOpen] = useState(false)
    const icons = [SlidersHorizontal, Plug, ScrollText, Workflow, TriangleAlert]
    useEffect(() => { if (reviewOpen && !settingsForm.isDirty && !settingsForm.isSaving) setReviewOpen(false) }, [reviewOpen, settingsForm.isDirty, settingsForm.isSaving])
    const isStealth = settingsForm.form.watch("is_stealth")
    useEffect(() => { onDirtyChange?.(settingsForm.isDirty) }, [settingsForm.isDirty, onDirtyChange])
    useEffect(() => () => onDirtyChange?.(false), [onDirtyChange])
    const handleSave = async () => {
        const invalidField = await settingsForm.save()
        if (invalidField) {
            setReviewOpen(false)
            setSection(settingsFields[invalidField].section)
            requestAnimationFrame(() => settingsForm.form.setFocus(invalidField))
        } else if (!settingsForm.form.formState.isDirty) setReviewOpen(false)
    }
    const formatValue = (value: unknown) => typeof value === "boolean" ? value ? "Enabled" : "Disabled" : String(value ?? "") || "Not set"

    const handleDelete = async () => {
        if (!await confirmDelete({
            title: forceDelete ? "Force delete node?" : "Delete node?",
            description: `Remove ${node.name} from the panel and attempt to uninstall its local agent and Xray.${forceDelete ? " Linked accounts and configuration will also be deleted." : " Servers with linked accounts or inbounds require force deletion."}`,
            variant: "destructive", typeToConfirm: node.name, confirmLabel: forceDelete ? "Force delete" : "Delete node",
        })) return
        try {
            const res = await deleteNode(node.id, forceDelete)
            if (res.success) {
                toast.success("Node deleted")
                navigate("/server")
            } else {
                if (res.error?.includes("has active")) {
                    setForceDelete(true)
                    toast.error("Node has active accounts. Enable force delete to proceed.")
                } else {
                    toast.error(res.error || "Failed to delete node")
                }
            }
        } catch {
            toast.error("Failed to delete node")
        }
    }

    return (
        <Form {...settingsForm.form}>
            <div className="relative min-w-0 space-y-5 pb-4">
                <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-3">
                    <div className="space-y-1.5">
                        <h2 className="text-lg font-semibold tracking-tight">Server settings</h2>
                        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                            <Badge variant={node.is_online ? "success" : "secondary"} className="gap-1.5"><span className={cn("size-1.5 rounded-full", node.is_online ? "bg-emerald-500" : "bg-muted-foreground")} />{node.is_online ? "Server online" : "Server offline"}</Badge>
                            <span className="capitalize">Local server</span>
                            <span aria-hidden="true" className="hidden sm:inline">·</span><span className="hidden font-mono sm:inline">Server #{node.id}</span>
                        </div>
                    </div>
                    <div className="flex items-center gap-2">
                    <Button size="sm" onClick={() => void handleSave()} disabled={!settingsForm.isDirty || settingsForm.isSaving}>
                        {settingsForm.isSaving ? <Loader2 className="size-3.5 animate-spin" /> : <Check className="size-3.5" />}
                        {settingsForm.isSaving ? "Saving…" : "Save changes"}
                    </Button>
                    <NodeSettingsActions node={node} settingsForm={settingsForm} onRefresh={onRefresh} />
                    </div>
                </div>

                <select aria-label="Settings category" value={section} onChange={event => setSection(event.target.value as SettingsSection)} className="h-11 w-full rounded-lg border border-border bg-card px-3 text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring sm:hidden">
                    {settingsSections.map(item => {
                        const count = settingsForm.dirtyFields.filter(f => settingsFields[f].section === item.id).length
                        return <option key={item.id} value={item.id}>{item.label}{count ? ` (${count} unsaved)` : ""}</option>
                    })}
                </select>
                <nav aria-label="Settings categories" className="hidden gap-1 sm:flex overflow-x-auto rounded-lg border border-border/60 bg-muted/30 p-1">
                    {settingsSections.map((item, index) => {
                        const Icon = icons[index]
                        const count = settingsForm.dirtyFields.filter(f => settingsFields[f].section === item.id).length
                        const hasError = Object.keys(settingsForm.form.formState.errors).some(f => settingsFields[f as keyof typeof settingsFields]?.section === item.id)
                        return <button key={item.id} type="button" onClick={() => setSection(item.id)} aria-pressed={section === item.id} aria-label={item.label}
                            className={cn("flex flex-1 shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-md px-3 py-2 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", section === item.id ? "bg-card text-foreground shadow-sm" : "text-muted-foreground hover:bg-muted hover:text-foreground")}>
                            <Icon className="hidden size-3.5 xl:block" />{item.label}
                            {(count > 0 || hasError) && <span aria-label={hasError ? "Has validation errors" : `${count} unsaved changes`} className={cn("size-1.5 rounded-full", hasError ? "bg-destructive" : "bg-amber-500")} />}
                        </button>
                    })}
                </nav>
                <p className="text-sm text-muted-foreground">{settingsSections.find(s => s.id === section)?.description}</p>

                <fieldset disabled={settingsForm.isSaving} className="min-w-0 space-y-5 disabled:opacity-70">
                    {section === "general" && <>
                        <NodeSettingsGeneral node={node} settingsForm={settingsForm} />
                        <NodeSettingsStatusBar node={node} settingsForm={settingsForm} />
                    </>}
                    {section === "connection" && <>
                        <SSHSettingsCard settingsForm={settingsForm} isStealth={isStealth} isOnline={node.is_online} />
                    </>}
                    {section === "logging" && <NodeSettingsXray isOnline={node.is_online} settingsForm={settingsForm} />}
                    {section === "automation" && <>
                        <NodeSettingsStarlink settingsForm={settingsForm} isStealth={isStealth} />
                        <NodeSettingsCrashRecovery settingsForm={settingsForm} isStealth={isStealth} lastRecovery={node.last_crash_recovery} />
                    </>}
                    {section === "danger" && <DangerZone nodeId={node.id} nodeName={node.name} onDelete={handleDelete} deleteLabel={forceDelete ? "Force delete node" : "Delete node"} />}
                </fieldset>

                <NodeSettingsSaveBar settingsForm={settingsForm} onSave={() => void handleSave()} onReview={() => setReviewOpen(true)} />
                <Dialog open={reviewOpen} onOpenChange={setReviewOpen}>
                    <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-xl">
                        <DialogHeader><DialogTitle>Review changes</DialogTitle><DialogDescription>Only edited settings will be saved. Failed changes stay available for retry.</DialogDescription></DialogHeader>
                        <div className="divide-y divide-border">
                            {settingsForm.dirtyFields.map(field => <div key={field} className="space-y-2 py-3">
                                <button type="button" className="text-sm font-medium hover:underline" onClick={() => { setSection(settingsFields[field].section); setReviewOpen(false) }}>{settingsFields[field].label}</button>
                                <div className="grid grid-cols-[1fr_auto_1fr] items-start gap-3 text-xs">
                                    <span className="break-all text-muted-foreground">{formatValue(settingsForm.savedValues[field])}</span>
                                    <ArrowRight className="size-3.5 text-muted-foreground" />
                                    <span className="break-all">{formatValue(settingsForm.form.getValues(field))}</span>
                                </div>
                            </div>)}
                            {!settingsForm.dirtyFields.length && <p className="py-6 text-center text-sm text-muted-foreground">All changes saved.</p>}
                        </div>
                        <DialogFooter><Button variant="outline" onClick={() => setReviewOpen(false)}>Keep editing</Button><Button onClick={() => void handleSave()} disabled={settingsForm.isSaving || !settingsForm.isDirty}>{settingsForm.isSaving ? "Saving…" : "Save changes"}</Button></DialogFooter>
                    </DialogContent>
                </Dialog>

                {/* Navigation guard dialog */}
                <Dialog open={unsaved.showDialog} onOpenChange={(open) => { if (!open) unsaved.cancelNavigation() }}>
                    <DialogContent>
                        <DialogHeader>
                            <DialogTitle>Unsaved Changes</DialogTitle>
                            <DialogDescription>
                                You have unsaved changes. Are you sure you want to leave this page?
                                Your changes will be lost.
                            </DialogDescription>
                        </DialogHeader>
                        <DialogFooter>
                            <Button variant="outline" onClick={unsaved.cancelNavigation}>
                                Stay on Page
                            </Button>
                            <Button variant="destructive" onClick={unsaved.confirmNavigation}>
                                Discard Changes
                            </Button>
                        </DialogFooter>
                    </DialogContent>
                </Dialog>
            </div>
        </Form>
    )
}
