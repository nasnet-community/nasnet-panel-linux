import { useNavigate } from "react-router"
import { ChevronDown, Download, Lock, RefreshCw, Settings, Trash2, XCircle } from "lucide-react"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import type { CertOverview } from "@/lib/types"
import { useCertActions } from "./use-cert-actions"

export function CertActionsMenu({ overview }: { overview?: CertOverview }) {
    const actions = useCertActions(overview)
    const navigate = useNavigate()
    const strict = overview?.strict_mode
    const expiring = overview?.attention.some(a => a.kind === "expiring") ?? false

    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" disabled={!overview}>
                    Actions<ChevronDown className="ml-1.5 h-4 w-4" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-60">
                <DropdownMenuItem disabled={!expiring} onClick={actions.rotateExpiring}>
                    <RefreshCw className="mr-2 h-4 w-4" />Rotate expiring certificates
                </DropdownMenuItem>
                <DropdownMenuItem disabled={!overview?.counts.orphans} onClick={actions.revokeOrphans}>
                    <XCircle className="mr-2 h-4 w-4" />Revoke orphaned certificates
                </DropdownMenuItem>
                <DropdownMenuItem onClick={actions.purge}>
                    <Trash2 className="mr-2 h-4 w-4" />Purge revoked &amp; expired
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem disabled={!overview?.ca} onClick={() => void actions.downloadCA()}>
                    <Download className="mr-2 h-4 w-4" />Download CA certificate
                </DropdownMenuItem>
                <DropdownMenuItem disabled={!strict || (!strict.enabled && !strict.ready)} onClick={actions.toggleStrict}>
                    <Lock className="mr-2 h-4 w-4" />{strict?.enabled ? "Turn off strict mode" : "Turn on strict mode"}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => navigate("/settings")}>
                    <Settings className="mr-2 h-4 w-4" />Auto-rotate settings
                </DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    )
}
