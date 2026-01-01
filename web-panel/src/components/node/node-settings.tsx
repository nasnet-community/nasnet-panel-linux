import { useState } from "react"
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
import { deleteNode } from "@/lib/admin-api"
import { useNodeSettingsForm } from "@/hooks/use-node-settings-form"
import { useUnsavedChanges } from "@/hooks/use-unsaved-changes"
import { SSHSettingsCard } from "./ssh-settings-card"
import { NodeSettingsStatusBar } from "./settings/node-settings-status-bar"
import { NodeSettingsGeneral } from "./settings/node-settings-general"
import { NodeSettingsXray } from "./settings/node-settings-xray"
// import { NodeSettingsBandwidth } from "./settings/node-settings-bandwidth"
import { NodeSettingsStarlink } from "./settings/node-settings-starlink"
import { NodeSettingsCrashRecovery } from "./settings/node-settings-crash-recovery"
import { NodeSettingsSaveBar } from "./settings/node-settings-save-bar"
import { NodeSettingsFab } from "./settings/node-settings-fab"
import { DangerZone } from "@/components/node/danger-zone"
import { EntityMaintenanceCard } from "@/components/maintenance/entity-maintenance-card"
import { queryKeys } from "@/lib/queries"
import type { Node } from "@/lib/types"

interface NodeSettingsProps {
    node: Node
    onRefresh: () => void
}

export function NodeSettings({ node, onRefresh }: NodeSettingsProps) {
    const navigate = useNavigate()
    const settingsForm = useNodeSettingsForm(node, { onRefresh })
    const unsaved = useUnsavedChanges(settingsForm.isDirty)
    const [forceDelete, setForceDelete] = useState(false)

    const handleDelete = async () => {
        if (!confirm(`Are you sure you want to delete node "${node.name}"? This action cannot be undone.`)) return
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
            <div className="space-y-6 max-w-4xl relative">
                {/* Enhanced status bar with Active toggle */}
                <NodeSettingsStatusBar node={node} settingsForm={settingsForm} />

                {/* Maintenance mode — operational state, grouped with the Active toggle above.
                    Own mutation/save, so it sits outside the shared form. */}
                <EntityMaintenanceCard
                    type="node"
                    id={node.id}
                    initialEnabled={!!node.maintenance_mode}
                    initialMessage={node.maintenance_message || ""}
                    initialSince={node.maintenance_since}
                    invalidateKey={queryKeys.nodeDetails(node.id)}
                />

                {/* Settings sections (share the same form) */}
                <NodeSettingsGeneral node={node} settingsForm={settingsForm} />
                <NodeSettingsXray isOnline={node.is_online} settingsForm={settingsForm} />
                {/*  Speed limit feature (bandwidth shaping) - disabled
                <NodeSettingsBandwidth settingsForm={settingsForm} isStealth={node.is_stealth} />
                */}
                <NodeSettingsStarlink settingsForm={settingsForm} isStealth={node.is_stealth} />
                <NodeSettingsCrashRecovery settingsForm={settingsForm} isStealth={node.is_stealth} />
                <SSHSettingsCard settingsForm={settingsForm} isStealth={node.is_stealth} />

                {/* Unified Danger Zone: Delete (hub-only) · Wipe · Nuke */}
                <DangerZone
                    nodeId={node.id}
                    nodeName={node.name}
                    onDelete={handleDelete}
                    deleteLabel={forceDelete ? "Force Delete" : "Delete Node"}
                />

                {/* Sticky save bar (appears when dirty) */}
                <NodeSettingsSaveBar settingsForm={settingsForm} />

                {/* Floating action menu */}
                <NodeSettingsFab
                    node={node}
                    onRefresh={onRefresh}
                />

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
