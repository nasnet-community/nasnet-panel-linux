import { QRCodeSVG } from "qrcode.react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
} from "@/components/ui/dialog"

export function CoreConfigQrDialog({
    open,
    onOpenChange,
    value,
    label,
}: {
    open: boolean
    onOpenChange: (open: boolean) => void
    value: string
    label?: string
}) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-sm">
                <DialogHeader>
                    <DialogTitle>Scan to connect</DialogTitle>
                    <DialogDescription>
                        {label || "Scan this code with the client app to import the connection."}
                    </DialogDescription>
                </DialogHeader>
                <div className="flex justify-center py-4">
                    <div className="rounded-xl bg-white p-4">
                        <QRCodeSVG value={value} size={220} level="M" bgColor="#ffffff" fgColor="#000000" />
                    </div>
                </div>
                {/* Text alternative for accessibility / copy-paste */}
                <p className="break-all rounded-md bg-muted px-3 py-2 text-center font-mono text-xs text-muted-foreground">
                    {value}
                </p>
            </DialogContent>
        </Dialog>
    )
}
