import { useState } from "react"
import { Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useChangePassword } from "@/lib/queries/use-settings"
import { SettingRow, SettingsGroup, Value } from "@/components/settings-page/settings-group"
import type { CardDef } from "./catalog"
import { Hl, usePage } from "./settings-card"

/** The admin login: the username is set in .env, the password changes here. */
export function PasswordCard({ card }: { card: CardDef }) {
    const { ctx } = usePage()
    const [open, setOpen] = useState(false)
    return (
        <SettingsGroup id={card.id} icon={card.icon} title={card.title} description={card.description}>
            <SettingRow
                label={<Hl text="Username" />}
                action={<span title="Set in .env as ADMIN_USERNAME" className="rounded border border-border px-1.5 py-px font-mono text-[11px] text-muted-foreground">.env</span>}
            >
                <Value mono>{ctx.status?.admin.username}</Value>
            </SettingRow>
            <SettingRow
                label={<Hl text="Password" />}
                action={<Button type="button" variant="outline" size="sm" className="h-8" onClick={() => setOpen(true)}>Change…</Button>}
            >
                <span className="font-mono text-[13px] text-muted-foreground">••••••••••</span>
            </SettingRow>
            <ChangePasswordDialog open={open} onOpenChange={setOpen} username={ctx.status?.admin.username} />
        </SettingsGroup>
    )
}

function ChangePasswordDialog({ open, onOpenChange, username }: { open: boolean; onOpenChange: (o: boolean) => void; username?: string }) {
    const change = useChangePassword()
    const [current, setCurrent] = useState("")
    const [next, setNext] = useState("")
    const [again, setAgain] = useState("")
    const [tried, setTried] = useState(false)
    const errors = {
        current: !current ? "Required" : "",
        next: next.length < 8 ? "At least 8 characters" : "",
        again: again !== next ? "Doesn't match" : "",
    }
    const close = (o: boolean) => {
        onOpenChange(o)
        if (!o) { setCurrent(""); setNext(""); setAgain(""); setTried(false) }
    }
    const submit = async (e: React.FormEvent) => {
        e.preventDefault()
        setTried(true)
        if (errors.current || errors.next || errors.again) return
        try {
            await change.mutateAsync({ current_password: current, new_password: next, confirm_password: again })
            close(false)
        } catch { /* the mutation toasts */ }
    }
    const field = (id: string, label: string, value: string, set: (v: string) => void, error: string, auto: string) => (
        <div className="space-y-1.5">
            <Label htmlFor={id}>{label}</Label>
            <Input id={id} type="password" autoComplete={auto} value={value} onChange={e => set(e.target.value)} aria-invalid={tried && !!error} />
            {tried && error && <p className="text-xs text-red-500">{error}</p>}
        </div>
    )
    return (
        <Dialog open={open} onOpenChange={close}>
            <DialogContent className="sm:max-w-md">
                <form onSubmit={submit} className="space-y-4">
                    <DialogHeader>
                        <DialogTitle>Change the panel password</DialogTitle>
                        <DialogDescription>For the {username ? <span className="font-mono text-foreground">{username}</span> : "admin"} login.</DialogDescription>
                    </DialogHeader>
                    {field("pw-current", "Current password", current, setCurrent, errors.current, "current-password")}
                    {field("pw-new", "New password", next, setNext, errors.next, "new-password")}
                    {field("pw-again", "Repeat the new password", again, setAgain, errors.again, "new-password")}
                    <DialogFooter>
                        <Button type="button" variant="ghost" onClick={() => close(false)}>Cancel</Button>
                        <Button type="submit" disabled={change.isPending}>{change.isPending && <Loader2 className="mr-1.5 size-4 animate-spin" />}Change password</Button>
                    </DialogFooter>
                </form>
            </DialogContent>
        </Dialog>
    )
}
