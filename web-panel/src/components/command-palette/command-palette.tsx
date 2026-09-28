import { useEffect, useState } from "react"
import { useLocation, useNavigate } from "react-router"
import * as DialogPrimitive from "@radix-ui/react-dialog"
import { Command } from "cmdk"
import {
    History, Keyboard, KeyRound, LogOut, Moon, PanelLeft, Search, Server, Sun, User, X, type LucideIcon,
} from "lucide-react"
import { useCommandPalette } from "@/store/command-palette-store"
import { useSubscriptionsStore } from "@/store/subscriptions-store"
import { useTheme } from "@/components/providers/theme-provider"
import { PALETTE_PAGES, type NavItem } from "@/components/layouts/nav-config"
import { listSubscriptions } from "@/lib/api/subscriptions"
import { isLocalNode, nodeAddress } from "@/lib/node-kind"
import { getApiBaseUrl } from "@/lib/config"
import { MOD_KEY } from "@/lib/platform"
import { pushRecent, readRecent, type RecentItem } from "@/lib/recent-items"
import { cn } from "@/lib/utils"
import type { Node, Subscription, UserListItem } from "@/lib/types"
import { useCommandSearch } from "./use-command-search"

type PillTone = "ok" | "warn" | "danger" | "neutral"

const PILL: Record<PillTone, string> = {
    ok: "bg-status-success-soft text-status-success",
    warn: "bg-status-warning-soft text-status-warning",
    danger: "bg-status-danger-soft text-status-danger",
    neutral: "bg-muted text-muted-foreground",
}

const SUB_TONE: Record<Subscription["status"], PillTone> = {
    active: "ok", paused: "neutral", expired: "danger", cancelled: "neutral", traffic_exhausted: "danger",
}

const RECENT_ICON: Record<Exclude<RecentItem["kind"], "page">, LucideIcon> = {
    user: User, subscription: KeyRound, node: Server,
}

interface Entry {
    value: string
    label: string
    detail?: string
    icon: LucideIcon
    pill?: { text: string; tone: PillTone }
    hint?: string
    /** Plain route, so ⌘↵ can open it in a new tab. */
    href?: string
    run: () => void
}

interface CommandPaletteProps {
    onToggleSidebar?: () => void
    onLogout: () => void
}

export function CommandPalette({ onToggleSidebar, onLogout }: CommandPaletteProps) {
    const open = useCommandPalette((s) => s.open)
    const setOpen = useCommandPalette((s) => s.setOpen)
    const setShortcutsOpen = useCommandPalette((s) => s.setShortcutsOpen)
    const navigate = useNavigate()
    const { pathname } = useLocation()
    const { resolvedTheme, setTheme } = useTheme()
    const [query, setQuery] = useState("")
    const [selected, setSelected] = useState("")
    const search = useCommandSearch(query, open)

    useEffect(() => {
        if (!open) setQuery("")
    }, [open])

    // Pages count as recent however they were reached; entities only when opened from here.
    useEffect(() => {
        const page = PALETTE_PAGES.find((p) => p.href === pathname)
        if (page) pushRecent({ kind: "page", label: page.label, href: page.href })
    }, [pathname])

    // The sheet reads the store, so it opens over the list whatever filters are set.
    const openSubscription = (sub: Subscription) => {
        useSubscriptionsStore.getState().openDetailsSheet(sub)
        navigate("/subscriptions")
    }

    const openSubscriptionByKey = async (key: string) => {
        const res = await listSubscriptions({ search: key, per_page: 5 })
        const sub = res.success ? (res.data ?? []).find((s) => s.link_key === key || s.config_id === key) : undefined
        if (sub) openSubscription(sub)
        else navigate("/subscriptions")
    }

    const pageEntry = (p: NavItem): Entry => ({
        value: `page:${p.href}`, label: p.label, icon: p.icon, href: p.href, run: () => navigate(p.href),
    })

    const recentEntry = (r: RecentItem): Entry => ({
        value: `recent:${r.key ?? r.href}`,
        label: r.label,
        detail: r.kind === "page" ? undefined : r.kind,
        icon: (r.kind === "page" ? PALETTE_PAGES.find((p) => p.href === r.href)?.icon : RECENT_ICON[r.kind]) ?? Search,
        href: r.key ? undefined : r.href,
        run: () => {
            if (r.key) void openSubscriptionByKey(r.key)
            else navigate(r.href)
        },
    })

    const userEntry = (u: UserListItem): Entry => {
        const name = u.username || [u.first_name, u.last_name].filter(Boolean).join(" ") || `user ${u.telegram_id}`
        const href = `/users/${u.id}`
        return {
            value: `user:${u.id}`, label: name, detail: `tg ${u.telegram_id} · ${u.active_subscriptions} active`,
            icon: User, href,
            run: () => { pushRecent({ kind: "user", label: name, href }); navigate(href) },
        }
    }

    const subscriptionEntry = (s: Subscription): Entry => {
        const label = s.label || `#${s.id}`
        const key = s.link_key || s.config_id || undefined
        return {
            value: `sub:${s.id}`, label,
            detail: s.user?.username || undefined,
            icon: KeyRound,
            pill: { text: s.status.replace("_", " "), tone: SUB_TONE[s.status] },
            run: () => { pushRecent({ kind: "subscription", label, href: "/subscriptions", key }); openSubscription(s) },
        }
    }

    const nodeEntry = (n: Node): Entry => {
        const href = `/nodes/${n.id}`
        return {
            value: `node:${n.id}`, label: n.name, detail: isLocalNode(n) ? "This router" : nodeAddress(n), icon: Server, href,
            pill: { text: n.is_online ? "online" : "offline", tone: n.is_online ? "ok" : "danger" },
            run: () => { pushRecent({ kind: "node", label: n.name, href }); navigate(href) },
        }
    }

    const q = query.trim().toLowerCase()
    const matches = (text: string) => text.toLowerCase().includes(q)
    const accessHistoryHref = `/access-history?q=${encodeURIComponent(search.term)}`
    const actions: Entry[] = [
        ...(search.term ? [{
            value: "action:access-history",
            label: `search access history for “${search.term}”`,
            detail: "who reached a domain or IP",
            icon: History,
            href: accessHistoryHref,
            run: () => navigate(accessHistoryHref),
        }] : []),
        {
            value: "action:theme",
            label: resolvedTheme === "dark" ? "switch to light mode" : "switch to dark mode",
            icon: resolvedTheme === "dark" ? Sun : Moon,
            run: () => setTheme(resolvedTheme === "dark" ? "light" : "dark"),
        },
        ...(onToggleSidebar ? [{ value: "action:sidebar", label: "toggle sidebar", icon: PanelLeft, hint: "[", run: onToggleSidebar }] : []),
        { value: "action:shortcuts", label: "keyboard shortcuts", icon: Keyboard, hint: "?", run: () => setShortcutsOpen(true) },
        { value: "action:logout", label: "log out", icon: LogOut, run: onLogout },
    ]

    // Built only while open; the layout re-renders on every badge poll.
    const groups = !open ? [] : (q
        ? [
            { heading: "Users", entries: search.users.map(userEntry) },
            { heading: "Subscriptions", entries: search.subscriptions.map(subscriptionEntry) },
            { heading: "Nodes", entries: search.nodes.map(nodeEntry) },
            { heading: "Pages", entries: PALETTE_PAGES.filter((p) => matches(p.label)).map(pageEntry) },
            { heading: "Actions", entries: actions.filter((a) => a.value === "action:access-history" || matches(a.label)) },
        ]
        : [
            { heading: "Recent", entries: readRecent().map(recentEntry) },
            { heading: "Pages", entries: PALETTE_PAGES.map(pageEntry) },
            { heading: "Actions", entries: actions },
        ]
    ).filter((g) => g.entries.length > 0)

    // Results arrive after the local groups; move the highlight to the best match
    // instead of leaving it on whatever row was first a moment ago.
    const firstValue = groups[0]?.entries[0]?.value ?? ""
    useEffect(() => setSelected(firstValue), [firstValue])

    const byValue = new Map(groups.flatMap((g) => g.entries.map((e) => [e.value, e] as const)))
    const run = (entry: Entry) => {
        setOpen(false)
        entry.run()
    }

    return (
        <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
            <DialogPrimitive.Portal>
                <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/60 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
                <DialogPrimitive.Content
                    aria-describedby={undefined}
                    className="fixed inset-0 z-50 flex flex-col overflow-hidden bg-popover text-popover-foreground md:inset-auto md:left-1/2 md:top-[12vh] md:w-[calc(100%-2rem)] md:max-w-[640px] md:-translate-x-1/2 md:rounded-xl md:border md:shadow-2xl"
                >
                    <DialogPrimitive.Title className="sr-only">Search or jump to</DialogPrimitive.Title>
                    <Command
                        shouldFilter={false}
                        loop
                        value={selected}
                        onValueChange={setSelected}
                        onKeyDown={(e) => {
                            if (e.key !== "Enter" || !(e.metaKey || e.ctrlKey)) return
                            const entry = byValue.get(selected)
                            if (!entry?.href) return
                            e.preventDefault()
                            window.open(`${getApiBaseUrl()}${entry.href}`, "_blank", "noopener")
                            setOpen(false)
                        }}
                        className="flex min-h-0 flex-1 flex-col"
                    >
                        <div className="flex h-[52px] shrink-0 items-center gap-3 border-b px-4">
                            <Search className="size-[18px] shrink-0 text-muted-foreground" />
                            <Command.Input
                                value={query}
                                onValueChange={setQuery}
                                placeholder="search users, subscriptions, nodes, pages…"
                                className="h-full min-w-0 flex-1 bg-transparent text-base outline-none placeholder:text-muted-foreground"
                            />
                            <kbd className="hidden md:inline rounded border px-1.5 font-mono text-[11px] leading-[18px] text-muted-foreground">esc</kbd>
                            <DialogPrimitive.Close aria-label="Close" className="md:hidden -mr-2 size-10 flex items-center justify-center rounded-lg text-muted-foreground">
                                <X className="size-5" />
                            </DialogPrimitive.Close>
                        </div>
                        <Command.List className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-1.5 md:max-h-[min(60vh,440px)] md:flex-none">
                            {search.loading && (
                                <Command.Loading className="px-2.5 py-2 text-xs text-muted-foreground">searching…</Command.Loading>
                            )}
                            <Command.Empty className="py-10 text-center text-sm text-muted-foreground">no matches</Command.Empty>
                            {groups.map((group) => (
                                <Command.Group
                                    key={group.heading}
                                    heading={group.heading}
                                    className="[&_[cmdk-group-heading]]:px-2.5 [&_[cmdk-group-heading]]:pt-2.5 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[0.08em] [&_[cmdk-group-heading]]:text-muted-foreground"
                                >
                                    {group.entries.map((entry) => (
                                        <Command.Item
                                            key={entry.value}
                                            value={entry.value}
                                            onSelect={() => run(entry)}
                                            className="flex h-11 cursor-pointer items-center gap-3 rounded-lg px-2.5 data-[selected=true]:bg-muted"
                                        >
                                            <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted/60 text-muted-foreground">
                                                <entry.icon className="size-4" />
                                            </span>
                                            <span className="flex min-w-0 flex-1 flex-col">
                                                <span className="flex items-center gap-2 text-[14px] font-medium">
                                                    <span className="truncate">{entry.label}</span>
                                                    {entry.pill && (
                                                        <span className={cn("shrink-0 rounded-full px-1.5 text-[11.5px] leading-[18px]", PILL[entry.pill.tone])}>
                                                            {entry.pill.text}
                                                        </span>
                                                    )}
                                                </span>
                                                {entry.detail && <span className="truncate text-[12.5px] text-muted-foreground">{entry.detail}</span>}
                                            </span>
                                            {entry.hint && (
                                                <kbd className="shrink-0 rounded border px-1.5 font-mono text-[11px] leading-[18px] text-muted-foreground">{entry.hint}</kbd>
                                            )}
                                        </Command.Item>
                                    ))}
                                </Command.Group>
                            ))}
                        </Command.List>
                        <div className="hidden md:flex h-10 shrink-0 items-center justify-between border-t px-4 font-mono text-[11px] text-muted-foreground">
                            <span className="flex gap-4">
                                <span>↑↓ move</span>
                                <span>↵ open</span>
                                <span>{MOD_KEY}↵ new tab</span>
                                <span>esc close</span>
                            </span>
                            <span>users · subs · nodes · pages</span>
                        </div>
                    </Command>
                </DialogPrimitive.Content>
            </DialogPrimitive.Portal>
        </DialogPrimitive.Root>
    )
}
