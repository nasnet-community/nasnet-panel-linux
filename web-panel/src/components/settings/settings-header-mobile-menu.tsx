import { Button } from "@/components/ui/button"
import {
    DropdownMenu,
    DropdownMenuTrigger,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu"
import { HiDotsHorizontal, HiOutlineRefresh } from "react-icons/hi"
import { SettingsImportExport } from "./settings-import-export"
import { DangerZone } from "./danger-zone"

interface SettingsHeaderMobileMenuProps {
    onRestart: () => void
    onRefresh: () => void
    restarting: boolean
    loading: boolean
    saving: boolean
}

export function SettingsHeaderMobileMenu({
    onRestart,
    onRefresh,
    restarting,
    loading,
    saving,
}: SettingsHeaderMobileMenuProps) {
    const disabled = saving || loading

    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button
                    variant="outline"
                    size="icon"
                    className="h-10 w-10 shrink-0"
                    aria-label="More actions"
                >
                    <HiDotsHorizontal className="w-5 h-5" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuItem
                    onSelect={(e) => {
                        e.preventDefault()
                        onRefresh()
                    }}
                    disabled={disabled}
                >
                    <HiOutlineRefresh className={`w-4 h-4 mr-2 ${loading ? "animate-spin" : ""}`} />
                    Refresh
                </DropdownMenuItem>

                <SettingsImportExport disabled={disabled} asMenuItem />

                <DropdownMenuItem
                    onSelect={(e) => {
                        e.preventDefault()
                        onRestart()
                    }}
                    disabled={disabled || restarting}
                >
                    <HiOutlineRefresh className={`w-4 h-4 mr-2 ${restarting ? "animate-spin" : ""}`} />
                    Restart Server
                </DropdownMenuItem>

                <DropdownMenuSeparator />

                <DangerZone disabled={disabled} asMenuItem />
            </DropdownMenuContent>
        </DropdownMenu>
    )
}
