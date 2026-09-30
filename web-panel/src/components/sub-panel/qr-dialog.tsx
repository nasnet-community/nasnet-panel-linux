import { QRCodeSVG } from "qrcode.react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
} from "@/components/ui/dialog"

interface QRDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    value: string
    /** Defaults to the subscription wording. */
    title?: string
    description?: string
}

export function QRDialog({
    open,
    onOpenChange,
    value,
    title = "Subscription QR Code",
    description = "Scan this code with your proxy client (v2rayNG, Streisand, Shadowrocket, etc.) to import the subscription.",
}: QRDialogProps) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-sm">
                <DialogHeader>
                    <DialogTitle>{title}</DialogTitle>
                    <DialogDescription>{description}</DialogDescription>
                </DialogHeader>
                <div className="flex justify-center py-4">
                    <div className="bg-white p-4 rounded-xl">
                        <QRCodeSVG
                            value={value}
                            size={220}
                            level="M"
                            bgColor="#ffffff"
                            fgColor="#000000"
                        />
                    </div>
                </div>
            </DialogContent>
        </Dialog>
    )
}
