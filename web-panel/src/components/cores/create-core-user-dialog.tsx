import { useState } from "react"
import { CheckCircle2 } from "lucide-react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
    DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useCreateCoreUser } from "@/lib/queries"
import type { CoreProtocol, CoreUser } from "@/lib/types/cores"
import { CoreConfigCard } from "./core-config-card"

export function CreateCoreUserDialog({
    inboundId,
    nodeId,
    protocol,
    host,
    hostOnline,
    open,
    onOpenChange,
}: {
    inboundId: number
    nodeId: number
    protocol: CoreProtocol
    host?: string
    hostOnline: boolean
    open: boolean
    onOpenChange: (open: boolean) => void
}) {
    const create = useCreateCoreUser(inboundId, nodeId)
    const [email, setEmail] = useState("")
    const [username, setUsername] = useState("")
    const [secret, setSecret] = useState("")
    const [kLimit, setKLimit] = useState(1)
    const [created, setCreated] = useState<CoreUser | null>(null)

    const reset = () => {
        setEmail("")
        setUsername("")
        setSecret("")
        setKLimit(1)
        setCreated(null)
    }

    const handleOpenChange = (o: boolean) => {
        if (!o) reset()
        onOpenChange(o)
    }

    const submit = async () => {
        if (!email.trim()) return
        try {
            const res = await create.mutateAsync({
                email: email.trim(),
                username: username.trim() || undefined,
                secret: secret.trim() || undefined,
                k_limit: kLimit,
            })
            setCreated(res.user)
        } catch {
            // toast handled in the hook
        }
    }

    return (
        <Dialog open={open} onOpenChange={handleOpenChange}>
            <DialogContent className="max-w-md">
                {created ? (
                    <>
                        <DialogHeader>
                            <DialogTitle className="flex items-center gap-2">
                                <CheckCircle2 className="size-5 text-emerald-500" />
                                User added
                            </DialogTitle>
                            <DialogDescription>
                                Hand this config to <span className="font-medium">{created.email}</span>. The
                                secret is only delivered here — it is never shown in the user list again.
                            </DialogDescription>
                        </DialogHeader>
                        {hostOnline ? (
                            <CoreConfigCard userId={created.id} host={host} protocol={protocol} />
                        ) : (
                            <p className="rounded-lg border border-amber-500/40 bg-amber-500/5 p-3 text-sm text-amber-600 dark:text-amber-400">
                                Node is offline — reconnect it to fetch the client config for this user.
                            </p>
                        )}
                        <DialogFooter>
                            <Button variant="outline" onClick={reset}>
                                Add another
                            </Button>
                            <Button onClick={() => handleOpenChange(false)}>Done</Button>
                        </DialogFooter>
                    </>
                ) : (
                    <>
                        <DialogHeader>
                            <DialogTitle>Add user</DialogTitle>
                            <DialogDescription>
                                A username and secret are generated automatically when left blank.
                            </DialogDescription>
                        </DialogHeader>
                        <div className="space-y-4 py-2">
                            <div className="space-y-1.5">
                                <Label htmlFor="core-user-email">Email *</Label>
                                <Input
                                    id="core-user-email"
                                    value={email}
                                    onChange={(e) => setEmail(e.target.value)}
                                    placeholder="alice@example.com"
                                    autoFocus
                                />
                                <p className="text-xs text-muted-foreground">
                                    Used for routing and accounting identity.
                                </p>
                            </div>
                            <div className="grid grid-cols-2 gap-3">
                                <div className="space-y-1.5">
                                    <Label htmlFor="core-user-username">Username</Label>
                                    <Input
                                        id="core-user-username"
                                        value={username}
                                        onChange={(e) => setUsername(e.target.value)}
                                        placeholder="auto"
                                    />
                                </div>
                                <div className="space-y-1.5">
                                    <Label htmlFor="core-user-klimit">Devices (K)</Label>
                                    <Input
                                        id="core-user-klimit"
                                        type="number"
                                        min={1}
                                        value={kLimit}
                                        onChange={(e) => setKLimit(Math.max(1, Number(e.target.value) || 1))}
                                    />
                                </div>
                            </div>
                            <div className="space-y-1.5">
                                <Label htmlFor="core-user-secret">Secret / password</Label>
                                <Input
                                    id="core-user-secret"
                                    value={secret}
                                    onChange={(e) => setSecret(e.target.value)}
                                    placeholder="auto-generated"
                                />
                            </div>
                        </div>
                        <DialogFooter>
                            <Button variant="outline" onClick={() => handleOpenChange(false)}>
                                Cancel
                            </Button>
                            <Button onClick={submit} disabled={!email.trim() || create.isPending}>
                                {create.isPending ? "Adding…" : "Add user"}
                            </Button>
                        </DialogFooter>
                    </>
                )}
            </DialogContent>
        </Dialog>
    )
}
