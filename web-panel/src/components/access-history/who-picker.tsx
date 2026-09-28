import { useEffect, useMemo, useRef, useState } from "react"
import { Search, User as UserIcon, X } from "lucide-react"
import { toast } from "@/lib/toast"
import { useSubscriptions } from "@/lib/queries/use-subscriptions"
import { useUsers } from "@/lib/queries/use-users"
import { getUserSubscriptions } from "@/lib/api/users"
import { subscriptionLabel, userDisplayName } from "@/lib/subscription-label"
import { cn } from "@/lib/utils"

export interface PickedSubscription {
    id: number
    label: string
}

interface Props {
    subscriptions: PickedSubscription[]
    emails: string[]
    onSubscriptions: (v: PickedSubscription[]) => void
    onEmails: (v: string[]) => void
    inputId?: string
    /** Taller input for the phone sheet. */
    touch?: boolean
    className?: string
}

/**
 * One box for "who": subscriptions by name or #id, users (added as all of
 * their subscriptions) and, as a last resort, an exact client email.
 */
export function WhoPicker({ subscriptions, emails, onSubscriptions, onEmails, inputId, touch, className }: Props) {
    const [raw, setRaw] = useState("")
    const [search, setSearch] = useState("")
    const [open, setOpen] = useState(false)
    const boxRef = useRef<HTMLDivElement>(null)

    useEffect(() => {
        const id = window.setTimeout(() => setSearch(raw.trim()), 250)
        return () => window.clearTimeout(id)
    }, [raw])

    useEffect(() => {
        if (!open) return
        const close = (e: MouseEvent) => { if (!boxRef.current?.contains(e.target as Node)) setOpen(false) }
        document.addEventListener("mousedown", close)
        return () => document.removeEventListener("mousedown", close)
    }, [open])

    const subsQuery = useSubscriptions({ page: 1, perPage: 8, search: search || undefined })
    const usersQuery = useUsers({ page: 1, per_page: 5, search })

    const picked = useMemo(() => new Set(subscriptions.map(s => s.id)), [subscriptions])
    const subOptions = (subsQuery.data ?? [])
        .filter(s => !picked.has(s.id))
        .map(s => ({ id: s.id, label: subscriptionLabel(s) || `Sub #${s.id}` }))
    const userOptions = search ? (usersQuery.data?.users ?? []).filter(u => u.total_subscriptions > 0).slice(0, 4) : []
    const text = raw.trim()

    const addSub = (s: PickedSubscription) => {
        onSubscriptions([...subscriptions, s])
        setRaw("")
        setOpen(false)
    }
    const addUser = async (userId: number, name: string) => {
        const res = await getUserSubscriptions(userId)
        if (!res.success) {
            toast.error(res.error || `Couldn't load ${name}'s subscriptions`)
            return
        }
        const add = (res.data ?? [])
            .filter(s => !picked.has(s.id))
            .map(s => ({ id: s.id, label: subscriptionLabel(s) || `Sub #${s.id}` }))
        onSubscriptions([...subscriptions, ...add])
        setRaw("")
        setOpen(false)
    }
    const addEmail = () => {
        if (!text || emails.includes(text)) return
        onEmails([...emails, text])
        setRaw("")
        setOpen(false)
    }

    const optionClass = "flex w-full items-center gap-2 rounded-md px-2 py-[7px] text-left text-sm transition-colors hover:bg-muted/60 focus-visible:bg-muted/60 focus-visible:outline-none"

    return (
        <div className={cn("space-y-2", className)}>
            <div ref={boxRef} className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <input
                    id={inputId}
                    value={raw}
                    onChange={e => { setRaw(e.target.value); setOpen(true) }}
                    onFocus={() => setOpen(true)}
                    onKeyDown={e => {
                        if (e.key === "Escape") setOpen(false)
                        if (e.key === "Enter") {
                            e.preventDefault()
                            // Options still belong to the previous text until the debounce lands.
                            if (search !== text) return
                            if (subOptions[0]) addSub(subOptions[0])
                            else if (text) addEmail()
                        }
                    }}
                    placeholder={subscriptions.length > 0 ? "Add a subscription, user or email" : "Subscription, user or client email"}
                    aria-expanded={open}
                    autoComplete="off"
                    className={cn(touch ? "h-11 text-base" : "h-8 text-[13px]", "w-full rounded-md border border-input bg-transparent pl-8 pr-2.5 shadow-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 dark:bg-input/30")}
                />
                {open && (subOptions.length > 0 || userOptions.length > 0 || text) && (
                    <div className="absolute inset-x-0 top-[calc(100%+6px)] z-30 flex max-h-80 flex-col gap-0.5 overflow-y-auto rounded-lg border border-border bg-popover p-1.5 shadow-lg">
                        {subOptions.length > 0 && <span className="px-2 pb-1 pt-1.5 text-[12px] text-muted-foreground">Subscriptions</span>}
                        {subOptions.map(o => (
                            <button key={o.id} type="button" className={optionClass} onMouseDown={e => e.preventDefault()} onClick={() => addSub(o)}>
                                <span className="font-mono text-[13px] text-muted-foreground">#{o.id}</span>
                                <span className="truncate">{o.label}</span>
                            </button>
                        ))}
                        {userOptions.length > 0 && (
                            <span className="mt-1 border-t border-border/60 px-2 pb-1 pt-2 text-[12px] text-muted-foreground">Users</span>
                        )}
                        {userOptions.map(u => {
                            const name = userDisplayName(u) || `User #${u.id}`
                            return (
                                <button key={u.id} type="button" className={optionClass} onMouseDown={e => e.preventDefault()} onClick={() => addUser(u.id, name)}>
                                    <UserIcon className="size-3.5 shrink-0 text-muted-foreground" />
                                    <span className="flex-1 truncate">{name}</span>
                                    <span className="shrink-0 text-[13px] text-muted-foreground">
                                        {u.total_subscriptions} subscription{u.total_subscriptions === 1 ? "" : "s"}
                                    </span>
                                </button>
                            )
                        })}
                        {text && (
                            <button
                                type="button"
                                className={cn(optionClass, "mt-1 border-t border-border/60 pt-2 text-muted-foreground")}
                                onMouseDown={e => e.preventDefault()}
                                onClick={addEmail}
                            >
                                Match client email exactly: <span className="truncate font-mono text-foreground">{text}</span>
                            </button>
                        )}
                    </div>
                )}
            </div>
            {(subscriptions.length > 0 || emails.length > 0) && (
                <div className="flex flex-wrap gap-1.5">
                    {subscriptions.map(s => (
                        <span key={s.id} className="inline-flex h-[26px] max-w-full items-center gap-1.5 rounded-full bg-muted pl-2.5 pr-1 text-[13px]">
                            <span className="font-mono text-muted-foreground">#{s.id}</span>
                            <span className="truncate font-medium">{s.label}</span>
                            <button
                                type="button"
                                aria-label={`Remove ${s.label}`}
                                onClick={() => onSubscriptions(subscriptions.filter(x => x.id !== s.id))}
                                className="flex size-[18px] items-center justify-center rounded-full text-muted-foreground hover:bg-foreground/10 hover:text-foreground"
                            >
                                <X className="size-3" />
                            </button>
                        </span>
                    ))}
                    {emails.map(e => (
                        <span key={e} className="inline-flex h-[26px] max-w-full items-center gap-1.5 rounded-full bg-muted pl-2.5 pr-1 font-mono text-[12px]">
                            <span className="truncate">{e}</span>
                            <button
                                type="button"
                                aria-label={`Remove ${e}`}
                                onClick={() => onEmails(emails.filter(x => x !== e))}
                                className="flex size-[18px] items-center justify-center rounded-full text-muted-foreground hover:bg-foreground/10 hover:text-foreground"
                            >
                                <X className="size-3" />
                            </button>
                        </span>
                    ))}
                </div>
            )}
        </div>
    )
}
