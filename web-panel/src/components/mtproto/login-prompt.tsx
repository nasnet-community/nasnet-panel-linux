import { useState } from "react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Checkbox } from "@/components/ui/checkbox"

export interface SshLogin {
    ssh_user: string
    ssh_password?: string
    ssh_key?: string
    ssh_passphrase?: string
    force?: boolean
}

export interface LoginPromptProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    title: string
    description: string
    defaultUser: string
    confirmLabel: string
    /** Offer "delete the node even if the agent cannot be removed". */
    allowForce?: boolean
    /** Ask for credentials as optional (removal over the link needs none). */
    optional?: boolean
    onSubmit: (login: SshLogin) => void
}

/** Asks for SSH credentials for one job. They are handed on and forgotten. */
export function LoginPrompt({ open, onOpenChange, title, description, defaultUser, confirmLabel, allowForce, optional, onSubmit }: LoginPromptProps) {
    const [user, setUser] = useState(defaultUser)
    const [method, setMethod] = useState<"password" | "key">("password")
    const [password, setPassword] = useState("")
    const [key, setKey] = useState("")
    const [passphrase, setPassphrase] = useState("")
    const [force, setForce] = useState(false)

    const hasSecret = method === "password" ? password !== "" : key.trim() !== ""
    const ready = optional || force || hasSecret

    function reset() {
        setPassword("")
        setKey("")
        setPassphrase("")
        setForce(false)
    }

    function submit() {
        const login: SshLogin = { ssh_user: user.trim() || "root" }
        if (method === "password" && password) login.ssh_password = password
        if (method === "key" && key.trim()) {
            login.ssh_key = key
            if (passphrase) login.ssh_passphrase = passphrase
        }
        if (force) login.force = true
        reset() // the secrets leave this component the moment they are handed on
        onSubmit(login)
    }

    return (
        <Dialog
            open={open}
            onOpenChange={(next) => {
                if (!next) reset()
                onOpenChange(next)
            }}
        >
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{title}</DialogTitle>
                    <DialogDescription>{description}</DialogDescription>
                </DialogHeader>
                <div className="space-y-4">
                    <div className="space-y-1.5">
                        <Label htmlFor="mtproto-login-user">User</Label>
                        <Input id="mtproto-login-user" value={user} onChange={(e) => setUser(e.target.value)} autoComplete="off" />
                    </div>
                    <div className="flex gap-2" role="group" aria-label="Sign in with">
                        <Button type="button" size="sm" variant={method === "password" ? "default" : "outline"} onClick={() => setMethod("password")}>
                            Password
                        </Button>
                        <Button type="button" size="sm" variant={method === "key" ? "default" : "outline"} onClick={() => setMethod("key")}>
                            Private key
                        </Button>
                    </div>
                    {method === "password" ? (
                        <div className="space-y-1.5">
                            <Label htmlFor="mtproto-login-password">Password</Label>
                            <Input
                                id="mtproto-login-password"
                                type="password"
                                autoComplete="off"
                                value={password}
                                onChange={(e) => setPassword(e.target.value)}
                            />
                            <p className="text-xs text-muted-foreground">Used once for this job. It is never stored.</p>
                        </div>
                    ) : (
                        <div className="space-y-3">
                            <div className="space-y-1.5">
                                <Label htmlFor="mtproto-login-key">Private key</Label>
                                <Textarea
                                    id="mtproto-login-key"
                                    rows={5}
                                    spellCheck={false}
                                    className="font-mono text-xs"
                                    value={key}
                                    onChange={(e) => setKey(e.target.value)}
                                />
                            </div>
                            <div className="space-y-1.5">
                                <Label htmlFor="mtproto-login-passphrase">Passphrase (if the key has one)</Label>
                                <Input
                                    id="mtproto-login-passphrase"
                                    type="password"
                                    autoComplete="off"
                                    value={passphrase}
                                    onChange={(e) => setPassphrase(e.target.value)}
                                />
                            </div>
                        </div>
                    )}
                    {allowForce && (
                        <label className="flex items-start gap-2 text-sm">
                            <Checkbox checked={force} onCheckedChange={(v) => setForce(v === true)} aria-label="Delete anyway" />
                            <span>
                                Delete the node even if the agent cannot be removed.
                                <span className="block text-xs text-muted-foreground">The agent would stay on the node until you remove it by hand.</span>
                            </span>
                        </label>
                    )}
                </div>
                <DialogFooter>
                    <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button type="button" disabled={!ready} onClick={submit}>
                        {confirmLabel}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
