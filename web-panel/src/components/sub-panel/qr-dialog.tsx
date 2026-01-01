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
}

export function QRDialog({ open, onOpenChange, value }: QRDialogProps) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-sm">
                <DialogHeader>
                    <DialogTitle>Subscription QR Code</DialogTitle>
                    <DialogDescription>
                        Scan this code with your proxy client (v2rayNG, Streisand, Shadowrocket, etc.) to import the subscription.
                    </DialogDescription>
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
