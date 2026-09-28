import { useEffect, useRef, useState, type ReactNode } from "react"
import { Copy, Pencil } from "lucide-react"
import { toast } from "@/lib/toast"
import type { UserDetails, UserOverview } from "@/lib/types"
import { useUpdateUserNotes } from "@/lib/queries"
import { copyToClipboard } from "@/lib/utils"
import { FieldLabel } from "@/components/access-history/bits"
import { fmtAgo, fmtDayTime, fmtUTC } from "@/components/access-history/format"
import { displayName, languageName } from "./format"

function CopyButton({ value, label }: { value: string; label: string }) {
    return (
        <button
            type="button"
            aria-label={`Copy ${label}`}
            onClick={async () => { await copyToClipboard(value); toast.success(`${label} copied`) }}
            className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
            <Copy className="size-[13px]" />
        </button>
    )
}

function Row({ label, children }: { label: string; children: ReactNode }) {
    return (
        <>
            <dt className="pt-0.5"><FieldLabel>{label}</FieldLabel></dt>
            <dd className="m-0 flex min-w-0 items-center gap-1.5 text-sm">{children}</dd>
        </>
    )
}

const card = "flex flex-col gap-3.5 rounded-xl border border-border bg-card p-5"

export function IdentityCard({ user, overview, onEditTelegramId }: { user: UserDetails; overview?: UserOverview; onEditTelegramId: () => void }) {
    const hasName = displayName(user) !== `@${user.username}` && !displayName(user).startsWith("User #")
    const lastSeen = user.last_active_at || overview?.last_seen_at
    return (
        <section aria-label="Identity" className={card}>
            <h2><FieldLabel>Identity</FieldLabel></h2>
            <dl className="grid grid-cols-[112px_minmax(0,1fr)] gap-x-3 gap-y-3">
                <Row label="Telegram">
                    {user.username ? <><span className="truncate">@{user.username}</span><CopyButton value={`@${user.username}`} label="Username" /></> : <span className="text-muted-foreground">No username</span>}
                </Row>
                <Row label="Telegram ID">
                    <span className="font-mono">{user.telegram_id > 0 ? user.telegram_id : "Not set"}</span>
                    {user.telegram_id > 0 && <CopyButton value={String(user.telegram_id)} label="Telegram ID" />}
                    <button type="button" aria-label="Edit Telegram ID" onClick={onEditTelegramId} className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                        <Pencil className="size-[13px]" />
                    </button>
                </Row>
                {hasName && <Row label="Name">{displayName(user)}</Row>}
                <Row label="Language">{languageName(user.language)}{user.language && <span className="font-mono text-muted-foreground">{user.language}</span>}</Row>
                <Row label="Joined"><span title={fmtUTC(user.created_at)}>{fmtDayTime(user.created_at)}</span></Row>
                <Row label="Last seen">
                    {lastSeen ? <span title={fmtUTC(lastSeen)}>{fmtAgo(lastSeen)}{overview?.last_seen_node && <span className="text-muted-foreground"> on {overview.last_seen_node}</span>}</span> : <span className="text-muted-foreground">Never connected</span>}
                </Row>
                <Row label="User ID"><span className="font-mono">#{user.id}</span><CopyButton value={String(user.id)} label="User ID" /></Row>
            </dl>
        </section>
    )
}

/** Admin-only notes, saved 1.5s after typing stops or on blur. */
export function NotesCard({ userId, initial }: { userId: number; initial: string }) {
    const [notes, setNotes] = useState(initial)
    const [savedAt, setSavedAt] = useState<number | null>(null)
    const saved = useRef(initial)
    const current = useRef(initial)
    const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
    const update = useUpdateUserNotes()

    // Take the server's copy only when nothing typed is waiting to be saved;
    // a refetch after a save must not wipe what came after it.
    useEffect(() => {
        if (current.current === saved.current) {
            setNotes(initial)
            current.current = initial
        }
        saved.current = initial
    }, [initial])
    useEffect(() => () => { if (timer.current) clearTimeout(timer.current) }, [])

    const save = (value: string) => {
        if (value === saved.current) return
        saved.current = value
        update.mutate({ userId, notes: value }, { onSuccess: () => setSavedAt(Date.now()) })
    }

    return (
        <section aria-label="Notes" className={card}>
            <div className="flex items-center justify-between">
                <label htmlFor="user-notes"><FieldLabel>Notes</FieldLabel></label>
                <span className="text-[12px] text-muted-foreground">Only admins see these</span>
            </div>
            <textarea
                id="user-notes"
                value={notes}
                placeholder="Add a note about this customer"
                onChange={(e) => {
                    const v = e.target.value
                    setNotes(v)
                    current.current = v
                    if (timer.current) clearTimeout(timer.current)
                    timer.current = setTimeout(() => save(v), 1500)
                }}
                onBlur={() => { if (timer.current) clearTimeout(timer.current); save(notes) }}
                className="min-h-[220px] w-full flex-1 resize-none rounded-lg border border-input bg-muted/20 px-3 py-2.5 text-sm leading-relaxed placeholder:text-muted-foreground/70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
            <span className="text-[12px] text-muted-foreground">
                {update.isPending ? "Saving…" : savedAt ? `Saved ${fmtAgo(savedAt)}` : "Saves as you type"}
            </span>
        </section>
    )
}
